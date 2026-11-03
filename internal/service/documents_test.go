package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
