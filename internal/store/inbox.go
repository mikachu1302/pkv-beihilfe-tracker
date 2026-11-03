package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The inbox folder holds documents waiting to be booked. A file is a
// document of its own; a folder inside it is a document of several pages –
// its visible files, in name order. The app names pages "01 <name>",
// "02 <name>", …; folders made by hand may name them freely ("Seite 2"
// sorts before "Seite 10"). Deeper folders are ignored.

// InboxFile is a file waiting in the inbox folder.
type InboxFile struct {
	Name     string // relative to the inbox: "scan.pdf" or "Scan 12/01 Scan 12.jpg"
	Size     int64
	Modified time.Time
}

// InboxEntry is a document waiting in the inbox: a file, or a folder with
// the pages of one document.
type InboxEntry struct {
	Name     string // file or folder name
	Group    bool
	Pages    []InboxFile
	Added    time.Time // oldest page: the order of the inbox
	Modified time.Time // newest page
}

// ErrInvalidName means a file name is empty, hidden or a path.
var ErrInvalidName = errors.New("ungültiger Dateiname")

// ErrNotInInbox means an inbox entry or page does not exist (any more).
var ErrNotInInbox = errors.New("nicht (mehr) im Posteingang")

// fileName checks that name is a plain, visible file name.
func fileName(name string) (string, error) {
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	return name, nil
}

