package service

import (
	"encoding/base64"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
)

func TestDocumentsPDF(t *testing.T) {
	s := newTestService(t)
	a, err := s.st.AddDocument(tinyPNG(), ".png")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.st.AddDocument(append(tinyPNG(), 0), ".png") // another file
	if err != nil {
		t.Fatal(err)
	}

	// One document: the file itself.
	one, err := s.DocumentsPDF([]string{a})
	if want, _ := s.st.DocumentPath(a); err != nil || one != want {
		t.Errorf("one page: %q, %v", one, err)
	}

	// Several: a temporary PDF in the session folder, outside the data folder.
	both, err := s.DocumentsPDF([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(both, s.tmpRoot) || filepath.Ext(both) != ".pdf" {
		t.Errorf("merged PDF at %q", both)
	}
	if head, _ := os.ReadFile(both); !strings.HasPrefix(string(head), "%PDF") {
		t.Error("not a PDF")
	}

	// Invalid names and pages that cannot go into a PDF are refused.
	if _, err := s.DocumentsPDF([]string{a, "../einstellungen.json"}); err == nil {
		t.Error("path accepted")
	}
	heic, _ := s.st.AddDocument([]byte("not really heic"), ".heic")
	if _, err := s.DocumentsPDF([]string{a, heic}); err == nil || !strings.Contains(err.Error(), "1 von 2 Seiten") {
		t.Errorf("heic page: %v", err)
	}
	if _, err := s.DocumentsPDF(nil); err == nil {
		t.Error("no names accepted")
	}

	// Shutdown removes the session folder.
	dir := filepath.Dir(both)
	_ = s.ServiceShutdown()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("session folder still there: %v", err)
	}
}

func TestStaleSessionsRemoved(t *testing.T) {
	s := newTestService(t)
	old := filepath.Join(s.tmpRoot, sessionPrefix+"alt")
	young := filepath.Join(s.tmpRoot, sessionPrefix+"jung")
	other := filepath.Join(s.tmpRoot, "fremd")
	for _, d := range []string{old, young, other} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	past := s.now().Add(-2 * staleSession)
	_ = os.Chtimes(old, past, past)
	_ = os.Chtimes(other, past, past)
	s.mu.Lock()
	_, err := s.sessionDir()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	for d, want := range map[string]bool{old: false, young: true, other: true} {
		if _, err := os.Stat(d); (err == nil) != want {
			t.Errorf("%s exists = %v, want %v", filepath.Base(d), err == nil, want)
		}
	}
}

func stagePNG(t *testing.T, s *Service, name string, extra byte) StagedDocDTO {
	t.Helper()
	d, err := s.StageFile(name, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(append(tinyPNG(), extra)))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestStageFiles(t *testing.T) {
	s := newTestService(t)
	d := stagePNG(t, s, `C:\Scans\seite 1.png`, 1)
	if d.Name != "seite 1.png" || d.Type != "image" || !strings.HasPrefix(d.URL, FilePrefix+"staged/") || !strings.HasPrefix(d.Path, s.tmpRoot) {
		t.Errorf("staged: %+v", d)
	}
	// The webview gets it under its URL; unknown IDs are not served.
	rec := httptest.NewRecorder()
	s.ServeFile(rec, httptest.NewRequest("GET", d.URL, nil))
	if rec.Code != 200 || rec.Body.Len() == 0 {
		t.Errorf("serve: %d", rec.Code)
	}
	for _, u := range []string{FilePrefix + "staged/0123456789abcdef.png", FilePrefix + "staged/../einstellungen.json"} {
		rec = httptest.NewRecorder()
		s.ServeFile(rec, httptest.NewRequest("GET", u, nil))
		if rec.Code != 404 {
			t.Errorf("%s: %d", u, rec.Code)
		}
	}

	if _, err := s.StageFile("virus.exe", base64.StdEncoding.EncodeToString([]byte("MZ"))); err == nil || !strings.Contains(err.Error(), "nur PDF und Bilder") {
		t.Errorf("exe: %v", err)
	}
	if _, err := s.StageFile("leer.pdf", ""); err == nil {
		t.Error("empty file accepted")
	}
	old := maxDocumentSize
	maxDocumentSize = 10
	t.Cleanup(func() { maxDocumentSize = old })
	if _, err := s.StageFile("gross.png", base64.StdEncoding.EncodeToString(tinyPNG())); err == nil || !strings.Contains(err.Error(), "größer als") {
		t.Errorf("too big: %v", err)
	}
	maxDocumentSize = old

	path := filepath.Join(t.TempDir(), "rechnung.png")
	if err := os.WriteFile(path, tinyPNG(), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := s.StageFiles([]string{path})
	if err != nil || len(got) != 1 || got[0].Name != "rechnung.png" {
		t.Fatalf("from dialog: %+v, %v", got, err)
	}
	s.DiscardStaged([]string{got[0].ID})
	if _, err := os.Stat(got[0].Path); !os.IsNotExist(err) {
		t.Error("discarded file still there")
	}
}

func TestSaveInvoiceWithStagedDocuments(t *testing.T) {
	s := newTestService(t)
	p1, p2 := stagePNG(t, s, "seite1.png", 1), stagePNG(t, s, "seite2.png", 2)
	again := stagePNG(t, s, "nochmal.png", 1) // the same file as page 1
	det, err := s.SaveInvoice(sampleInvoice(), nil, []string{p1.ID, p2.ID, again.ID})
	if err != nil {
		t.Fatal(err)
	}
	atts := det.Invoice.Attachments
	if len(atts) != 2 || det.View.Attachments != 2 {
		t.Fatalf("attachments: %v", atts)
	}
	for _, a := range atts {
		if p, _ := s.st.DocumentPath(a); !exists(p) {
			t.Errorf("%s not in belege", a)
		}
	}
	if exists(p1.Path) || exists(p2.Path) {
		t.Error("staged files left behind")
	}

	// The same document for another invoice is refused, and nothing is saved.
	dup := stagePNG(t, s, "doppelt.png", 2)
	other := sampleInvoice()
	other.Provider = "Labor Nord"
	if _, err := s.SaveInvoice(other, nil, []string{dup.ID}); err == nil || !strings.Contains(err.Error(), "gehört bereits zu einer anderen Rechnung") {
		t.Errorf("duplicate: %v", err)
	}
	if rows, _ := s.ListInvoices(0); len(rows) != 1 {
		t.Errorf("invoices: %d", len(rows))
	}
	// A staged document that is gone (app restarted) is reported.
	if _, err := s.SaveInvoice(other, nil, []string{"0123456789abcdef.png"}); !errors.Is(err, errStagedGone) {
		t.Errorf("gone: %v", err)
	}
	// A new invoice never takes attachments from the form itself.
	other.Attachments = []string{atts[0]}
	if det, err := s.SaveInvoice(other, nil, nil); err != nil || len(det.Invoice.Attachments) != 0 {
		t.Errorf("foreign attachment taken: %+v, %v", det.Invoice.Attachments, err)
	}
}

func TestRemovedAttachmentGoesBackToInbox(t *testing.T) {
	s := newTestService(t)
	p1, p2, p3 := stagePNG(t, s, "a.png", 1), stagePNG(t, s, "b.png", 2), stagePNG(t, s, "c.png", 3)
	det, err := s.SaveInvoice(sampleInvoice(), nil, []string{p1.ID, p2.ID, p3.ID})
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := det.Invoice.Attachments[0], det.Invoice.Attachments[1], det.Invoice.Attachments[2]
	// Page c also belongs to a Bescheid: it is only unlinked.
	doc := model.RefundDoc{ID: "20261102-0000d0c1", PayerID: "beihilfe-bund", Attachment: c}
	if _, err := s.st.SaveRefundDoc(doc); err != nil {
		t.Fatal(err)
	}
	s.data.RefundDocs = append(s.data.RefundDocs, doc)

	// Meanwhile another device adds page d.
	d, _ := s.st.AddDocument(append(tinyPNG(), 4), ".png")
	cur, _ := findInvoice(s.data, det.Invoice.ID)
	inv := cloneInvoice(cur)
	inv.Attachments = append(inv.Attachments, d)
	if _, err := s.st.SaveInvoice(inv); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reload(); err != nil {
		t.Fatal(err)
	}

	edit := det.Invoice
	edit.Attachments = []string{b} // a and c removed in the form
	saved, err := s.SaveInvoice(edit, &det.Invoice, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Invoice.Attachments; !slices.Equal(got, []string{b, d}) {
		t.Errorf("attachments %v, want b and d", got)
	}
	if saved.MovedToInbox != 1 {
		t.Errorf("moved %d, want 1", saved.MovedToInbox)
	}
	if p, _ := s.st.DocumentPath(a); exists(p) {
		t.Error("page a still in belege")
	}
	if p, _ := s.st.DocumentPath(c); !exists(p) {
		t.Error("page c (used by the Bescheid) removed")
	}
	items, _ := s.ListInbox()
	if len(items) != 1 || items[0].Name != "Dr. Muster 2026-03-14.png" {
		t.Errorf("inbox: %+v", items)
	}
}

func TestStagedToInbox(t *testing.T) {
	s := newTestService(t)
	d := stagePNG(t, s, "bescheid.png", 1)
	name, err := s.StagedToInbox([]string{d.ID})
	if err != nil || name != "bescheid.png" {
		t.Fatalf("name %q, %v", name, err)
	}
	if exists(d.Path) {
		t.Error("staged file left behind")
	}
	if _, err := s.StagedToInbox([]string{d.ID}); !errors.Is(err, errStagedGone) {
		t.Errorf("twice: %v", err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
