// Package service is the layer between the desktop UI and the core packages.
// Its exported methods are bound to the frontend by Wails. It keeps the
// loaded data folder in memory and writes every change through the store.
package service

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/ai"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/config"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/store"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/upload"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/watch"
)

// Version is set at build time (-ldflags "-X …/service.Version=…").
var Version = "dev"

// ErrNoDataDir means no data folder has been chosen yet.
var ErrNoDataDir = errors.New("kein Datenordner gewählt")

// EventDataChanged is emitted (with a DataChangedDTO) after the data folder
// was reloaded because someone else changed it.
const EventDataChanged = "data-changed"

// DataChangedDTO is the payload of EventDataChanged.
type DataChangedDTO struct {
	Issues     int    `json:"issues"`     // notes from loading (e.g. merged conflict copies)
	InboxAdded int    `json:"inboxAdded"` // new documents in the inbox folder (e.g. from the phone)
	InboxPages int    `json:"inboxPages"` // new pages of documents that were there already
	Error      string `json:"error"`
}

// Emitter sends events to the frontend (application.EventManager).
type Emitter interface {
	Emit(name string, data ...any) bool
}

// Service holds the state of the running app.
type Service struct {
	mu      sync.Mutex
	cfgDir  string
	cfg     *config.Config
	cfgErr  error
	st      *store.Store
	data    *store.Data
	issues  []store.Issue
	loadErr error
	pending map[string]*pendingImport

	watcher       *watch.Watcher
	watchErr      error
	watchDebounce time.Duration
	emitter       Emitter
	backupErr     error
	bg            sync.WaitGroup // background backups
	upload        *upload.Server
	inboxSeen     map[string]bool      // inbox pages at the last reload (names relative to the inbox)
	session       string               // temporary folder of this run (see sessionDir)
	tmpRoot       string               // where session folders are made; "" = os.TempDir() (tests set it)
	staged        map[string]stagedDoc // documents added in invoice forms, by ID
	aiLock        *ai.Lock             // one request to the AI server at a time

	now func() time.Time
}

// New creates the service with the configuration of the current user and
// opens the data folder chosen last time.
func New() *Service {
	dir, err := config.Dir()
	s := newService(dir)
	if err != nil {
		s.cfgErr = err
	}
	return s
}

// NewWithConfigDir is New with an explicit configuration folder (tests).
func NewWithConfigDir(dir string) *Service { return newService(dir) }

func newService(cfgDir string) *Service {
	s := &Service{cfgDir: cfgDir, now: time.Now, pending: map[string]*pendingImport{}, watchDebounce: 800 * time.Millisecond,
		aiLock: ai.NewLock()}
	if cfgDir == "" {
		return s
	}
	s.cfg, s.cfgErr = config.Load(cfgDir)
	if s.cfgErr == nil && s.cfg.DataDir != "" {
		s.loadErr = s.open(s.cfg.DataDir)
	}
	return s
}

func (s *Service) today() civil.Date { return civil.Of(s.now()) }

func (s *Service) newID() string { return model.NewID(s.now()) }

// open loads a data folder (caller holds the lock or is the constructor).
func (s *Service) open(dir string) error {
	if s.cfg == nil {
		return errors.New("Konfiguration nicht verfügbar")
	}
	st, err := store.Open(dir, s.cfg.DeviceID)
	if err != nil {
		return fmt.Errorf("Datenordner kann nicht geöffnet werden: %w", err)
	}
	data, issues, err := st.Load()
	if err != nil {
		return fmt.Errorf("Datenordner kann nicht gelesen werden: %w", err)
	}
	s.st, s.data, s.issues, s.loadErr = st, data, issues, nil
	s.stopWatch()
	if s.upload != nil { // it writes into the inbox of the previous folder
		_ = s.upload.Stop()
		s.upload = nil
	}
	s.inboxSeen = map[string]bool{}
	if entries, err := st.ListInbox(); err == nil {
		for _, e := range entries {
			for _, p := range e.Pages {
				s.inboxSeen[p.Name] = true
			}
		}
	}
	s.watcher, s.watchErr = watch.Start(dir, s.watchDebounce, st.IsOwnChange, s.externalChange)
	s.autoBackup()
	return nil
}

func (s *Service) stopWatch() {
	if s.watcher != nil {
		_ = s.watcher.Close()
		s.watcher = nil
	}
}

// newInboxPages compares the inbox with the last reload: pages the app did
// not put there itself are new – a new document, or new pages of one that
// was there already (the phone adding pages to a multi-page document).
func (s *Service) newInboxPages(entries []store.InboxEntry) (docs, pages int) {
	before := s.inboxSeen
	s.inboxSeen = map[string]bool{}
	for _, e := range entries {
		fresh, known := 0, 0
		for _, p := range e.Pages {
			s.inboxSeen[p.Name] = true
			path, err := s.st.InboxPath(p.Name)
			if before[p.Name] || err != nil || s.st.IsOwnChange(path) {
				known++
			} else {
				fresh++
			}
		}
		switch {
		case fresh == 0:
		case known > 0:
			pages += fresh
		default:
			docs++
		}
	}
	return docs, pages
}

// externalChange reloads the data folder after someone else changed it.
func (s *Service) externalChange() {
	s.mu.Lock()
	if s.st == nil {
		s.mu.Unlock()
		return
	}
	ev := DataChangedDTO{}
	if entries, err := s.st.ListInbox(); err == nil {
		ev.InboxAdded, ev.InboxPages = s.newInboxPages(entries)
	}
	data, issues, err := s.st.Load()
	if err != nil {
		ev.Error = err.Error()
	} else {
		s.data, s.issues = data, issues
		ev.Issues = len(issues)
	}
	em := s.emitter
	s.mu.Unlock()
	if em != nil {
		em.Emit(EventDataChanged, ev)
	}
}

