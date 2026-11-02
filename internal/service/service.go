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

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/config"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/store"
)

// Version is set at build time (-ldflags "-X …/service.Version=…").
var Version = "dev"

// ErrNoDataDir means no data folder has been chosen yet.
var ErrNoDataDir = errors.New("kein Datenordner gewählt")

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
	s := &Service{cfgDir: cfgDir, now: time.Now, pending: map[string]*pendingImport{}}
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
}

// Status returns the current state.
func (s *Service) Status() StatusDTO {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status()
}

func (s *Service) status() StatusDTO {
	st := StatusDTO{Version: Version, Issues: []IssueDTO{}, Problems: []ProblemDTO{}}
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

// replaceInvoice puts inv into the in-memory list (by ID).
func replaceInvoice(d *store.Data, inv model.Invoice) {
	for i := range d.Invoices {
		if d.Invoices[i].ID == inv.ID {
			d.Invoices[i] = inv
			return
		}
	}
	d.Invoices = append(d.Invoices, inv)
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
