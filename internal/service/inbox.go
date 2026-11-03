package service

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/extract"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/store"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/upload"
)

// FilePrefix is the URL path under which the webview gets data folder files:
// FilePrefix+"inbox/<name>" and FilePrefix+"doc/<name>", and documents added
// in the invoice form before it is saved: FilePrefix+"staged/<id>".
const FilePrefix = "/datafile/"

// phoneUploadTTL is how long the phone upload stays open at most.
const phoneUploadTTL = 15 * time.Minute

// InboxItemDTO is a document waiting in the inbox: a file, or a folder with
// the pages of a multi-page document.
type InboxItemDTO struct {
	Name     string         `json:"name"`
	Size     int64          `json:"size"`     // all pages
	Modified string         `json:"modified"` // newest page, "2026-11-02 10:15"
	Type     string         `json:"type"`     // pdf, image, other; group for several pages
	URL      string         `json:"url"`      // for the webview (single file only)
	Group    bool           `json:"group"`
	Pages    []InboxPageDTO `json:"pages"`
}

// InboxPageDTO is a file of an inbox document.
type InboxPageDTO struct {
	Name     string `json:"name"` // "scan.pdf" or "Scan 12/01 Scan 12.jpg"
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	Type     string `json:"type"`
	URL      string `json:"url"`
}

func inboxItem(e store.InboxEntry) InboxItemDTO {
	it := InboxItemDTO{Name: e.Name, Modified: e.Modified.Format("2006-01-02 15:04"), Group: e.Group, Type: "group",
		Pages: []InboxPageDTO{}}
	for _, p := range e.Pages {
		pg := InboxPageDTO{Name: p.Name, Size: p.Size, Modified: p.Modified.Format("2006-01-02 15:04:05"),
			Type: fileType(p.Name), URL: FilePrefix + "inbox/" + urlEscape(p.Name)}
		it.Pages = append(it.Pages, pg)
		it.Size += p.Size
	}
	if !e.Group && len(it.Pages) == 1 {
		it.Type, it.URL = it.Pages[0].Type, it.Pages[0].URL
	}
	return it
}

func fileType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf":
		return "pdf"
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".tif", ".tiff", ".heic", ".heif":
		return "image"
	}
	return "other"
}

// ListInbox returns the files in the inbox folder, oldest first.
func (s *Service) ListInbox() ([]InboxItemDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, _, err := s.ready()
	if err != nil {
		return nil, err
	}
	entries, err := st.ListInbox()
	if err != nil {
		return nil, err
	}
	out := []InboxItemDTO{}
	for _, e := range entries {
		out = append(out, inboxItem(e))
	}
	return out, nil
}

func urlEscape(name string) string {
	return strings.NewReplacer("%", "%25", " ", "%20", "#", "%23", "?", "%3F").Replace(name)
}

// AddToInbox stores a file dropped onto the window (content as base64).
func (s *Service) AddToInbox(name, data string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, _, err := s.ready()
	if err != nil {
		return "", err
	}
	if i := strings.Index(data, ","); strings.HasPrefix(data, "data:") && i > 0 {
		data = data[i+1:]
	}
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", errors.New("Datei konnte nicht gelesen werden")
	}
	return st.AddToInbox(name, b)
}

// AddFilesToInbox copies files chosen in the file dialog into the inbox folder.
func (s *Service) AddFilesToInbox(paths []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, _, err := s.ready()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return names, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		n, err := st.AddToInbox(filepath.Base(p), b)
		if err != nil {
			return names, err
		}
		names = append(names, n)
	}
	return names, nil
}

// DiscardInbox deletes a document from the inbox, or one page of a
// multi-page document ("folder/file"). pages are the pages the user saw: only
// they are deleted, a page that arrived meanwhile stays (empty: all pages).
func (s *Service) DiscardInbox(name string, pages []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, _, err := s.ready()
	if err != nil {
		return err
	}
	if len(pages) == 0 {
		return st.RemoveFromInbox(name)
	}
	for _, p := range pages {
		if p != name && !strings.HasPrefix(p, name+"/") {
			return fmt.Errorf("„%s“ gehört nicht zu „%s“", p, name)
		}
	}
	return st.RemoveInboxPages(pages)
}

// MergeInbox makes one document of several inbox documents, in the given
// order, and returns its name.
func (s *Service) MergeInbox(names []string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, _, err := s.ready()
	if err != nil {
		return "", err
	}
	return st.MergeInbox(names)
}

// ReorderInbox puts the pages of a multi-page document in the given order.
func (s *Service) ReorderInbox(name string, pages []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, _, err := s.ready()
	if err != nil {
		return err
	}
	return st.ReorderInbox(name, pages)
}

// SplitInbox takes a page ("folder/file") or all pages ("folder") out of a
// multi-page document; each becomes a document of its own.
func (s *Service) SplitInbox(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, _, err := s.ready()
	if err != nil {
		return err
	}
	return st.SplitInbox(name)
}

