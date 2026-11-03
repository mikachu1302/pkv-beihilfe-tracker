package store

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func inboxNames(t *testing.T, s *Store) []string {
	t.Helper()
	list, err := s.ListInbox()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range list {
		name := e.Name
		if e.Group {
			name += "["
			for i, p := range e.Pages {
				if i > 0 {
					name += ","
				}
				name += filepath.Base(p.Name)
			}
			name += "]"
		}
		out = append(out, name)
	}
	return out
}

// addAt adds an inbox file with a modification time, so the order is known.
func addAt(t *testing.T, s *Store, name string, minute int) string {
	t.Helper()
	n, err := s.AddToInbox(name, []byte("content of "+name))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 3, 10, minute, 0, 0, time.UTC)
	p, _ := s.InboxPath(n)
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMergeAndSplitInbox(t *testing.T) {
	s := open(t, t.TempDir(), "a", "2026-11-03T10:00:00Z")
	addAt(t, s, "Scan 1.jpg", 1)
	addAt(t, s, "Scan 2.jpg", 2)
	addAt(t, s, "Scan 3.jpg", 3)
	addAt(t, s, "Brief.pdf", 4)

	g, err := s.MergeInbox([]string{"Scan 1.jpg", "Scan 3.jpg", "Scan 2.jpg"})
	if err != nil || g != "Scan 1" {
		t.Fatalf("merge: %q, %v", g, err)
	}
	want := []string{"Scan 1[01 Scan 1.jpg,02 Scan 3.jpg,03 Scan 2.jpg]", "Brief.pdf"}
	if got := inboxNames(t, s); !slices.Equal(got, want) {
		t.Errorf("after merge %v, want %v", got, want)
	}
	// Moved pages count as the app's own changes (no reload, no "new document").
	for _, p := range []string{"Scan 1.jpg", "Scan 1/02 Scan 3.jpg"} {
		if path, _ := s.InboxPath(p); !s.IsOwnChange(path) {
			t.Errorf("%s not recognised as own change", p)
		}
	}

	// Reorder: all pages in a new order.
	if err := s.ReorderInbox("Scan 1", []string{"Scan 1/01 Scan 1.jpg", "Scan 1/03 Scan 2.jpg", "Scan 1/02 Scan 3.jpg"}); err != nil {
		t.Fatal(err)
	}
	if got := inboxNames(t, s)[0]; got != "Scan 1[01 Scan 1.jpg,02 Scan 2.jpg,03 Scan 3.jpg]" {
		t.Errorf("after reorder %s", got)
	}
	if err := s.ReorderInbox("Scan 1", []string{"Scan 1/01 Scan 1.jpg"}); err == nil {
		t.Error("incomplete order accepted")
	}

	// A further document joins the group at the end.
	if _, err := s.MergeInbox([]string{"Scan 1", "Brief.pdf"}); err != nil {
		t.Fatal(err)
	}
	if got := inboxNames(t, s); len(got) != 1 || got[0] != "Scan 1[01 Scan 1.jpg,02 Scan 2.jpg,03 Scan 3.jpg,04 Brief.pdf]" {
		t.Errorf("after joining %v", got)
	}

	// Take one page out: it is a document of its own again, the rest is renumbered.
	if err := s.SplitInbox("Scan 1/02 Scan 2.jpg"); err != nil {
		t.Fatal(err)
	}
	got := inboxNames(t, s)
	if !slices.Contains(got, "Scan 2.jpg") || !slices.Contains(got, "Scan 1[01 Scan 1.jpg,02 Scan 3.jpg,03 Brief.pdf]") {
		t.Errorf("after taking a page out %v", got)
	}
	// Splitting all pages dissolves the folder.
	if err := s.SplitInbox("Scan 1"); err != nil {
		t.Fatal(err)
	}
	got = inboxNames(t, s)
	slices.Sort(got)
	if !slices.Equal(got, []string{"Brief.pdf", "Scan 1.jpg", "Scan 2.jpg", "Scan 3.jpg"}) {
		t.Errorf("after split %v", got)
	}
	if _, err := os.Stat(filepath.Join(s.InboxDirPath(), "Scan 1")); !os.IsNotExist(err) {
		t.Error("folder left behind")
	}
	if _, err := s.MergeInbox([]string{"Scan 1.jpg"}); err == nil {
		t.Error("merging one entry accepted")
	}
	if _, err := s.MergeInbox([]string{"Scan 1.jpg", "fehlt.jpg"}); err == nil {
		t.Error("missing entry accepted")
	}
}

func TestPagesWithTheSameName(t *testing.T) {
	// Two phone photos named alike: renumbering must not let them collide.
	s := open(t, t.TempDir(), "a", "2026-11-03T10:00:00Z")
	dir := filepath.Join(s.InboxDirPath(), "Handy")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "01 Seite.jpg"), []byte("eins"), 0o644)
	os.WriteFile(filepath.Join(dir, "02 Seite.jpg"), []byte("zwei"), 0o644)
	if err := s.ReorderInbox("Handy", []string{"Handy/02 Seite.jpg", "Handy/01 Seite.jpg"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "01 Seite.jpg"))
	if string(b) != "zwei" {
		t.Errorf("page 1 holds %q", b)
	}
	// Taking one out of two dissolves the group; both get free names.
	if err := s.SplitInbox("Handy/01 Seite.jpg"); err != nil {
		t.Fatal(err)
	}
	got := inboxNames(t, s)
	slices.Sort(got)
	if !slices.Equal(got, []string{"Seite (2).jpg", "Seite.jpg"}) {
		t.Errorf("after split %v", got)
	}
}

