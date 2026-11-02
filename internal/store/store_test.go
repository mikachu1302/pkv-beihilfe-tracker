package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
	n1, err := s.AddDocument(strings.NewReader("%PDF-1.7 beispiel"), ".PDF")
	if err != nil {
		t.Fatal(err)
	}
	n2, _ := s.AddDocument(strings.NewReader("%PDF-1.7 beispiel"), ".pdf")
	if n1 != n2 || !strings.HasSuffix(n1, ".pdf") || len(n1) != 64+4 {
		t.Errorf("names %q %q", n1, n2)
	}
	if b, err := os.ReadFile(s.DocumentPath(n1)); err != nil || string(b) != "%PDF-1.7 beispiel" {
		t.Errorf("content %q, %v", b, err)
	}
	if _, err := s.AddDocument(strings.NewReader("x"), "../evil"); err == nil {
		t.Error("bad extension accepted")
	}
}

func TestFlattenRoundTrip(t *testing.T) {
	in := json.RawMessage(`{"a":{"b/c":1,"d":{"e":[1,{"f":2}]}},"g":null,"h":"x~y"}`)
	leaves, err := flatten(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := leaves["/a/b~1c"]; !ok || string(leaves["/a/d/e"]) != `[1,{"f":2}]` {
		t.Errorf("leaves %v", leaves)
	}
	out, _ := unflatten(leaves)
	var x, y any
	json.Unmarshal(in, &x)
	json.Unmarshal(out, &y)
	if a, b := mustJSON(x), mustJSON(y); a != b {
		t.Errorf("round trip %s != %s", a, b)
	}
	if _, err := flatten(json.RawMessage(`[1]`)); err == nil {
		t.Error("non-object accepted")
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestOpenRejectsBadDevice(t *testing.T) {
	if _, err := Open(t.TempDir(), "a@b"); err == nil {
		t.Error("device with @ accepted")
	}
}
