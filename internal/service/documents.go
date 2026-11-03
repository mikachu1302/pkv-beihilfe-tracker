package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/export"
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