func TestHandMadeFolders(t *testing.T) {
	s := open(t, t.TempDir(), "a", "2026-11-03T10:00:00Z")
	dir := filepath.Join(s.InboxDirPath(), "Rechnung Zahnarzt")
	os.MkdirAll(filepath.Join(dir, "tiefer"), 0o755)
	for _, n := range []string{"Seite 10.jpg", "Seite 2.jpg", "Seite 1.jpg", ".DS_Store"} {
		os.WriteFile(filepath.Join(dir, n), []byte(n), 0o644)
	}
	os.WriteFile(filepath.Join(dir, "tiefer", "x.jpg"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(s.InboxDirPath(), "leer"), 0o755)
	if got := inboxNames(t, s); !slices.Equal(got, []string{"Rechnung Zahnarzt[Seite 1.jpg,Seite 2.jpg,Seite 10.jpg]"}) {
		t.Errorf("list %v", got)
	}
	e, err := s.InboxEntry("Rechnung Zahnarzt")
	if err != nil || !e.Group || len(e.Pages) != 3 || e.Pages[0].Name != "Rechnung Zahnarzt/Seite 1.jpg" {
		t.Errorf("entry %+v, %v", e, err)
	}
	// Removing the entry removes its pages and system junk, never what the
	// inbox does not show (the deeper folder).
	os.WriteFile(filepath.Join(dir, "Thumbs.db"), []byte("x"), 0o644)
	if got := inboxNames(t, s); len(got) != 1 || strings.Contains(got[0], "Thumbs") {
		t.Errorf("junk listed: %v", got)
	}
	if err := s.RemoveFromInbox("Rechnung Zahnarzt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tiefer", "x.jpg")); err != nil {
		t.Error("deeper folder removed")
	}
	if got := inboxNames(t, s); len(got) != 0 {
		t.Errorf("after removing %v", got)
	}
}

func TestRemoveOnlyTheReadPages(t *testing.T) {
	// A page arriving after the pages were read (phone, sync) is kept.
	s := open(t, t.TempDir(), "a", "2026-11-03T10:00:00Z")
	dir := filepath.Join(s.InboxDirPath(), "Handy")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "01 Seite.jpg"), []byte("1"), 0o644)
	os.WriteFile(filepath.Join(dir, "02 Seite.jpg"), []byte("2"), 0o644)
	e, _ := s.InboxEntry("Handy")
	os.WriteFile(filepath.Join(dir, "03 Seite.jpg"), []byte("3"), 0o644) // arrives now
	os.WriteFile(filepath.Join(dir, ".upload-123"), []byte("4"), 0o644)  // still being written
	var read []string
	for _, p := range e.Pages {
		read = append(read, p.Name)
	}
	if err := s.RemoveInboxPages(read); err != nil {
		t.Fatal(err)
	}
	if got := inboxNames(t, s); !slices.Equal(got, []string{"Handy[03 Seite.jpg]"}) {
		t.Errorf("after removing the read pages %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".upload-123")); err != nil {
		t.Error("file being written removed")
	}
}

func TestReorderNeverOverwrites(t *testing.T) {
	s := open(t, t.TempDir(), "a", "2026-11-03T10:00:00Z")
	dir := filepath.Join(s.InboxDirPath(), "Scan")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "01 a.jpg"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "02 b.jpg"), []byte("b"), 0o644)
	// Moving b to the front while a file named like the target exists already.
	files := []string{filepath.Join(dir, "02 b.jpg"), filepath.Join(dir, "01 a.jpg")}
	os.WriteFile(filepath.Join(dir, "01 b.jpg"), []byte("neu"), 0o644)
	if err := s.placePages(dir, files); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "01 b.jpg")); string(b) != "neu" {
		t.Errorf("existing file replaced: %q", b)
	}
}

func TestRemovePageOfGroup(t *testing.T) {
	s := open(t, t.TempDir(), "a", "2026-11-03T10:00:00Z")
	addAt(t, s, "a.jpg", 1)
	addAt(t, s, "b.jpg", 2)
	g, _ := s.MergeInbox([]string{"a.jpg", "b.jpg"})
	if err := s.RemoveFromInbox(g + "/01 a.jpg"); err != nil {
		t.Fatal(err)
	}
	if got := inboxNames(t, s); !slices.Equal(got, []string{"a[02 b.jpg]"}) {
		t.Errorf("after removing a page %v", got)
	}
	if err := s.RemoveFromInbox(g + "/02 b.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.InboxDirPath(), g)); !os.IsNotExist(err) {
		t.Error("empty folder left behind")
	}
}

func TestInterruptedRenumbering(t *testing.T) {
	s := open(t, t.TempDir(), "a", "2026-11-03T10:00:00Z")
	dir := filepath.Join(s.InboxDirPath(), "Scan")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, renamePrefix+"0-01 a.jpg"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "02 b.jpg"), []byte("b"), 0o644)
	if got := inboxNames(t, s); !slices.Equal(got, []string{"Scan[01 a.jpg,02 b.jpg]"}) {
		t.Errorf("list %v", got)
	}
}

func TestNaturalCompare(t *testing.T) {
	names := []string{"Seite 10", "seite 2", "Seite 1", "Übersicht", "Anlage"}
	slices.SortFunc(names, naturalCompare)
	if want := []string{"Anlage", "Seite 1", "seite 2", "Seite 10", "Übersicht"}; !slices.Equal(names, want) {
		t.Errorf("%v, want %v", names, want)
	}
}
