// Package backup writes zip snapshots of the data folder to a second
// location. Without a cloud this is the only copy besides the data folder;
// with a cloud it protects against a sync tool spreading a mistake.
//
// To restore, unzip a snapshot into an empty folder and choose that folder
// as the data folder.
package backup

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

const (
	prefix = "pkv-sicherung-"
	layout = "2006-01-02-150405"
	// DefaultKeep is how many snapshots are kept.
	DefaultKeep = 20
)

// ErrInsideDataDir means the backup folder lies inside the data folder.
var ErrInsideDataDir = errors.New("der Sicherungsordner darf nicht im Datenordner liegen")

// Info describes one snapshot.
type Info struct {
	Name string
	Path string
	Time time.Time
	Size int64
}

// CheckDir reports whether backupDir can be used for dataDir.
func CheckDir(dataDir, backupDir string) error {
	if backupDir == "" {
		return errors.New("kein Sicherungsordner gewählt")
	}
	d, err1 := resolve(dataDir)
	b, err2 := resolve(backupDir)
	if err1 != nil || err2 != nil {
		return errors.Join(err1, err2)
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" { // usually case-insensitive
		d, b = strings.ToLower(d), strings.ToLower(b)
	}
	rel, err := filepath.Rel(d, b)
	if err != nil { // e.g. another drive on Windows
		return nil
	}
	if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrInsideDataDir
	}
	return nil
}

// resolve returns the absolute path with symbolic links resolved as far as
// the path exists (the backup folder may not exist yet).
func resolve(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var rest []string
	for p := abs; ; p = filepath.Dir(p) {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...), nil
		}
		if filepath.Dir(p) == p {
			return abs, nil
		}
		rest = append([]string{filepath.Base(p)}, rest...)
	}
}

// Create writes a snapshot of dataDir (without hidden files) into backupDir
// and deletes the oldest snapshots beyond keep.
func Create(dataDir, backupDir string, now time.Time, keep int) (Info, error) {
	if err := CheckDir(dataDir, backupDir); err != nil {
		return Info{}, err
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return Info{}, err
	}
	name := prefix + now.Format(layout) + ".zip"
	final := filepath.Join(backupDir, name)
	tmp, err := os.CreateTemp(backupDir, ".tmp-*.zip")
	if err != nil {
		return Info{}, err
	}
	werr := writeZip(tmp, dataDir)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), final)
	}
	if werr != nil {
		os.Remove(tmp.Name())
		return Info{}, werr
	}
	if keep > 0 {
		if err := prune(backupDir, keep); err != nil {
			return Info{}, err
		}
	}
	fi, err := os.Stat(final)
	if err != nil {
		return Info{}, err
	}
	return Info{Name: name, Path: final, Time: now, Size: fi.Size()}, nil
}

func writeZip(w io.Writer, dataDir string) error {
	zw := zip.NewWriter(w)
	// The sync tool or the app may remove files while the snapshot runs.
	gone := func(err error) bool { return errors.Is(err, fs.ErrNotExist) }
	err := filepath.WalkDir(dataDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if gone(err) && path != dataDir {
				return nil
			}
			return err
		}
		if path != dataDir && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dataDir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if gone(err) {
			return nil
		} else if err != nil {
			return err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		hdr.Method = zip.Deflate
		src, err := os.Open(path)
		if gone(err) {
			return nil
		} else if err != nil {
			return err
		}
		defer src.Close()
		dst, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, src)
		return err
	})
	if err != nil {
		return fmt.Errorf("Sicherung fehlgeschlagen: %w", err)
	}
	return zw.Close()
}

// List returns the snapshots in backupDir, newest first.
func List(backupDir string) ([]Info, error) {
	entries, err := os.ReadDir(backupDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".zip") {
			continue
		}
		t, err := time.ParseInLocation(layout, strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".zip"), time.Local)
		if err != nil {
			continue
		}
		var size int64
		if fi, err := e.Info(); err == nil {
			size = fi.Size()
		}
		out = append(out, Info{Name: name, Path: filepath.Join(backupDir, name), Time: t, Size: size})
	}
	slices.SortFunc(out, func(a, b Info) int { return b.Time.Compare(a.Time) })
	return out, nil
}

func prune(backupDir string, keep int) error {
	list, err := List(backupDir)
	if err != nil {
		return err
	}
	for i := keep; i < len(list); i++ {
		if err := os.Remove(list[i].Path); err != nil {
			return err
		}
	}
	return nil
}
