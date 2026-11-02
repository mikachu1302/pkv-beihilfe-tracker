package backup

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func dataDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "2026", "rechnungen"), 0o755)
	os.MkdirAll(filepath.Join(d, "belege"), 0o755)
	os.MkdirAll(filepath.Join(d, ".git"), 0o755)
	os.WriteFile(filepath.Join(d, "einstellungen.json"), []byte(`{"schema":1}`), 0o644)
	os.WriteFile(filepath.Join(d, "2026", "rechnungen", "a.json"), []byte(`{"schema":1}`), 0o644)
	os.WriteFile(filepath.Join(d, "belege", "abc.pdf"), []byte("%PDF"), 0o644)
	os.WriteFile(filepath.Join(d, "2026", "rechnungen", ".tmp-1"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(d, ".git", "config"), []byte("x"), 0o644)
	return d
}

func TestCreateAndList(t *testing.T) {
	data, dir := dataDir(t), filepath.Join(t.TempDir(), "sicherung")
	now := time.Date(2026, 11, 2, 10, 15, 0, 0, time.Local)
	info, err := Create(data, dir, now, 3)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "pkv-sicherung-2026-11-02-101500.zip" || info.Size == 0 {
		t.Errorf("info %+v", info)
	}
	zr, err := zip.OpenReader(info.Path)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	zr.Close() // Windows cannot delete open files (pruning below)
	slices.Sort(names)
	want := []string{"2026/rechnungen/a.json", "belege/abc.pdf", "einstellungen.json"}
	if !slices.Equal(names, want) {
		t.Errorf("zip contains %v, want %v", names, want)
	}

	for i := 1; i <= 4; i++ {
		if _, err := Create(data, dir, now.Add(time.Duration(i)*time.Hour), 3); err != nil {
			t.Fatal(err)
		}
	}
	list, err := List(dir)
	if err != nil || len(list) != 3 || !list[0].Time.Equal(now.Add(4*time.Hour)) {
		t.Fatalf("list %+v, %v", list, err)
	}
	if empty, err := List(filepath.Join(t.TempDir(), "gibt-es-nicht")); err != nil || empty != nil {
		t.Errorf("missing folder: %v %v", empty, err)
	}
}

func TestBackupDirInsideDataDirIsRefused(t *testing.T) {
	data := dataDir(t)
	for _, dir := range []string{data, filepath.Join(data, "sicherung")} {
		if _, err := Create(data, dir, time.Now(), 3); !errors.Is(err, ErrInsideDataDir) {
			t.Errorf("%s: err = %v", dir, err)
		}
	}
	if err := CheckDir(data, filepath.Join(filepath.Dir(data), "daneben")); err != nil {
		t.Errorf("sibling folder refused: %v", err)
	}
}
