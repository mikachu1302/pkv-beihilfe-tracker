package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

var d = civil.MustParse

// clock returns a fake time source that advances one second per call.
func clock(start string) func() time.Time {
	t, _ := time.Parse(time.RFC3339, start)
	return func() time.Time {
		t = t.Add(time.Second)
		return t
	}
}

func open(t *testing.T, root, device, start string) *Store {
	t.Helper()
	s, err := Open(root, device)
	if err != nil {
		t.Fatal(err)
	}
	s.now = clock(start)
	return s
}

func load(t *testing.T, s *Store) (*Data, []Issue) {
	t.Helper()
	data, issues, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	return data, issues
}

func sampleInvoice() model.Invoice {
	return model.Invoice{
		ID: "20260314-0000abcd", Provider: "Dr. Muster", InvoiceDate: d("2026-03-14"), ServiceFrom: d("2026-03-03"),
		Amount: 28640, Claims: map[string]model.Claim{"bh": {Submitted: model.Mark{Date: d("2026-04-02")}}},
	}
}

func readEnv(t *testing.T, path string) envelope {
	t.Helper()
	env, err := readEnvelope(path)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestSaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	s := open(t, root, "a", "2026-11-02T10:00:00Z")
	set := model.Settings{Payers: []model.Payer{{ID: "bh", Kind: model.Beihilfe, Name: "Beihilfe"}},
		Coverage: []model.CoveragePeriod{{From: d("2023-01-01"), Shares: []model.Share{{PayerID: "bh", Rate: money.Percent(50)}}}}}
	if _, err := s.SaveSettings(set); err != nil {
		t.Fatal(err)
	}
	inv := sampleInvoice()
	if _, err := s.SaveInvoice(inv); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveAllowance(model.Allowance{ID: "k1", PayerID: "pkv", Kind: model.AllowanceHospitalDay, Date: d("2025-07-01"), Amount: 45000}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{SettingsFile, "2026/rechnungen/20260314-0000abcd.json", "2025/leistungen/k1.json", InboxDir, DocumentsDir} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	env := readEnv(t, filepath.Join(root, "2026/rechnungen/20260314-0000abcd.json"))
	if env.Type != KindInvoice || env.Clock["/amount"] != "2026-11-02T10:00:02.000Z@a" || env.Clock["/claims/bh/submitted/date"] == "" {
		t.Errorf("envelope: %+v", env)
	}

	data, issues := load(t, open(t, root, "b", "2026-11-02T11:00:00Z"))
	if len(issues) != 0 || len(data.Invoices) != 1 || len(data.Allowances) != 1 || len(data.Settings.Payers) != 1 {
		t.Fatalf("loaded %+v, issues %v", data, issues)
	}
	got, _ := json.Marshal(data.Invoices[0])
	want, _ := json.Marshal(inv)
	if !bytes.Equal(got, want) {
		t.Errorf("round trip\n got %s\nwant %s", got, want)
	}
}

func TestOnlyChangedLeavesGetNewStamps(t *testing.T) {
	root := t.TempDir()
	s := open(t, root, "a", "2026-11-02T10:00:00Z")
	inv := sampleInvoice()
	s.SaveInvoice(inv)
	path := filepath.Join(root, "2026/rechnungen", inv.ID+".json")
	before := readEnv(t, path).Clock
	inv.Amount = 30000
	inv.Claims["bh"] = model.Claim{} // untick
	s.SaveInvoice(inv)
	after := readEnv(t, path).Clock
	for p, st := range after {
		changed := p == "/amount" || p == "/claims/bh/submitted/date"
		if changed == (st == before[p]) {
			t.Errorf("%s: before %q after %q", p, before[p], st)
		}
	}
}

func TestYearChangeMovesFile(t *testing.T) {
	root := t.TempDir()
	s := open(t, root, "a", "2026-11-02T10:00:00Z")
	inv := sampleInvoice()
	s.SaveInvoice(inv)
	inv.ServiceFrom = d("2025-12-29")
	if _, err := s.SaveInvoice(inv); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "2026/rechnungen", inv.ID+".json")); !os.IsNotExist(err) {
		t.Error("old file still there")
	}
	if _, err := os.Stat(filepath.Join(root, "2025/rechnungen", inv.ID+".json")); err != nil {
		t.Error("new file missing")
	}
}

