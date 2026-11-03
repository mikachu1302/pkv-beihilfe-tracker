package service

import (
	"encoding/base64"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// addPages puts made-up photos into the inbox and merges them into one document.
func addPages(t *testing.T, s *Service, names ...string) string {
	t.Helper()
	var added []string
	for i, n := range names {
		got, err := s.AddToInbox(n, base64.StdEncoding.EncodeToString(append(tinyPNG(), byte(10+i))))
		if err != nil {
			t.Fatal(err)
		}
		added = append(added, got)
	}
	if len(added) == 1 {
		return added[0]
	}
	g, err := s.MergeInbox(added)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestInboxGroupListedWithPages(t *testing.T) {
	s := newTestService(t)
	g := addPages(t, s, "Scan #1.png", "Scan 2.png")
	single := addPages(t, s, "Brief.png")
	items, err := s.ListInbox()
	if err != nil || len(items) != 2 {
		t.Fatalf("items %+v, %v", items, err)
	}
	it := items[0]
	if it.Name != g || !it.Group || it.Type != "group" || it.URL != "" || len(it.Pages) != 2 {
		t.Fatalf("group: %+v", it)
	}
	if p := it.Pages[0]; p.Name != "Scan #1/01 Scan #1.png" || p.Type != "image" || p.URL != FilePrefix+"inbox/Scan%20%231/01%20Scan%20%231.png" {
		t.Errorf("page: %+v", p)
	}
	if it.Size != it.Pages[0].Size+it.Pages[1].Size {
		t.Errorf("size %d", it.Size)
	}
	if one := items[1]; one.Name != single || one.Group || one.Type != "image" || len(one.Pages) != 1 || one.URL != one.Pages[0].URL {
		t.Errorf("single: %+v", one)
	}
	// The webview gets pages under their URL.
	rec := httptestGet(t, s, it.Pages[1].URL)
	if rec != 200 {
		t.Errorf("serve page: %d", rec)
	}
}

func TestInvoiceFromMultiPageInboxDocument(t *testing.T) {
	s := newTestService(t)
	g := addPages(t, s, "a.png", "b.png", "c.png")
	if err := s.SplitInbox(g + "/03 c.png"); err != nil { // c becomes a document of its own
		t.Fatal(err)
	}
	det, err := s.CreateInvoiceFromInbox(g, sampleInvoice())
	if err != nil {
		t.Fatal(err)
	}
	if len(det.Invoice.Attachments) != 2 {
		t.Fatalf("attachments %v", det.Invoice.Attachments)
	}
	items, _ := s.ListInbox()
	if len(items) != 1 || items[0].Name != "c.png" {
		t.Errorf("inbox after saving: %+v", items)
	}
	if _, err := os.Stat(filepath.Join(s.st.InboxDirPath(), g)); !os.IsNotExist(err) {
		t.Error("folder left behind")
	}
	// The pages in order: a, then b.
	a, _ := s.st.DocumentPath(det.Invoice.Attachments[0])
	if b, _ := os.ReadFile(a); b[len(b)-1] != 10 {
		t.Error("first attachment is not page a")
	}

	// A page that belongs to an invoice already is named.
	again := addPages(t, s, "x.png", "y.png") // same contents as a and b
	if _, err := s.CreateInvoiceFromInbox(again, sampleInvoice()); err == nil || !strings.Contains(err.Error(), "Die Seite „01 x.png“ ist bereits erfasst") {
		t.Errorf("duplicate page: %v", err)
	}
}

func TestMultiPageBescheid(t *testing.T) {
	s := newTestService(t)
	refundFixture(t, s)
	r, err := s.AnalyzeRefund(AnalyzeInputDTO{Lines: linesOf(notice)})
	if err != nil {
		t.Fatal(err)
	}
	g := addPages(t, s, "Bescheid 1.png", "Bescheid 2.png")
	res, err := s.ConfirmRefund(g, r, false)
	if err != nil {
		t.Fatal(err)
	}
	var doc = s.data.RefundDocs[len(s.data.RefundDocs)-1]
	if doc.ID != res.DocID || doc.Attachment == "" || len(doc.MorePages) != 1 {
		t.Fatalf("doc %+v", doc)
	}
	if items, _ := s.ListInbox(); len(items) != 0 {
		t.Errorf("inbox: %+v", items)
	}
	path, err := s.RefundDocPath(doc.ID)
	if err != nil || filepath.Ext(path) != ".pdf" || !strings.HasPrefix(path, s.tmpRoot) {
		t.Errorf("open: %q, %v", path, err)
	}
	// A page of the Bescheid is in use: removing it from an invoice keeps it.
	if !documentInUse(s.data, doc.MorePages[0], "") {
		t.Error("second page not in use")
	}
}

func TestStagedPagesGoToTheInboxAsOneDocument(t *testing.T) {
	s := newTestService(t)
	p1, p2 := stagePNG(t, s, "Bescheid S1.png", 1), stagePNG(t, s, "Bescheid S2.png", 2)
	name, err := s.StagedToInbox([]string{p1.ID, p2.ID})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := s.ListInbox()
	if len(items) != 1 || items[0].Name != name || !items[0].Group || len(items[0].Pages) != 2 {
		t.Errorf("inbox: %+v", items)
	}
}

func TestNewPagesReported(t *testing.T) {
	s := newTestService(t)
	g := addPages(t, s, "a.png", "b.png")
	events := make(chan DataChangedDTO, 10)
	s.SetEmitter(emitterFunc(func(_ string, data ...any) bool {
		if len(data) > 0 {
			if ev, ok := data[0].(DataChangedDTO); ok {
				events <- ev
			}
		}
		return true
	}))
	// The phone (or a sync tool) adds a third page to the document.
	if err := os.WriteFile(filepath.Join(s.st.InboxDirPath(), g, "03 c.png"), tinyPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-events:
		if ev.InboxPages != 1 || ev.InboxAdded != 0 {
			t.Errorf("event %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
	}
}

func httptestGet(t *testing.T, s *Service, url string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeFile(rec, httptest.NewRequest("GET", url, nil))
	return rec.Code
}

type emitterFunc func(name string, data ...any) bool

func (f emitterFunc) Emit(name string, data ...any) bool { return f(name, data...) }

func TestSuggestInboxGroups(t *testing.T) {
	s := newTestService(t)
	page := func(text string) []LineDTO { return linesOf(text) }
	got := s.SuggestInboxGroups([]InboxTextDTO{
		{Name: "b.jpg", Lines: page("Praxis Dr. Muster\nSeite 2 von 2\nRechnungsbetrag 120,00")},
		{Name: "x.pdf", Lines: page("Apotheke am Markt\nSumme 9,99")},
		{Name: "a.jpg", Lines: page("Praxis Dr. Muster\nSeite 1 von 2\nRechnung")},
	})
	if len(got) != 1 || strings.Join(got[0].Names, ",") != "a.jpg,b.jpg" || got[0].Reason != "Seite 1 und 2 von 2" {
		t.Errorf("suggestions %+v", got)
	}
	if got := s.SuggestInboxGroups(nil); got == nil || len(got) != 0 {
		t.Errorf("empty: %+v", got)
	}
}
