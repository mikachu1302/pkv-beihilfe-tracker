package service

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/extract"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/upload"
)

// FilePrefix is the URL path under which the webview gets data folder files:
// FilePrefix+"inbox/<name>" and FilePrefix+"doc/<name>".
const FilePrefix = "/datafile/"

// phoneUploadTTL is how long the phone upload stays open at most.
const phoneUploadTTL = 15 * time.Minute

// InboxItemDTO is a file waiting in the inbox folder.
type InboxItemDTO struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"` // "2026-11-02 10:15"
	Type     string `json:"type"`     // pdf, image, other
	URL      string `json:"url"`      // for the webview
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
	files, err := st.ListInbox()
	if err != nil {
		return nil, err
	}
	out := []InboxItemDTO{}
	for _, f := range files {
		out = append(out, InboxItemDTO{Name: f.Name, Size: f.Size, Modified: f.Modified.Format("2006-01-02 15:04"),
			Type: fileType(f.Name), URL: FilePrefix + "inbox/" + urlEscape(f.Name)})
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

// DiscardInbox deletes a file from the inbox folder.
func (s *Service) DiscardInbox(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, _, err := s.ready()
	if err != nil {
		return err
	}
	return st.RemoveFromInbox(name)
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
	inv := model.Invoice{Provider: d.Provider, Payee: d.Payee, IBAN: d.IBAN, Reference: d.Reference, Category: d.Category,
		InvoiceDate: d.InvoiceDate, ServiceFrom: d.ServiceFrom, ServiceTo: d.ServiceTo, Amount: d.Amount, Due: d.Due}
	out := AnalysisDTO{Kind: d.Kind, Draft: s.detail(&data.Settings, &inv), Sources: d.Sources, Notes: d.Notes}
	if out.Notes == nil {
		out.Notes = []string{}
	}
	return out, nil
}

// CreateInvoiceFromInbox saves an invoice and moves the inbox file to the
// documents folder, linked to the invoice.
func (s *Service) CreateInvoiceFromInbox(name string, d InvoiceDTO) (InvoiceDetailDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, data, err := s.ready()
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	b, err := os.ReadFile(st.InboxPath(name))
	if err != nil {
		return InvoiceDetailDTO{}, fmt.Errorf("Datei im Posteingang nicht gefunden: %w", err)
	}
	inv, err := fromDTO(d, &data.Settings, s.newID)
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	if err := validateInvoice(&inv); err != nil {
		return InvoiceDetailDTO{}, err
	}
	doc, err := st.AddDocument(bytes.NewReader(b), strings.ToLower(filepath.Ext(name)))
	if err != nil {
		return InvoiceDetailDTO{}, fmt.Errorf("Beleg konnte nicht abgelegt werden: %w", err)
	}
	inv.ID = s.newID()
	inv.Attachments = append(inv.Attachments, doc)
	saved, err := st.SaveInvoice(inv)
	if err != nil {
		return InvoiceDetailDTO{}, fmt.Errorf("Speichern fehlgeschlagen: %w", err)
	}
	replaceInvoice(data, saved)
	if err := st.RemoveFromInbox(name); err != nil {
		return InvoiceDetailDTO{}, err
	}
	return s.detail(&data.Settings, &saved), nil
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
	return st.DocumentPath(name), nil
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
	switch {
	case strings.HasPrefix(rest, "inbox/"):
		path = st.InboxPath(strings.TrimPrefix(rest, "inbox/"))
	case strings.HasPrefix(rest, "doc/"):
		path = st.DocumentPath(strings.TrimPrefix(rest, "doc/"))
	default:
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
		if s.upload, err = upload.Start(filepath.Dir(st.InboxPath("x")), phoneUploadTTL, s.now); err != nil {
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