// inboxRel checks an inbox name: "file" or "folder/file".
func inboxRel(name string) (string, error) {
	parts := strings.Split(name, "/")
	if len(parts) > 2 {
		return "", fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	for _, p := range parts {
		if _, err := fileName(p); err != nil {
			return "", fmt.Errorf("%w: %q", ErrInvalidName, name)
		}
	}
	return filepath.Join(parts...), nil
}

// InboxDirPath returns the inbox folder.
func (s *Store) InboxDirPath() string { return filepath.Join(s.root, InboxDir) }

// InboxPath returns the full path of an inbox file ("file" or "folder/file").
func (s *Store) InboxPath(name string) (string, error) {
	rel, err := inboxRel(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.InboxDirPath(), rel), nil
}

// junkFiles are files the operating system puts into folders by itself
// (hidden on Windows and macOS by attribute, not by a leading dot).
var junkFiles = map[string]bool{"thumbs.db": true, "desktop.ini": true, "icon\r": true, ".ds_store": true, "ehthumbs.db": true}

func isJunk(name string) bool { return junkFiles[strings.ToLower(name)] }

// visible reports whether an inbox file is a document (not hidden, no system junk).
func visible(name string) bool { return !strings.HasPrefix(name, ".") && !isJunk(name) }

// visibleFiles returns the visible files directly in dir, in natural name order.
func visibleFiles(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := slices.DeleteFunc(entries, func(e os.DirEntry) bool { return e.IsDir() || !visible(e.Name()) })
	slices.SortStableFunc(out, func(a, b os.DirEntry) int { return naturalCompare(a.Name(), b.Name()) })
	return out, nil
}

// ListInbox returns the documents in the inbox folder, oldest first. Empty
// folders are left out.
func (s *Store) ListInbox() ([]InboxEntry, error) {
	entries, err := os.ReadDir(s.InboxDirPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []InboxEntry
	for _, e := range entries {
		if !visible(e.Name()) {
			continue
		}
		if !e.IsDir() {
			if fi, err := e.Info(); err == nil {
				f := InboxFile{Name: e.Name(), Size: fi.Size(), Modified: fi.ModTime()}
				out = append(out, InboxEntry{Name: e.Name(), Pages: []InboxFile{f}, Added: f.Modified, Modified: f.Modified})
			}
			continue
		}
		if g, ok := s.group(e.Name()); ok {
			out = append(out, g)
		}
	}
	slices.SortStableFunc(out, func(a, b InboxEntry) int {
		if c := a.Added.Compare(b.Added); c != 0 {
			return c
		}
		return naturalCompare(a.Name, b.Name)
	})
	return out, nil
}

// group reads the folder of a multi-page document; false if it has no pages.
func (s *Store) group(name string) (InboxEntry, bool) {
	dir := filepath.Join(s.InboxDirPath(), name)
	s.finishRenames(dir)
	files, err := visibleFiles(dir)
	if err != nil || len(files) == 0 {
		return InboxEntry{}, false
	}
	g := InboxEntry{Name: name, Group: true}
	for _, f := range files {
		fi, err := f.Info()
		if err != nil {
			continue
		}
		g.Pages = append(g.Pages, InboxFile{Name: name + "/" + f.Name(), Size: fi.Size(), Modified: fi.ModTime()})
		if g.Added.IsZero() || fi.ModTime().Before(g.Added) {
			g.Added = fi.ModTime()
		}
		if fi.ModTime().After(g.Modified) {
			g.Modified = fi.ModTime()
		}
	}
	return g, len(g.Pages) > 0
}

// InboxEntry returns one document of the inbox by its name.
func (s *Store) InboxEntry(name string) (InboxEntry, error) {
	if _, err := fileName(name); err != nil {
		return InboxEntry{}, err
	}
	path := filepath.Join(s.InboxDirPath(), name)
	fi, err := os.Stat(path)
	if err != nil {
		return InboxEntry{}, fmt.Errorf("„%s“ %w", name, ErrNotInInbox)
	}
	if !fi.IsDir() {
		f := InboxFile{Name: name, Size: fi.Size(), Modified: fi.ModTime()}
		return InboxEntry{Name: name, Pages: []InboxFile{f}, Added: f.Modified, Modified: f.Modified}, nil
	}
	g, ok := s.group(name)
	if !ok {
		return InboxEntry{}, fmt.Errorf("„%s“ %w", name, ErrNotInInbox)
	}
	return g, nil
}

// freeName returns a path in dir for name that is not taken yet
// ("Scan.jpg", "Scan (2).jpg", …).
func freeName(dir, name string) string {
	base, ext := strings.TrimSuffix(name, filepath.Ext(name)), filepath.Ext(name)
	path := filepath.Join(dir, name)
	for i := 2; ; i++ {
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			return path
		}
		path = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
	}
}

// AddToInbox writes a file into the inbox folder under a free name and returns it.
func (s *Store) AddToInbox(name string, data []byte) (string, error) {
	name = strings.TrimSpace(name[strings.LastIndexAny(name, `/\`)+1:]) // also Windows paths
	if name == "" || strings.HasPrefix(name, ".") {
		name = "Beleg" + filepath.Ext(name)
	}
	path := freeName(s.InboxDirPath(), name)
	if err := writeFileAtomic(path, data); err != nil {
		return "", err
	}
	s.noteOwn(path)
	return filepath.Base(path), nil
}

// RemoveFromInbox deletes a document of the inbox – a file, or the pages a
// folder holds now – or one page ("folder/file"). Callers that read the
// pages before (booking, discarding what the user saw) use RemoveInboxPages,
// so that a page arriving in between is kept.
func (s *Store) RemoveFromInbox(name string) error {
	path, err := s.InboxPath(name)
	if err != nil {
		return err
	}
	fi, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case fi.IsDir():
		e, err := s.InboxEntry(name)
		if err != nil {
			return nil // no pages (any more)
		}
		pages := make([]string, len(e.Pages))
		for i, p := range e.Pages {
			pages[i] = p.Name
		}
		return s.RemoveInboxPages(pages)
	}
	return s.RemoveInboxPages([]string{name})
}

// RemoveInboxPages deletes exactly these inbox files (names as ListInbox
// gives them). A page folder goes when nothing but system junk is left in it;
// anything else in it – pages that arrived meanwhile, files being synced,
// deeper folders – is kept.
func (s *Store) RemoveInboxPages(names []string) error {
	dirs := map[string]bool{}
	for _, n := range names {
		path, err := s.InboxPath(n)
		if err != nil {
			return err
		}
		if err := s.remove(path); err != nil {
			return err
		}
		if dir := filepath.Dir(path); dir != s.InboxDirPath() {
			dirs[dir] = true
		}
	}
	for dir := range dirs {
		s.removeEmptyDir(dir)
	}
	return nil
}

// removeEmptyDir deletes a page folder if nothing but system junk is left
// in it (never with its contents: a file that arrives meanwhile stays).
func (s *Store) removeEmptyDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !isJunk(e.Name()) {
			return
		}
	}
	for _, e := range entries {
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
	if os.Remove(dir) == nil {
		s.markRemoved(dir)
	}
}

// pagePrefix is the number the app puts before page names.
var pagePrefix = regexp.MustCompile(`^\d{2,3} `)

// pageName is the name of page n (from 1) of total: "01 Scan.jpg".
func pageName(n, total int, name string) string {
	name = pagePrefix.ReplaceAllString(name, "")
	if total >= 100 {
		return fmt.Sprintf("%03d %s", n, name)
	}
	return fmt.Sprintf("%02d %s", n, name)
}

// renamePrefix marks a page on its way to a new name: renumbering goes
// through hidden names, so two pages never collide. An interrupted
// renumbering is finished by finishRenames.
const renamePrefix = ".umbenennen-"

// placePages moves the files (full paths, in order) into dir as its pages
// "01 …", "02 …". dir may already hold some of them.
func (s *Store) placePages(dir string, files []string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	type move struct{ from, tmp, to string }
	moves := make([]move, len(files))
	for i, f := range files {
		to := pageName(i+1, len(files), filepath.Base(f))
		moves[i] = move{f, filepath.Join(dir, fmt.Sprintf("%s%d-%s", renamePrefix, i, to)), filepath.Join(dir, to)}
	}
	for _, m := range moves {
		if err := os.Rename(m.from, m.tmp); err != nil {
			return err
		}
		s.markRemoved(m.from)
	}
	for _, m := range moves {
		// A file that arrived meanwhile under the same name (the phone) is
		// never replaced: this page then takes a free name.
		to := freeName(dir, filepath.Base(m.to))
		if err := os.Rename(m.tmp, to); err != nil {
			return err
		}
		s.noteOwn(to)
	}
	return nil
}

// finishRenames completes a renumbering that was interrupted (the app was
// closed in between): pages left under their temporary name get the final one.
func (s *Store) finishRenames(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		rest, ok := strings.CutPrefix(e.Name(), renamePrefix)
		if !ok || e.IsDir() {
			continue
		}
		if _, final, ok := strings.Cut(rest, "-"); ok && final != "" {
			to := freeName(dir, final)
			if os.Rename(filepath.Join(dir, e.Name()), to) == nil {
				s.noteOwn(to)
			}
		}
	}
}

func (s *Store) markRemoved(path string) {
	s.ownMu.Lock()
	s.own[path] = ownStamp{removed: true}
	s.ownMu.Unlock()
}

// MergeInbox makes one document of several inbox entries, in the given
// order: the pages of a multi-page entry stay together. The first entry's
// folder takes the others; for a single file a folder is made, named after
// it. It returns the folder name.
func (s *Store) MergeInbox(names []string) (string, error) {
	if len(names) < 2 {
		return "", errors.New("zum Zusammenfassen mindestens zwei Belege wählen")
	}
	var files []string
	var groups []string
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		e, err := s.InboxEntry(n)
		if err != nil {
			return "", err
		}
		if e.Group {
			groups = append(groups, e.Name)
		}
		for _, p := range e.Pages {
			files = append(files, filepath.Join(s.InboxDirPath(), filepath.FromSlash(p.Name)))
		}
	}
	first, err := s.InboxEntry(names[0])
	if err != nil {
		return "", err
	}
	target := first.Name
	if !first.Group {
		base := strings.TrimSuffix(first.Name, filepath.Ext(first.Name))
		target = filepath.Base(freeName(s.InboxDirPath(), base))
	}
	dir := filepath.Join(s.InboxDirPath(), target)
	if err := s.placePages(dir, files); err != nil {
		return "", fmt.Errorf("Seiten konnten nicht zusammengefasst werden: %w", err)
	}
	for _, g := range groups {
		if g == target {
			continue
		}
		s.removeEmptyDir(filepath.Join(s.InboxDirPath(), g))
	}
	return target, nil
}

// ReorderInbox puts the pages of a multi-page entry in the given order
// (all its pages, as listed by ListInbox).
func (s *Store) ReorderInbox(group string, pages []string) error {
	e, err := s.InboxEntry(group)
	if err != nil {
		return err
	}
	if !e.Group {
		return fmt.Errorf("„%s“ hat nur eine Seite", group)
	}
	current := make([]string, len(e.Pages))
	for i, p := range e.Pages {
		current[i] = p.Name
	}
	if len(pages) != len(current) || !sameSet(pages, current) {
		return errors.New("die Seiten haben sich inzwischen geändert – bitte erneut versuchen")
	}
	files := make([]string, len(pages))
	for i, p := range pages {
		files[i] = filepath.Join(s.InboxDirPath(), filepath.FromSlash(p))
	}
	return s.placePages(filepath.Join(s.InboxDirPath(), group), files)
}

// SplitInbox takes a page out of a multi-page entry ("folder/file"); it
// becomes a document of its own. With just the folder name, all pages are
// taken out. A folder left with one page is dissolved too.
func (s *Store) SplitInbox(name string) error {
	groupName, page, isPage := strings.Cut(name, "/")
	e, err := s.InboxEntry(groupName)
	if err != nil {
		return err
	}
	if !e.Group {
		return fmt.Errorf("„%s“ hat nur eine Seite", groupName)
	}
	dir := filepath.Join(s.InboxDirPath(), groupName)
	var out, keep []string
	for _, p := range e.Pages {
		full := filepath.Join(s.InboxDirPath(), filepath.FromSlash(p.Name))
		if !isPage || p.Name == name {
			out = append(out, full)
		} else {
			keep = append(keep, full)
		}
	}
	if isPage && len(out) == 0 {
		return fmt.Errorf("Seite „%s“ %w", page, ErrNotInInbox)
	}
	if len(keep) == 1 {
		out, keep = append(out, keep...), nil
	}
	for _, f := range out {
		to := freeName(s.InboxDirPath(), pagePrefix.ReplaceAllString(filepath.Base(f), ""))
		if err := os.Rename(f, to); err != nil {
			return err
		}
		s.markRemoved(f)
		s.noteOwn(to)
	}
	if len(keep) == 0 {
		s.removeEmptyDir(dir)
		return nil
	}
	return s.placePages(dir, keep)
}

func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

// naturalCompare orders names with numbers by their value: "Seite 2"
// before "Seite 10".
func naturalCompare(a, b string) int {
	ra, rb := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	for len(ra) > 0 && len(rb) > 0 {
		da, db := digits(ra), digits(rb)
		if da > 0 && db > 0 {
			na, _ := strconv.Atoi(string(ra[:da]))
			nb, _ := strconv.Atoi(string(rb[:db]))
			if na != nb {
				return na - nb
			}
			ra, rb = ra[da:], rb[db:]
			continue
		}
		if ra[0] != rb[0] {
			return int(ra[0]) - int(rb[0])
		}
		ra, rb = ra[1:], rb[1:]
	}
	return len(ra) - len(rb)
}

// digits counts the leading digits (at most 9, so the number fits an int).
func digits(r []rune) int {
	i := 0
	for i < len(r) && i < 9 && r[i] >= '0' && r[i] <= '9' {
		i++
	}
	return i
}