func TestConcurrentEditsOfDifferentFieldsAreKept(t *testing.T) {
	root := t.TempDir()
	a := open(t, root, "a", "2026-11-02T10:00:00Z")
	a.SaveInvoice(sampleInvoice())
	b := open(t, root, "b", "2026-11-02T10:05:00Z")
	dataB, _ := load(t, b)

	// Device A changes the provider; device B, still on the old version, the amount.
	invA := sampleInvoice()
	invA.Provider = "Dr. Muster-Neu"
	a.SaveInvoice(invA)
	invB := dataB.Invoices[0]
	invB.Amount = 99900
	got, err := b.SaveInvoice(invB)
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "Dr. Muster-Neu" || got.Amount != 99900 {
		t.Errorf("merged = %+v", got)
	}
}

func TestSameFieldNewerWins(t *testing.T) {
	ea := envelope{Schema: 1, Type: KindInvoice, Data: json.RawMessage(`{"id":"x","note":"alt"}`),
		Clock: map[string]string{"/id": "2026-11-02T10:00:00.000Z@a", "/note": "2026-11-02T10:00:00.000Z@a"}}
	eb := envelope{Schema: 1, Type: KindInvoice, Data: json.RawMessage(`{"id":"x","note":"neu"}`),
		Clock: map[string]string{"/id": "2026-11-02T10:00:00.000Z@a", "/note": "2026-11-02T10:00:01.000Z@b"}}
	for _, pair := range [][2]envelope{{ea, eb}, {eb, ea}} {
		m, err := merge(pair[0], pair[1])
		if err != nil || !strings.Contains(string(m.Data), `"note":"neu"`) {
			t.Errorf("merge = %s, %v", m.Data, err)
		}
	}
	// A newer removal beats an older value.
	ec := envelope{Schema: 1, Type: KindInvoice, Data: json.RawMessage(`{"id":"x"}`),
		Clock: map[string]string{"/id": "2026-11-02T10:00:00.000Z@a", "/note": "2026-11-02T10:00:02.000Z@c"}}
	if m, _ := merge(eb, ec); strings.Contains(string(m.Data), "note") {
		t.Errorf("removal lost: %s", m.Data)
	}
}

func TestConflictCopiesAreMerged(t *testing.T) {
	names := []string{
		"%s (conflicted copy 2026-11-02 101500).json",   // Nextcloud
		"%s.sync-conflict-20261102-101500-ABCDEFG.json", // Syncthing
		"%s 2.json",      // iCloud
		"%s (1).json",    // Google Drive
		"%s-LAPTOP.json", // OneDrive
	}
	for _, pattern := range names {
		t.Run(pattern, func(t *testing.T) {
			root := t.TempDir()
			s := open(t, root, "a", "2026-11-02T10:00:00Z")
			inv := sampleInvoice()
			s.SaveInvoice(inv)
			dir := filepath.Join(root, "2026/rechnungen")
			canon := filepath.Join(dir, inv.ID+".json")

			// The other device's version: note added later.
			other := open(t, t.TempDir(), "b", "2026-11-02T12:00:00Z")
			other.base[key(KindInvoice, inv.ID)] = readEnv(t, canon)
			inv2 := inv
			inv2.Note = "Rückfrage gestellt"
			if _, err := other.SaveInvoice(inv2); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(filepath.Join(other.root, "2026/rechnungen", inv.ID+".json"))
			copyPath := filepath.Join(dir, strings.ReplaceAll(pattern, "%s", inv.ID))
			os.WriteFile(copyPath, b, 0o644)

			data, issues := load(t, open(t, root, "a", "2026-11-02T13:00:00Z"))
			if len(data.Invoices) != 1 || data.Invoices[0].Note != "Rückfrage gestellt" {
				t.Fatalf("invoices %+v", data.Invoices)
			}
			if len(issues) != 1 || !strings.Contains(issues[0].Text, "Konfliktkopie") {
				t.Errorf("issues %v", issues)
			}
			if _, err := os.Stat(copyPath); !os.IsNotExist(err) {
				t.Error("conflict copy not removed")
			}
			if env := readEnv(t, canon); !strings.Contains(string(env.Data), "Rückfrage") {
				t.Error("merge not written to the canonical file")
			}
		})
	}
}

