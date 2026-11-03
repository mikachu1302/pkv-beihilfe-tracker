package service

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/export"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/store"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/upload"
)

// sessionPrefix names the temporary folder of a running app (merged PDFs to
// open, documents added in the invoice form before it is saved). It lies
// outside the data folder, so nothing temporary is synced.
const sessionPrefix = "pkv-beihilfe-sitzung-"

// staleSession is the age after which a session folder left behind (the app
// crashed) is removed when the next session starts.
const staleSession = 24 * time.Hour

// sessionDir returns the temporary folder of this run, creating it on first
// use (caller holds the lock).
func (s *Service) sessionDir() (string, error) {
	if s.session != "" {
		if _, err := os.Stat(s.session); err == nil {
			return s.session, nil
		}
	}
	root := s.tmpRoot
	if root == "" {
		root = os.TempDir()
	}
	removeStaleSessions(root, s.now())
	dir, err := os.MkdirTemp(root, sessionPrefix)
	if err != nil {
		return "", fmt.Errorf("temporärer Ordner kann nicht angelegt werden: %w", err)
	}
	s.session = dir
	return dir, nil
}

// removeStaleSessions deletes session folders of earlier runs. Young ones
// are kept: they may belong to a second app window that is still open.
func removeStaleSessions(root string, now time.Time) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), sessionPrefix) {
			continue
		}
		if fi, err := e.Info(); err == nil && now.Sub(fi.ModTime()) > staleSession {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}

// removeSession deletes the temporary folder of this run (at shutdown).
func (s *Service) removeSession() {
	if s.session != "" {
		_ = os.RemoveAll(s.session)
		s.session = ""
	}
	s.staged = nil
}

// DocumentsPDF returns a file to open the stored documents with: the document
// itself if it is one, else a temporary PDF with all pages in order.
func (s *Service) DocumentsPDF(names []string) (string, error) {
	s.mu.Lock()
	st, _, err := s.ready()
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	if len(names) == 0 {
		s.mu.Unlock()
		return "", errors.New("keine Belege angegeben")
	}
	paths := make([]string, 0, len(names))
	for _, n := range names {
		p, err := st.DocumentPath(n)
		if err != nil {
			s.mu.Unlock()
			return "", err
		}
		paths = append(paths, p)
	}
	if len(paths) == 1 {
		s.mu.Unlock()
		return paths[0], nil
	}
	dir, err := s.sessionDir()
	s.mu.Unlock()
	if err != nil {
		return "", err
	}
	// Merging can take a moment: done without holding the lock.
	f, err := os.CreateTemp(dir, "Belege-*.pdf")
	if err != nil {
		return "", err
	}
	out := f.Name()
	_ = f.Close()
	n, err := export.MergeDocuments(paths, out)
	if err != nil {
		_ = os.Remove(out)
		return "", fmt.Errorf("Seiten konnten nicht zusammengefasst werden: %w", err)
	}
	if n < len(paths) {
		// Some pages (e.g. HEIC photos) cannot go into a PDF: better no PDF
		// than one that silently lacks pages.
		_ = os.Remove(out)
		return "", fmt.Errorf("%d von %d Seiten lassen sich nicht in ein PDF übernehmen – bitte einzeln öffnen", len(paths)-n, len(paths))
	}
	return out, nil
}

// maxDocumentSize is the largest document accepted in the invoice form – the
// same limit as the phone upload and drag & drop into the inbox.
var maxDocumentSize int64 = upload.MaxFileSize

// StagedDocDTO is a document added in the invoice form. It waits in the
// session folder until the form is saved (then it moves to belege/) or
// discarded.
type StagedDocDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"` // original file name
	Type string `json:"type"` // pdf, image
	URL  string `json:"url"`  // for the webview
	Path string `json:"path"` // to open it with the default app
	Size int64  `json:"size"`
}

type stagedDoc struct {
	name string // original file name
	path string
}

var stagedIDRe = regexp.MustCompile(`^[0-9a-f]{16}\.[a-z0-9]{2,5}$`)