// inboxPage is a page of an inbox document on its way into belege/.
type inboxPage struct {
	name, doc string // inbox name, name in belege/
	content   []byte
}

// readInboxPages reads the pages of an inbox document in order. The same
// file twice (photographed twice) is kept once. read are all files read –
// exactly these are removed from the inbox after booking.
func readInboxPages(st *store.Store, name string) (pages []inboxPage, read []string, err error) {
	e, err := st.InboxEntry(name)
	if err != nil {
		return nil, nil, fmt.Errorf("Datei im Posteingang nicht gefunden: %w", err)
	}
	for _, p := range e.Pages {
		path, err := st.InboxPath(p.Name)
		if err != nil {
			return nil, nil, err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("Datei im Posteingang nicht gefunden: %w", err)
		}
		doc, err := documentName(b, p.Name)
		if err != nil {
			return nil, nil, err
		}
		read = append(read, p.Name)
		if !slices.ContainsFunc(pages, func(x inboxPage) bool { return x.doc == doc }) {
			pages = append(pages, inboxPage{name: p.Name, doc: doc, content: b})
		}
	}
	return pages, read, nil
}

// storePages puts the pages into belege/ and returns their names there.
func storePages(st *store.Store, pages []inboxPage) ([]string, error) {
	var docs []string
	for _, p := range pages {
		if _, err := st.AddDocument(p.content, filepath.Ext(p.name)); err != nil {
			return nil, fmt.Errorf("Beleg konnte nicht abgelegt werden: %w", err)
		}
		docs = append(docs, p.doc)
	}
	return docs, nil
}

// LineDTO is a line of recognised text with its position (0..1 of the page).
type LineDTO = extract.Line

// AnalyzeInputDTO is the text the frontend recognised in a document.
type AnalyzeInputDTO struct {
	Lines []LineDTO `json:"lines"`
	QR    []string  `json:"qr"` // decoded QR payloads (GiroCode)
}

// AnalysisDTO is the recognised draft.
type AnalysisDTO struct {
	Kind    string            `json:"kind"` // invoice, refund, unknown
	Draft   InvoiceDetailDTO  `json:"draft"`
	Sources map[string]string `json:"sources"` // field → qr | label | guess
	Notes   []string          `json:"notes"`
}

// AnalyzeDocument pre-fills an invoice from recognised text.
func (s *Service) AnalyzeDocument(in AnalyzeInputDTO) (AnalysisDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return AnalysisDTO{}, err
	}
	d := extract.Analyze(in.Lines, in.QR, s.now())
	inv := draftInvoice(d)
	out := AnalysisDTO{Kind: d.Kind, Draft: s.detail(&data.Settings, &inv), Sources: d.Sources, Notes: d.Notes}
	if out.Notes == nil {
		out.Notes = []string{}
	}
	return out, nil
}

// draftInvoice is the invoice a recognised draft describes.
func draftInvoice(d extract.Draft) model.Invoice {
	return model.Invoice{Provider: d.Provider, Payee: d.Payee, IBAN: d.IBAN, Reference: d.Reference, Category: d.Category,
		InvoiceDate: d.InvoiceDate, ServiceFrom: d.ServiceFrom, ServiceTo: d.ServiceTo, Amount: d.Amount, Due: d.Due,
		Paid: model.Mark{Date: d.Paid}}
}

// CreateInvoiceFromInbox saves an invoice and moves the inbox document – all
// its pages, in order – to the documents folder, linked to the invoice.
func (s *Service) CreateInvoiceFromInbox(name string, d InvoiceDTO) (InvoiceDetailDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, data, err := s.ready()
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	pages, read, err := readInboxPages(st, name)
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	inv, err := fromDTO(d, &data.Settings, s.newID)
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	if err := validateInvoice(&inv); err != nil {
		return InvoiceDetailDTO{}, err
	}
	// The same file again: scanned twice, or the inbox file could not be
	// removed last time. A second invoice would count the costs twice.
	for _, p := range pages {
		for i := range data.Invoices {
			if other := &data.Invoices[i]; !other.Deleted && slices.Contains(other.Attachments, p.doc) {
				which := "Dieser Beleg ist"
				if len(pages) > 1 {
					which = fmt.Sprintf("Die Seite „%s“ ist", filepath.Base(p.name))
				}
				return InvoiceDetailDTO{}, fmt.Errorf("%s bereits erfasst (%s) – die Datei kann im Posteingang verworfen werden", which, invoiceLabel(other))
			}
		}
	}
	docs, err := storePages(st, pages)
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	inv.ID = s.newID()
	inv.Attachments = docs
	saved, err := st.SaveInvoice(inv)
	if err != nil {
		return InvoiceDetailDTO{}, fmt.Errorf("Speichern fehlgeschlagen: %w", err)
	}
	replaceInvoice(data, saved)
	// The invoice is saved. If the files cannot be removed (locked by the sync
	// tool), they stay in the inbox; using them again is caught above.
	_ = st.RemoveInboxPages(read)
	return s.detail(&data.Settings, &saved), nil
}