func TestSettingsConflictCopy(t *testing.T) {
	root := t.TempDir()
	s := open(t, root, "a", "2026-11-02T10:00:00Z")
	s.SaveSettings(model.Settings{ClarifyTolerance: 100})
	b, _ := os.ReadFile(filepath.Join(root, SettingsFile))
	var env envelope
	json.Unmarshal(b, &env)
	env.Data = json.RawMessage(`{"payers":null,"coverage":null,"clarifyTolerance":"5.00"}`)
	env.Clock["/clarifyTolerance"] = "2026-11-02T11:00:00.000Z@b"
	nb, _ := json.Marshal(env)
	os.WriteFile(filepath.Join(root, "einstellungen (Konfliktkopie).json"), nb, 0o644)
	os.WriteFile(filepath.Join(root, "notizen.json"), []byte(`{"meine":"daten"}`), 0o644)

	data, issues := load(t, open(t, root, "a", "2026-11-02T12:00:00Z"))
	if data.Settings.ClarifyTolerance != 500 || len(issues) != 1 {
		t.Errorf("settings %+v, issues %v", data.Settings, issues)
	}
	if _, err := os.Stat(filepath.Join(root, "notizen.json")); err != nil {
		t.Error("unrelated user file must be left alone")
	}
}

func TestMisplacedRecordIsMoved(t *testing.T) {
	root := t.TempDir()
	s := open(t, root, "a", "2026-11-02T10:00:00Z")
	inv := sampleInvoice()
	s.SaveInvoice(inv)
	os.MkdirAll(filepath.Join(root, "2024/rechnungen"), 0o755)
	os.Rename(filepath.Join(root, "2026/rechnungen", inv.ID+".json"), filepath.Join(root, "2024/rechnungen", inv.ID+".json"))
	_, issues := load(t, open(t, root, "a", "2026-11-02T11:00:00Z"))
	if len(issues) != 1 || !strings.Contains(issues[0].Text, "verschoben") {
		t.Errorf("issues %v", issues)
	}
	if _, err := os.Stat(filepath.Join(root, "2026/rechnungen", inv.ID+".json")); err != nil {
		t.Error("record not moved back")
	}
}

func TestBadFilesAreReported(t *testing.T) {
	root := t.TempDir()
	s := open(t, root, "a", "2026-11-02T10:00:00Z")
	s.SaveInvoice(sampleInvoice())
	dir := filepath.Join(root, "2026/rechnungen")
	os.WriteFile(filepath.Join(dir, "kaputt.json"), []byte("{nicht json"), 0o644)
	os.WriteFile(filepath.Join(dir, "neu.json"), []byte(`{"schema":99,"type":"invoice","data":{"id":"n"}}`), 0o644)
	os.WriteFile(filepath.Join(dir, ".tmp-12345"), []byte("halb geschrieben"), 0o644)
	os.WriteFile(filepath.Join(dir, "ohne-id.json"), []byte(`{"schema":1,"type":"invoice","data":{"amount":"1.00"}}`), 0o644)
	data, issues := load(t, open(t, root, "a", "2026-11-02T11:00:00Z"))
	if len(data.Invoices) != 1 || len(issues) != 3 {
		t.Errorf("invoices %d, issues %v", len(data.Invoices), issues)
	}
	if _, err := os.Stat(filepath.Join(dir, "neu.json")); err != nil {
		t.Error("file of a newer version must not be touched")
	}
}

func TestTimestampsAreMonotonic(t *testing.T) {
	s := open(t, t.TempDir(), "a", "2026-11-02T10:00:00Z")
	s.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) } // clock behind
	base := envelope{Clock: map[string]string{"/x": "2026-11-02T10:00:00.000Z@b"}}
	if got := s.timestamp(base); got != "2026-11-02T10:00:00.001Z@a" {
		t.Errorf("timestamp = %s", got)
	}
}