// stage stores content in the session folder (caller holds the lock).
func (s *Service) stage(name string, content []byte) (StagedDocDTO, error) {
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, `\`, "/")))
	if int64(len(content)) > maxDocumentSize {
		return StagedDocDTO{}, fmt.Errorf("„%s“ ist größer als %d MB", name, maxDocumentSize>>20)
	}
	if len(content) == 0 {
		return StagedDocDTO{}, fmt.Errorf("„%s“ ist leer", name)
	}
	ext := strings.ToLower(filepath.Ext(name))
	if _, err := documentName(content, name); err != nil {
		return StagedDocDTO{}, err
	}
	dir, err := s.sessionDir()
	if err != nil {
		return StagedDocDTO{}, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "staged"), 0o700); err != nil {
		return StagedDocDTO{}, err
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return StagedDocDTO{}, err
	}
	id := hex.EncodeToString(b[:]) + ext
	path := filepath.Join(dir, "staged", id)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return StagedDocDTO{}, fmt.Errorf("„%s“ konnte nicht übernommen werden: %w", name, err)
	}
	if s.staged == nil {
		s.staged = map[string]stagedDoc{}
	}
	s.staged[id] = stagedDoc{name: name, path: path}
	return StagedDocDTO{ID: id, Name: name, Type: fileType(name), URL: FilePrefix + "staged/" + id, Path: path,
		Size: int64(len(content))}, nil
}

// StageFile takes a document dropped onto the invoice form (content as
// base64, optionally as data URL).
func (s *Service) StageFile(name, data string) (StagedDocDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, _, err := s.ready(); err != nil {
		return StagedDocDTO{}, err
	}
	if i := strings.Index(data, ","); strings.HasPrefix(data, "data:") && i > 0 {
		data = data[i+1:]
	}
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return StagedDocDTO{}, errors.New("Datei konnte nicht gelesen werden")
	}
	return s.stage(name, b)
}

// StageFiles takes documents chosen in the file dialog of the invoice form.
func (s *Service) StageFiles(paths []string) ([]StagedDocDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, _, err := s.ready(); err != nil {
		return nil, err
	}
	out := []StagedDocDTO{}
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && fi.Size() > maxDocumentSize {
			return out, fmt.Errorf("„%s“ ist größer als %d MB", filepath.Base(p), maxDocumentSize>>20)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return out, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		d, err := s.stage(filepath.Base(p), b)
		if err != nil {
			return out, err
		}
		out = append(out, d)
	}
	return out, nil
}

// DiscardStaged forgets documents added in a form that was not saved.
func (s *Service) DiscardStaged(ids []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discardStaged(ids)
}

// discardStaged is DiscardStaged for a caller holding the lock.
func (s *Service) discardStaged(ids []string) {
	for _, id := range ids {
		if d, ok := s.staged[id]; ok {
			_ = os.Remove(d.path)
			delete(s.staged, id)
		}
	}
}

// StagedToInbox moves documents added in the invoice form into the inbox –
// a Bescheid is booked from there. Several documents become the pages of
// one inbox document. It returns the inbox name.
func (s *Service) StagedToInbox(ids []string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, _, err := s.ready()
	if err != nil {
		return "", err
	}
	var names []string
	for _, id := range ids {
		d, ok := s.staged[id]
		if !ok {
			return "", errStagedGone
		}
		b, err := os.ReadFile(d.path)
		if err != nil {
			return "", errStagedGone
		}
		n, err := st.AddToInbox(d.name, b)
		if err != nil {
			return "", err
		}
		names = append(names, n)
		_ = os.Remove(d.path)
		delete(s.staged, id)
	}
	switch len(names) {
	case 0:
		return "", nil
	case 1:
		return names[0], nil
	}
	return st.MergeInbox(names)
}

var errStagedGone = errors.New("Ein hinzugefügter Beleg ist nicht mehr verfügbar – bitte erneut hinzufügen")

// stagedPath returns the file of a staged document for the webview.
func (s *Service) stagedPath(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !stagedIDRe.MatchString(id) {
		return "", os.ErrNotExist
	}
	d, ok := s.staged[id]
	if !ok {
		return "", os.ErrNotExist
	}
	return d.path, nil
}

// pendingDoc is a staged document on its way into belege/.
type pendingDoc struct {
	id, name, doc string // staged ID, original name, name in belege/
	content       []byte
}

// takeStaged reads the staged documents for inv and checks them: a document
// of another invoice is refused (it would count the costs twice), one the
// invoice already has is skipped (caller holds the lock).
func (s *Service) takeStaged(data *store.Data, inv *model.Invoice, ids []string) ([]pendingDoc, error) {
	var out []pendingDoc
	seen := map[string]bool{}
	for _, a := range inv.Attachments {
		seen[a] = true
	}
	for _, id := range ids {
		d, ok := s.staged[id]
		if !ok {
			return nil, errStagedGone
		}
		b, err := os.ReadFile(d.path)
		if err != nil {
			return nil, errStagedGone
		}
		doc, err := documentName(b, d.name)
		if err != nil {
			return nil, err
		}
		for i := range data.Invoices {
			if other := &data.Invoices[i]; other.ID != inv.ID && !other.Deleted && slices.Contains(other.Attachments, doc) {
				return nil, fmt.Errorf("„%s“ gehört bereits zu einer anderen Rechnung (%s)", d.name, invoiceLabel(other))
			}
		}
		if seen[doc] {
			continue
		}
		seen[doc] = true
		out = append(out, pendingDoc{id: id, name: d.name, doc: doc, content: b})
	}
	return out, nil
}

// documentInUse reports whether a record other than the invoice except
// refers to the stored document. Deleted invoices count: their documents
// are kept with them.
func documentInUse(data *store.Data, doc, except string) bool {
	for i := range data.Invoices {
		if inv := &data.Invoices[i]; inv.ID != except && slices.Contains(inv.Attachments, doc) {
			return true
		}
	}
	for _, d := range data.RefundDocs {
		if slices.Contains(d.Pages(), doc) {
			return true
		}
	}
	return false
}

var unsafeNameChars = strings.NewReplacer("/", "-", `\`, "-", ":", "-", "*", "", "?", "", `"`, "", "<", "", ">", "", "|", "-")

// backToInbox moves documents an invoice no longer uses into the inbox, so
// that nothing is lost: from there they can be attached elsewhere or
// discarded. It returns how many were moved (caller holds the lock).
func backToInbox(st *store.Store, data *store.Data, inv *model.Invoice, removed []string) int {
	var free []string
	for _, doc := range removed {
		if !documentInUse(data, doc, inv.ID) {
			free = append(free, doc)
		}
	}
	base := strings.TrimSpace(unsafeNameChars.Replace(inv.Provider))
	if base == "" {
		base = "Beleg"
	}
	if !inv.InvoiceDate.IsZero() {
		base += " " + inv.InvoiceDate.String()
	}
	moved := 0
	for i, doc := range free {
		path, err := st.DocumentPath(doc)
		if err != nil {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			continue // already gone
		}
		name := base
		if len(free) > 1 {
			name += fmt.Sprintf(" Seite %d", i+1)
		}
		if _, err := st.AddToInbox(name+filepath.Ext(doc), b); err != nil {
			continue
		}
		_ = st.RemoveDocument(doc)
		moved++
	}
	return moved
}