// documentName is store.DocumentName for the inbox file name, with a message for the user.
func documentName(content []byte, name string) (string, error) {
	doc, err := store.DocumentName(content, filepath.Ext(name))
	if errors.Is(err, store.ErrDocumentType) {
		return "", fmt.Errorf("Dateityp „%s“ wird als Beleg nicht unterstützt – nur PDF und Bilder", filepath.Ext(name))
	}
	return doc, err
}

// DocumentURL returns the webview URL of a stored document.
func (s *Service) DocumentURL(name string) string {
	return FilePrefix + "doc/" + urlEscape(filepath.Base(name))
}

// DocumentPath returns the file path of a stored document (to open it with the default app).
func (s *Service) DocumentPath(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, _, err := s.ready()
	if err != nil {
		return "", err
	}
	return st.DocumentPath(name)
}

// ServeFile serves inbox files and documents to the webview. It returns
// false for other paths (the caller passes them on).
//
//wails:ignore
func (s *Service) ServeFile(w http.ResponseWriter, r *http.Request) bool {
	rest, ok := strings.CutPrefix(r.URL.Path, FilePrefix)
	if !ok {
		return false
	}
	s.mu.Lock()
	st := s.st
	s.mu.Unlock()
	if st == nil {
		http.NotFound(w, r)
		return true
	}
	var path string
	var err error
	switch {
	case strings.HasPrefix(rest, "inbox/"):
		path, err = st.InboxPath(strings.TrimPrefix(rest, "inbox/"))
	case strings.HasPrefix(rest, "doc/"):
		path, err = st.DocumentPath(strings.TrimPrefix(rest, "doc/"))
	case strings.HasPrefix(rest, "staged/"):
		path, err = s.stagedPath(strings.TrimPrefix(rest, "staged/"))
	default:
		err = os.ErrNotExist
	}
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	if fi, err := os.Stat(path); err != nil || fi.IsDir() {
		http.NotFound(w, r)
		return true
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
	return true
}

// PhoneUploadDTO describes the open phone upload.
type PhoneUploadDTO struct {
	URL      string `json:"url"`
	QR       string `json:"qr"` // PNG as data URL
	Expires  string `json:"expires"`
	Received int    `json:"received"`
	LAN      bool   `json:"lan"` // false: no network address found, the phone cannot connect
}

// StartPhoneUpload opens the upload page for the phone (or returns the open one).
func (s *Service) StartPhoneUpload() (PhoneUploadDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, _, err := s.ready()
	if err != nil {
		return PhoneUploadDTO{}, err
	}
	if s.upload == nil || s.now().After(s.upload.Expires) {
		if s.upload != nil {
			_ = s.upload.Stop()
		}
		if s.upload, err = upload.Start(st.InboxDirPath(), phoneUploadTTL, s.now); err != nil {
			return PhoneUploadDTO{}, fmt.Errorf("Upload für das Handy konnte nicht gestartet werden: %w", err)
		}
	}
	png, err := qrcode.Encode(s.upload.URL, qrcode.Medium, 320)
	if err != nil {
		return PhoneUploadDTO{}, err
	}
	return PhoneUploadDTO{URL: s.upload.URL, QR: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		Expires: s.upload.Expires.Format("15:04"), Received: s.upload.Received(), LAN: upload.LANAddress() != ""}, nil
}

// StopPhoneUpload closes the upload page.
func (s *Service) StopPhoneUpload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.upload != nil {
		_ = s.upload.Stop()
		s.upload = nil
	}
}

// InboxTextDTO is the recognised text of an inbox document.
type InboxTextDTO struct {
	Name  string    `json:"name"`
	Lines []LineDTO `json:"lines"`
	QR    []string  `json:"qr"`
}

// InboxSuggestionDTO proposes to merge inbox documents (names in page order).
type InboxSuggestionDTO struct {
	Names  []string `json:"names"`
	Reason string   `json:"reason"` // "Seite 1 und 2 von 2", "gleiche Rechnungsnummer", …
}

// SuggestInboxGroups proposes which inbox documents are pages of one
// document – from their content only (page numbers, invoice numbers,
// continuation pages). texts are in inbox order.
func (s *Service) SuggestInboxGroups(texts []InboxTextDTO) []InboxSuggestionDTO {
	docs := make([]extract.DocText, len(texts))
	for i, t := range texts {
		docs[i] = extract.DocText{Name: t.Name, Lines: t.Lines, QR: t.QR}
	}
	out := []InboxSuggestionDTO{}
	for _, g := range extract.SuggestGroups(docs, s.now()) {
		out = append(out, InboxSuggestionDTO{Names: g.Names, Reason: g.Reason})
	}
	return out
}
