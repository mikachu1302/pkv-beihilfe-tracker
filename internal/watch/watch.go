// Package watch notices changes in the data folder made by someone else – a
// sync tool bringing changes from another device, the phone upload, or the
// user editing files – while the app is running. Relevant are record files
// (*.json) and any file in the inbox folder or in a folder inside it (the
// pages of a multi-page document).
//
// It watches the data folder, the inbox folder and the folders in it, the
// year folders and their record folders – nothing else, as on macOS every watched folder costs one
// file descriptor per file in it (fsnotify is not recursive, so new folders
// are added as they appear). It ignores hidden temp files and the app's own
// writes, and calls onChange once a burst of events has settled. A year or
// record folder that is removed or renamed also counts as a change.
package watch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// InboxDir is the inbox folder whose files – and the files of the folders
// in it – are all relevant.
const InboxDir = "Eingang"

// Watcher watches one data folder.
type Watcher struct {
	fs       *fsnotify.Watcher
	root     string
	debounce time.Duration
	own      func(path string) bool
	onChange func()

	// addMu orders adding folders and closing: on Windows, fsnotify's Add
	// blocks forever when it runs after Close.
	addMu  sync.Mutex
	closed bool

	mu      sync.Mutex
	timer   *time.Timer
	pending map[string]bool // changed paths since the last check (true = folder)
	force   bool            // something may have been missed (watcher error)
	done    chan struct{}
	wg      sync.WaitGroup
}

// Start watches root. own reports whether a changed path was written by the
// app itself (those events are ignored); onChange runs on its own goroutine.
func Start(root string, debounce time.Duration, own func(path string) bool, onChange func()) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{fs: fw, root: root, debounce: debounce, own: own, onChange: onChange, done: make(chan struct{}),
		pending: map[string]bool{}}
	if err := w.addTree(root, 0); err != nil {
		_ = fw.Close()
		return nil, err
	}
	w.wg.Add(1)
	go w.loop()
	return w, nil
}

// Close stops watching; pending notifications are dropped.
func (w *Watcher) Close() error {
	w.addMu.Lock()
	w.closed = true
	w.addMu.Unlock()
	close(w.done)
	err := w.fs.Close()
	w.wg.Wait()
	w.mu.Lock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.mu.Unlock()
	return err
}

var yearRe = regexp.MustCompile(`^\d{4}$`)

// wanted reports whether the folder name at depth (root = 0, year or inbox
// = 1, record folder or multi-page document = 2) is one to watch.
func wanted(name string, depth int) bool {
	switch {
	case hidden(name):
		return false
	case depth == 1:
		return name == InboxDir || yearRe.MatchString(name)
	case depth == 2:
		return true // folders inside a year folder (records) or the inbox (pages)
	}
	return depth == 0
}

// addTree watches dir and the wanted sub folders below it.
func (w *Watcher) addTree(dir string, depth int) error {
	w.addMu.Lock()
	if w.closed {
		w.addMu.Unlock()
		return nil
	}
	err := w.fs.Add(dir)
	w.addMu.Unlock()
	if err != nil {
		return err
	}
	if depth >= 2 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() && wanted(e.Name(), depth+1) {
			if err := w.addTree(filepath.Join(dir, e.Name()), depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func hidden(name string) bool { return strings.HasPrefix(name, ".") }

func (w *Watcher) depth(path string) int {
	rel, err := filepath.Rel(w.root, path)
	if err != nil || rel == "." {
		return 0
	}
	return len(strings.Split(rel, string(filepath.Separator)))
}

func (w *Watcher) loop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.fs.Events:
			if !ok {
				return
			}
			w.handle(ev)
		case _, ok := <-w.fs.Errors:
			if !ok {
				return
			}
			// Errors (e.g. queue overflow) mean we may have missed something.
			w.mu.Lock()
			w.force = true
			w.mu.Unlock()
			w.schedule()
		}
	}
}

func (w *Watcher) handle(ev fsnotify.Event) {
	name := filepath.Base(ev.Name)
	if hidden(name) || ev.Op == fsnotify.Chmod {
		return
	}
	d := w.depth(ev.Name)
	if ev.Has(fsnotify.Create) {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
			parent := filepath.Base(filepath.Dir(ev.Name))
			if d <= 2 && wanted(name, d) && (d < 2 || yearRe.MatchString(parent) || parent == InboxDir) {
				_ = w.addTree(ev.Name, d)
				// Files may have arrived before the folder was watched.
				w.note(ev.Name, true)
			}
			return
		}
	}
	if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
		// A year or record folder moved away (to the trash, or renamed by
		// hand): its records are gone. The app never removes folders.
		if d == 1 && yearRe.MatchString(name) || d == 2 && yearRe.MatchString(filepath.Base(filepath.Dir(ev.Name))) && filepath.Ext(name) == "" {
			w.mu.Lock()
			w.force = true
			w.mu.Unlock()
			w.schedule()
			return
		}
	}
	if !w.relevant(ev.Name) {
		return
	}
	w.note(ev.Name, false)
}

// note remembers a changed path. Whether it was the app's own change is
// checked only when the burst has settled: the app records its writes right
// after renaming a file into place, which can be after the event arrives.
func (w *Watcher) note(path string, dir bool) {
	w.mu.Lock()
	w.pending[path] = w.pending[path] || dir
	w.mu.Unlock()
	w.schedule()
}

// fire runs when a burst has settled: it reports a change if any noted path
// was not written by the app itself.
func (w *Watcher) fire() {
	w.mu.Lock()
	paths, force := w.pending, w.force
	w.pending, w.force = map[string]bool{}, false
	w.mu.Unlock()
	foreign := force
	for p, dir := range paths {
		if foreign {
			break
		}
		if dir {
			foreign = w.foreignFiles(p)
		} else {
			foreign = w.own == nil || !w.own(p)
		}
	}
	if foreign {
		w.onChange()
	}
}

// relevant: a record file, or any file in the inbox folder or in a folder
// inside it (also such a folder itself).
func (w *Watcher) relevant(path string) bool {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return true
	}
	inbox := filepath.Join(w.root, InboxDir)
	return filepath.Dir(path) == inbox || filepath.Dir(filepath.Dir(path)) == inbox
}

// foreignFiles reports whether dir (or its sub folders) holds record files
// that the app did not write itself.
func (w *Watcher) foreignFiles(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipAll
		}
		if !d.IsDir() && !hidden(d.Name()) && w.relevant(path) && (w.own == nil || !w.own(path)) {
			found = true
		}
		return nil
	})
	return found
}

// schedule (re)starts the debounce timer.
func (w *Watcher) schedule() {
	w.mu.Lock()
	defer w.mu.Unlock()
	select {
	case <-w.done:
		return
	default:
	}
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(w.debounce, func() {
		select {
		case <-w.done:
		default:
			w.fire()
		}
	})
}