// SetEmitter connects the service to the app's event bus.
//
//wails:ignore
func (s *Service) SetEmitter(e Emitter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitter = e
}

// ServiceShutdown stops watching the data folder (called by Wails on exit).
//
//wails:ignore
func (s *Service) ServiceShutdown() error {
	s.mu.Lock()
	s.stopWatch()
	if s.upload != nil {
		_ = s.upload.Stop()
		s.upload = nil
	}
	s.removeSession()
	s.mu.Unlock()
	// Let a running backup finish (it writes to a temp file first, so an
	// interrupted one leaves no broken snapshot – but a finished one is better).
	done := make(chan struct{})
	go func() { s.bg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
	}
	return nil
}

// ready returns the open store and data or ErrNoDataDir.
func (s *Service) ready() (*store.Store, *store.Data, error) {
	if s.st == nil || s.data == nil {
		return nil, nil, ErrNoDataDir
	}
	return s.st, s.data, nil
}

// IssueDTO is a note from loading the data folder.
type IssueDTO struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

// ProblemDTO is a finding of the settings check.
type ProblemDTO struct {
	Error bool   `json:"error"`
	Text  string `json:"text"`
}

// StatusDTO describes the state of the app for the frontend.
type StatusDTO struct {
	Configured bool         `json:"configured"` // a data folder is open
	NeedsSetup bool         `json:"needsSetup"` // no payers / coverage yet
	DataDir    string       `json:"dataDir"`
	Version    string       `json:"version"`
	DeviceID   string       `json:"deviceId"`
	Error      string       `json:"error"`
	Issues     []IssueDTO   `json:"issues"`
	Problems   []ProblemDTO `json:"problems"`
	// Warning: the app keeps working, but something is not as it should be
	// (no live updates, last backup failed).
	Warnings []string `json:"warnings"`
}

// Status returns the current state.
func (s *Service) Status() StatusDTO {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status()
}

func (s *Service) status() StatusDTO {
	st := StatusDTO{Version: Version, Issues: []IssueDTO{}, Problems: []ProblemDTO{}, Warnings: []string{}}
	if s.cfg != nil {
		st.DataDir = s.cfg.DataDir
		st.DeviceID = s.cfg.DeviceID
	}
	switch {
	case s.cfgErr != nil:
		st.Error = "Konfiguration: " + s.cfgErr.Error()
	case s.loadErr != nil:
		st.Error = s.loadErr.Error()
	}
	if s.data == nil {
		return st
	}
	st.Configured = true
	if s.watchErr != nil {
		st.Warnings = append(st.Warnings, "Änderungen von anderen Geräten werden erst nach „Neu einlesen“ sichtbar: "+s.watchErr.Error())
	}
	if s.backupErr != nil {
		st.Warnings = append(st.Warnings, "Letzte automatische Sicherung fehlgeschlagen: "+s.backupErr.Error())
	}
	st.NeedsSetup = len(s.data.Settings.Payers) == 0 || len(s.data.Settings.Coverage) == 0
	for _, i := range s.issues {
		st.Issues = append(st.Issues, IssueDTO(i))
	}
	if !st.NeedsSetup {
		for _, p := range s.data.Settings.Validate() {
			st.Problems = append(st.Problems, ProblemDTO(p))
		}
	}
	return st
}

// SetDataDir opens (or creates) the data folder and remembers it for the next start.
func (s *Service) SetDataDir(dir string) (StatusDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return s.status(), errors.New("bitte einen Ordner wählen")
	}
	if err := s.open(dir); err != nil {
		return s.status(), err
	}
	if s.cfg.DataDir == "" {
		// A device set up now starts with the new design: no hint about the old one.
		s.cfg.DesignHintSeen = true
	}
	s.cfg.DataDir = dir
	if err := s.cfg.Save(s.cfgDir); err != nil {
		return s.status(), fmt.Errorf("Einstellung konnte nicht gespeichert werden: %w", err)
	}
	return s.status(), nil
}

// Reload reads the data folder again (e.g. after another device synced changes).
func (s *Service) Reload() (StatusDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st == nil {
		return s.status(), ErrNoDataDir
	}
	err := s.open(s.st.Root())
	return s.status(), err
}

// Years returns the Leistungsjahre that have records, plus the current year, newest first.
func (s *Service) Years() ([]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{s.today().Year(): true}
	for i := range data.Invoices {
		if inv := &data.Invoices[i]; !inv.Deleted {
			seen[inv.ServiceYear()] = true
		}
	}
	for _, a := range data.Allowances {
		if !a.Deleted {
			seen[a.Date.Year()] = true
		}
	}
	years := make([]int, 0, len(seen))
	for y := range seen {
		years = append(years, y)
	}
	slices.Sort(years)
	slices.Reverse(years)
	return years, nil
}

// replaceInvoice puts inv into the in-memory list (by ID), keeping it sorted
// by service date.
func replaceInvoice(d *store.Data, inv model.Invoice) {
	i := slices.IndexFunc(d.Invoices, func(x model.Invoice) bool { return x.ID == inv.ID })
	if i >= 0 {
		d.Invoices[i] = inv
	} else {
		d.Invoices = append(d.Invoices, inv)
	}
	slices.SortStableFunc(d.Invoices, func(a, b model.Invoice) int {
		if c := a.ServiceDate().Compare(b.ServiceDate()); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
}

func findInvoice(d *store.Data, id string) (*model.Invoice, error) {
	for i := range d.Invoices {
		if d.Invoices[i].ID == id && !d.Invoices[i].Deleted {
			return &d.Invoices[i], nil
		}
	}
	return nil, fmt.Errorf("Rechnung %s nicht gefunden", id)
}
