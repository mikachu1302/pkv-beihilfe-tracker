package service

import (
	"errors"
	"strings"
	"time"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/backup"
)

// autoBackupAge: on start, a snapshot is made if the newest one is older.
const autoBackupAge = 24 * time.Hour

// BackupStatusDTO describes the backups of this device.
type BackupStatusDTO struct {
	Dir      string `json:"dir"`
	Last     string `json:"last"` // "2026-11-02 10:15" or ""
	LastSize int64  `json:"lastSize"`
	Count    int    `json:"count"`
	Error    string `json:"error"`
}

func (s *Service) backupStatus() BackupStatusDTO {
	st := BackupStatusDTO{}
	if s.cfg == nil {
		return st
	}
	st.Dir = s.cfg.BackupDir
	if s.backupErr != nil {
		st.Error = s.backupErr.Error()
	}
	if st.Dir == "" {
		return st
	}
	list, err := backup.List(st.Dir)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Count = len(list)
	if len(list) > 0 {
		st.Last = list[0].Time.Format("2006-01-02 15:04")
		st.LastSize = list[0].Size
	}
	return st
}

// BackupStatus returns where backups go and when the last one was made.
func (s *Service) BackupStatus() BackupStatusDTO {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.backupStatus()
}

// SetBackupDir sets (or with "" clears) the backup folder of this device
// and makes a first snapshot.
func (s *Service) SetBackupDir(dir string) (BackupStatusDTO, error) {
	s.mu.Lock()
	st, _, err := s.ready()
	if err != nil {
		defer s.mu.Unlock()
		return s.backupStatus(), err
	}
	dir = strings.TrimSpace(dir)
	if dir != "" {
		if err := backup.CheckDir(st.Root(), dir); err != nil {
			defer s.mu.Unlock()
			return s.backupStatus(), err
		}
	}
	s.cfg.BackupDir = dir
	if err := s.cfg.Save(s.cfgDir); err != nil {
		defer s.mu.Unlock()
		return s.backupStatus(), err
	}
	s.backupErr = nil
	if dir == "" {
		defer s.mu.Unlock()
		return s.backupStatus(), nil
	}
	return s.snapshot(st.Root(), dir)
}

// BackupNow makes a snapshot right away.
func (s *Service) BackupNow() (BackupStatusDTO, error) {
	s.mu.Lock()
	st, _, err := s.ready()
	if err != nil {
		defer s.mu.Unlock()
		return s.backupStatus(), err
	}
	if s.cfg.BackupDir == "" {
		defer s.mu.Unlock()
		return s.backupStatus(), errors.New("bitte zuerst einen Sicherungsordner wählen")
	}
	return s.snapshot(st.Root(), s.cfg.BackupDir)
}

// snapshot zips the data folder without holding the lock – that can take a
// while with many documents, and the app must stay usable. The caller holds
// the lock; snapshot releases it.
func (s *Service) snapshot(root, dir string) (BackupStatusDTO, error) {
	now := s.now()
	s.mu.Unlock()
	_, err := backup.Create(root, dir, now, backup.DefaultKeep)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backupErr = err
	return s.backupStatus(), err
}

// autoBackup starts a snapshot in the background when the last one is older
// than a day (caller holds the lock).
func (s *Service) autoBackup() {
	if s.cfg == nil || s.cfg.BackupDir == "" || s.st == nil {
		return
	}
	now := s.now()
	list, err := backup.List(s.cfg.BackupDir)
	for _, b := range list {
		// A snapshot named with a future time (clock was wrong) does not count.
		if err == nil && !b.Time.After(now.Add(time.Minute)) {
			if now.Sub(b.Time) < autoBackupAge {
				return
			}
			break
		}
	}
	root, dir := s.st.Root(), s.cfg.BackupDir
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		_, err := backup.Create(root, dir, now, backup.DefaultKeep)
		s.mu.Lock()
		s.backupErr = err
		s.mu.Unlock()
	}()
}