func TestAddDocument(t *testing.T) {
	s := open(t, t.TempDir(), "a", "2026-11-02T10:00:00Z")
	n1, err := s.AddDocument([]byte("%PDF-1.7 beispiel"), ".PDF")
	if err != nil {
		t.Fatal(err)
	}
	n2, _ := s.AddDocument([]byte("%PDF-1.7 beispiel"), ".pdf")
	if n1 != n2 || !strings.HasSuffix(n1, ".pdf") || len(n1) != 64+4 {
		t.Errorf("names %q %q", n1, n2)
	}
	path, err := s.DocumentPath(n1)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "%PDF-1.7 beispiel" {
		t.Errorf("content %q, %v", b, err)
	}
	// Documents are opened with the default app: no executables or scripts.
	for _, ext := range []string{"../evil", ".exe", ".bat", ".hta", ".sh", ""} {
		if _, err := s.AddDocument([]byte("x"), ext); err == nil {
			t.Errorf("extension %q accepted", ext)
		}
	}
}

func TestFileNamesAreChecked(t *testing.T) {
	s := open(t, t.TempDir(), "a", "2026-11-02T10:00:00Z")
	for _, name := range []string{"", ".", "..", ".hidden", "a/b.pdf", `a\b.pdf`, "../x.pdf"} {
		if _, err := s.DocumentPath(name); err == nil {
			t.Errorf("DocumentPath(%q) accepted", name)
		}
	}
	// Inbox names may name a page of a multi-page entry ("folder/file"), no more.
	for _, name := range []string{"", ".", "..", ".hidden", `a\b.pdf`, "../x.pdf", "a/b/c.pdf", "a/.x", "../a/x.pdf", "a/"} {
		if _, err := s.InboxPath(name); err == nil {
			t.Errorf("InboxPath(%q) accepted", name)
		}
	}
	if p, err := s.InboxPath("Scan/01 Scan.jpg"); err != nil || p != filepath.Join(s.InboxDirPath(), "Scan", "01 Scan.jpg") {
		t.Errorf("page path %q, %v", p, err)
	}
	if err := s.RemoveFromInbox(""); err == nil {
		t.Error("RemoveFromInbox(\"\") accepted")
	}
	if _, err := os.Stat(s.InboxDirPath()); err != nil {
		t.Error("inbox folder removed")
	}
}

func TestUnreadableFileIsNeverOverwritten(t *testing.T) {
	root := t.TempDir()
	s := open(t, root, "a", "2026-11-02T10:00:00Z")
	inv := sampleInvoice()
	s.SaveInvoice(inv)
	dir := filepath.Join(root, "2026/rechnungen")
	canon := filepath.Join(dir, inv.ID+".json")
	good, _ := os.ReadFile(canon)
	// A typo made by hand in the file, and a readable conflict copy.
	os.WriteFile(canon, []byte("{kaputt"), 0o644)
	copyPath := filepath.Join(dir, inv.ID+" (1).json")
	os.WriteFile(copyPath, good, 0o644)

	s2 := open(t, root, "a", "2026-11-02T11:00:00Z")
	data, issues := load(t, s2)
	if len(data.Invoices) != 0 {
		t.Errorf("record loaded from the copy although its file is unreadable: %+v", data.Invoices)
	}
	if len(issues) != 2 {
		t.Errorf("issues %v", issues)
	}
	if b, _ := os.ReadFile(canon); string(b) != "{kaputt" {
		t.Error("unreadable file was overwritten")
	}
	if _, err := os.Stat(copyPath); err != nil {
		t.Error("conflict copy removed")
	}
	if _, err := s2.SaveInvoice(inv); err == nil || !strings.Contains(err.Error(), "nicht überschrieben") {
		t.Errorf("save over unreadable file: %v", err)
	}
}

func TestSettingsOfNewerVersionAreKept(t *testing.T) {
	root := t.TempDir()
	newer := `{"schema":99,"type":"settings","data":{"payers":[]}}`
	os.WriteFile(filepath.Join(root, SettingsFile), []byte(newer), 0o644)
	s := open(t, root, "a", "2026-11-02T10:00:00Z")
	_, issues := load(t, s)
	if len(issues) != 1 || !strings.Contains(issues[0].Text, "neueren") {
		t.Errorf("issues %v", issues)
	}
	if _, err := s.SaveSettings(model.Settings{ClarifyTolerance: 100}); err == nil {
		t.Error("settings of a newer version overwritten")
	}
	if b, _ := os.ReadFile(filepath.Join(root, SettingsFile)); string(b) != newer {
		t.Error("file changed")
	}
}

