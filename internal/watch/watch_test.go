package watch

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

const debounce = 60 * time.Millisecond

func start(t *testing.T, root string, own func(string) bool) (*atomic.Int32, chan struct{}) {
	return startWith(t, root, own, debounce)
}

func startWith(t *testing.T, root string, own func(string) bool, wait time.Duration) (*atomic.Int32, chan struct{}) {
	t.Helper()
	var n atomic.Int32
	ch := make(chan struct{}, 10)
	w, err := Start(root, wait, own, func() {
		n.Add(1)
		ch <- struct{}{}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return &n, ch
}

func wait(t *testing.T, ch chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("no change reported")
	}
}

func quiet(t *testing.T, ch chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("unexpected change reported")
	case <-time.After(4 * debounce):
	}
}

func TestReportsExternalChangesOnce(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "2026", "rechnungen")
	os.MkdirAll(dir, 0o755)
	// A longer quiet period so the burst settles even on a slow CI runner.
	n, ch := startWith(t, root, nil, 300*time.Millisecond)
	for i := 0; i < 5; i++ { // a burst from a sync tool
		os.WriteFile(filepath.Join(dir, "a.json"), []byte(`{}`), 0o644)
	}
	wait(t, ch)
	select {
	case <-ch:
		t.Fatal("burst reported twice")
	case <-time.After(time.Second):
	}
	if n.Load() != 1 {
		t.Errorf("reported %d times, want once per burst", n.Load())
	}
	os.Remove(filepath.Join(dir, "a.json"))
	wait(t, ch)
}

func TestIgnoresOwnWritesTempFilesAndOtherFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "2026", "rechnungen")
	os.MkdirAll(dir, 0o755)
	ownPath := filepath.Join(dir, "mine.json")
	_, ch := start(t, root, func(p string) bool { return p == ownPath })
	os.WriteFile(ownPath, []byte(`{}`), 0o644)
	os.WriteFile(filepath.Join(dir, ".tmp-123"), []byte(`x`), 0o644)
	os.WriteFile(filepath.Join(root, "notizen.txt"), []byte(`x`), 0o644)
	quiet(t, ch)
}

func TestFollowsNewFolders(t *testing.T) {
	root := t.TempDir()
	_, ch := start(t, root, nil)
	dir := filepath.Join(root, "2027", "rechnungen")
	os.MkdirAll(dir, 0o755)
	quiet(t, ch) // an empty folder is no change
	os.WriteFile(filepath.Join(dir, "b.json"), []byte(`{}`), 0o644)
	wait(t, ch)
}

func TestNewFolderWithFiles(t *testing.T) {
	root := t.TempDir()
	mine := filepath.Join(root, "2028", "rechnungen", "mine.json")
	_, ch := start(t, root, func(p string) bool { return p == mine })

	// A sync tool moves in a whole year folder with another device's file.
	src := filepath.Join(t.TempDir(), "2027")
	os.MkdirAll(filepath.Join(src, "rechnungen"), 0o755)
	os.WriteFile(filepath.Join(src, "rechnungen", "x.json"), []byte(`{}`), 0o644)
	if err := os.Rename(src, filepath.Join(root, "2027")); err != nil {
		t.Skip("cannot move across file systems:", err)
	}
	wait(t, ch)

	// The app itself creates a year folder with its own file: no change.
	os.MkdirAll(filepath.Dir(mine), 0o755)
	os.WriteFile(mine, []byte(`{}`), 0o644)
	quiet(t, ch)
}

func TestInboxFilesAreRelevant(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, InboxDir)
	os.MkdirAll(inbox, 0o755)
	os.MkdirAll(filepath.Join(root, "belege"), 0o755)
	_, ch := start(t, root, nil)
	os.WriteFile(filepath.Join(root, "belege", "abc.pdf"), []byte("%PDF"), 0o644)
	quiet(t, ch) // documents folder: not relevant
	os.WriteFile(filepath.Join(inbox, "Handy-1.jpg"), []byte("jpg"), 0o644)
	wait(t, ch)
}

func TestPagesInInboxFoldersAreRelevant(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, InboxDir)
	existing := filepath.Join(inbox, "Scan")
	os.MkdirAll(existing, 0o755)
	_, ch := start(t, root, nil)
	// A page arrives in an existing multi-page document.
	os.WriteFile(filepath.Join(existing, "02 Seite.jpg"), []byte("jpg"), 0o644)
	wait(t, ch)
	// A new folder with pages (the phone, or a sync tool).
	group := filepath.Join(inbox, "Handy-20261103")
	os.MkdirAll(group, 0o755)
	os.WriteFile(filepath.Join(group, "01 Seite.jpg"), []byte("jpg"), 0o644)
	wait(t, ch)
	// A page added later to the new folder is seen too (the folder is watched).
	time.Sleep(2 * debounce)
	os.WriteFile(filepath.Join(group, "02 Seite.jpg"), []byte("jpg"), 0o644)
	wait(t, ch)
	// Deeper folders are not watched.
	deep := filepath.Join(group, "tiefer")
	os.MkdirAll(deep, 0o755)
	time.Sleep(2 * debounce)
	drain(ch)
	os.WriteFile(filepath.Join(deep, "x.jpg"), []byte("jpg"), 0o644)
	quiet(t, ch)
}

// drain forgets changes reported so far.
func drain(ch chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func TestYearFolderRemovedIsAChange(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "2025", "rechnungen")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "a.json"), []byte(`{}`), 0o644)
	_, ch := start(t, root, nil)
	// Moved to the trash by hand: rename out of the data folder.
	if err := os.Rename(filepath.Join(root, "2025"), filepath.Join(t.TempDir(), "2025")); err != nil {
		t.Skip("cannot move across file systems:", err)
	}
	wait(t, ch)
}

func TestOtherFoldersAreNotWatched(t *testing.T) {
	root := t.TempDir()
	own := filepath.Join(root, "Privat")
	os.MkdirAll(filepath.Join(own, "2026"), 0o755)
	_, ch := start(t, root, nil)
	os.WriteFile(filepath.Join(own, "notizen.json"), []byte(`{}`), 0o644)
	os.WriteFile(filepath.Join(own, "2026", "x.json"), []byte(`{}`), 0o644)
	quiet(t, ch)
}