func TestFailedMergeKeepsState(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a read-only folder")
	}
	root := t.TempDir()
	s := open(t, root, "a", "2026-11-02T10:00:00Z")
	inv := sampleInvoice()
	s.SaveInvoice(inv)
	dir := filepath.Join(root, "2026/rechnungen")
	canon := filepath.Join(dir, inv.ID+".json")

	other := open(t, t.TempDir(), "b", "2026-11-02T12:00:00Z")
	other.base[key(KindInvoice, inv.ID)] = readEnv(t, canon)
	inv2 := inv
	inv2.Note = "von Gerät b"
	other.SaveInvoice(inv2)
	b, _ := os.ReadFile(filepath.Join(other.root, "2026/rechnungen", inv.ID+".json"))
	copyPath := filepath.Join(dir, inv.ID+" (1).json")
	os.WriteFile(copyPath, b, 0o644)

	// The merge cannot be written (a file locked by another program).
	os.Chmod(dir, 0o555)
	s2 := open(t, root, "a", "2026-11-02T13:00:00Z")
	data, issues := load(t, s2)
	os.Chmod(dir, 0o755)
	if len(data.Invoices) != 1 || data.Invoices[0].Note != "von Gerät b" {
		t.Fatalf("merged state not used: %+v", data.Invoices)
	}
	if len(issues) != 1 || !strings.Contains(issues[0].Text, "nicht zusammengeführt") {
		t.Errorf("issues %v", issues)
	}
	// A later save still knows the record's file and keeps the other device's note.
	inv3 := data.Invoices[0]
	inv3.Reference = "R-1"
	saved, err := s2.SaveInvoice(inv3)
	if err != nil || saved.Note != "von Gerät b" || saved.Reference != "R-1" {
		t.Errorf("save after failed merge: %+v, %v", saved, err)
	}
}

func TestOpenRejectsBadDevice(t *testing.T) {
	if _, err := Open(t.TempDir(), "a@b"); err == nil {
		t.Error("device with @ accepted")
	}
}

func TestIsOwnChange(t *testing.T) {
	root := t.TempDir()
	s := open(t, root, "a", "2026-11-02T10:00:00Z")
	inv := sampleInvoice()
	s.SaveInvoice(inv)
	path := filepath.Join(root, "2026/rechnungen", inv.ID+".json")
	if !s.IsOwnChange(path) {
		t.Fatal("own write not recognised")
	}
	// A sync tool replaces the file with another device's version.
	time.Sleep(10 * time.Millisecond)
	b, _ := os.ReadFile(path)
	os.WriteFile(path, append(b, ' '), 0o644)
	if s.IsOwnChange(path) {
		t.Error("external change taken for our own")
	}
	if s.IsOwnChange(filepath.Join(root, "2026/rechnungen/fremd.json")) {
		t.Error("unknown file taken for our own")
	}
	// Moving to another year removes the old file: that removal is ours too.
	inv.ServiceFrom = d("2025-12-30")
	s.SaveInvoice(inv)
	if !s.IsOwnChange(path) {
		t.Error("own removal not recognised")
	}
}

func TestInbox(t *testing.T) {
	root := t.TempDir()
	s := open(t, root, "a", "2026-11-02T10:00:00Z")
	n1, err := s.AddToInbox("scan.pdf", []byte("%PDF eins"))
	if err != nil || n1 != "scan.pdf" {
		t.Fatalf("add: %q %v", n1, err)
	}
	n2, _ := s.AddToInbox(`C:\Users\x\scan.pdf`, []byte("%PDF zwei"))
	if n2 != "scan (2).pdf" {
		t.Errorf("second name %q", n2)
	}
	p1, _ := s.InboxPath(n1)
	if !s.IsOwnChange(p1) {
		t.Error("own inbox file not recognised")
	}
	os.WriteFile(filepath.Join(root, InboxDir, ".tmp-x"), []byte("x"), 0o644)
	list, err := s.ListInbox()
	if err != nil || len(list) != 2 {
		t.Fatalf("list %+v %v", list, err)
	}
	if err := s.RemoveFromInbox(n1); err != nil || !s.IsOwnChange(p1) {
		t.Errorf("remove: %v", err)
	}
	if list, _ := s.ListInbox(); len(list) != 1 || list[0].Name != n2 {
		t.Errorf("after remove %+v", list)
	}
}
