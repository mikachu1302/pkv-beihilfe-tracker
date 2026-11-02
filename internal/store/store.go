// Package store keeps the records as JSON files in a user-chosen data folder.
//
// Layout:
//
//	<root>/einstellungen.json
//	<root>/Eingang/                     new scans and PDFs (picked up by the app)
//	<root>/belege/<sha256>.<ext>        documents, named by content
//	<root>/<year>/rechnungen/<id>.json
//	<root>/<year>/erstattungen/<id>.json
//	<root>/<year>/einreichungen/<id>.json
//	<root>/<year>/leistungen/<id>.json
//
// The folder may be synced by any tool (or none). Every record is its own
// file, so concurrent edits on two devices only collide for the same record.
// Each file carries a per-leaf clock; conflict copies – whatever the sync
// tool names them – are recognised by the record ID inside and merged leaf by
// leaf (newer value wins). Writes are atomic (temp file + rename).
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
)

// SchemaVersion is the current record file format.
const SchemaVersion = 1

// Names in the data folder.
const (
	SettingsFile = "einstellungen.json"
	InboxDir     = "Eingang"
	DocumentsDir = "belege"
)

// Kind is the type of a record file.
type Kind string

// Record kinds.
const (
	KindSettings   Kind = "settings"
	KindInvoice    Kind = "invoice"
	KindRefundDoc  Kind = "refund-document"
	KindSubmission Kind = "submission"
	KindAllowance  Kind = "allowance"
)

var kindDirs = map[Kind]string{
	KindInvoice:    "rechnungen",
	KindRefundDoc:  "erstattungen",
	KindSubmission: "einreichungen",
	KindAllowance:  "leistungen",
}

const settingsID = "settings"

var (
	errNotObject = errors.New("record data is not a JSON object")
	yearDirRe    = regexp.MustCompile(`^\d{4}$`)
	extRe        = regexp.MustCompile(`^\.[a-z0-9]{1,5}$`)
)

// ErrNewerSchema means a file was written by a newer version of the app.
var ErrNewerSchema = errors.New("file was written by a newer app version")

type envelope struct {
	Schema int               `json:"schema"`
	Type   Kind              `json:"type"`
	Data   json.RawMessage   `json:"data"`
	Clock  map[string]string `json:"clock,omitempty"`
}

// Data is everything in the data folder.
type Data struct {
	Settings    model.Settings
	Invoices    []model.Invoice
	RefundDocs  []model.RefundDoc
	Submissions []model.Submission
	Allowances  []model.Allowance
}

// Issue is something the user should know about after loading (merged
// conflict copies, unreadable files, files from a newer version).
type Issue struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

// Store reads and writes one data folder.
type Store struct {
	root   string
	device string
	now    func() time.Time

	mu    sync.Mutex
	paths map[string]string   // record key → path relative to root
	base  map[string]envelope // record key → version last read or written
}

// Open prepares root (created if missing) for use by the device with the given ID.
func Open(root, device string) (*Store, error) {
	if device == "" || strings.ContainsAny(device, "@/") {
		return nil, fmt.Errorf("invalid device ID %q", device)
	}
	for _, d := range []string{root, filepath.Join(root, InboxDir), filepath.Join(root, DocumentsDir)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return &Store{root: root, device: device, now: time.Now,
		paths: map[string]string{}, base: map[string]envelope{}}, nil
}

// Root returns the data folder.
func (s *Store) Root() string { return s.root }

func key(kind Kind, id string) string { return string(kind) + "/" + id }

type loaded struct {
	path string
	env  envelope
}

// Load reads the whole data folder. Conflict copies and records found in more
// than one place are merged and written back to their canonical file.
func (s *Store) Load() (*Data, []Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var issues []Issue
	groups := map[string][]loaded{}

	addFile := func(rel string, required bool) {
		env, err := readEnvelope(filepath.Join(s.root, rel))
		switch {
		case errors.Is(err, ErrNewerSchema):
			issues = append(issues, Issue{rel, "Datei stammt von einer neueren App-Version und wird ignoriert"})
			return
		case err != nil:
			if required {
				issues = append(issues, Issue{rel, "Datei nicht lesbar: " + err.Error()})
			}
			return
		}
		id := settingsID
		if env.Type != KindSettings {
			if _, ok := kindDirs[env.Type]; !ok {
				if required {
					issues = append(issues, Issue{rel, fmt.Sprintf("unbekannter Typ %q", env.Type)})
				}
				return
			}
			var head struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(env.Data, &head) != nil || head.ID == "" || strings.ContainsAny(head.ID, `/\.`) {
				issues = append(issues, Issue{rel, "Datensatz ohne gültige ID"})
				return
			}
			id = head.ID
		} else if required {
			return // settings only live in the root folder
		}
		k := key(env.Type, id)
		groups[k] = append(groups[k], loaded{rel, env})
	}

	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasPrefix(name, "."):
		case !e.IsDir() && strings.HasSuffix(strings.ToLower(name), ".json"):
			addFile(name, false) // other JSON files may belong to the user
		case e.IsDir() && yearDirRe.MatchString(name):
			for _, dir := range kindDirs {
				files, err := os.ReadDir(filepath.Join(s.root, name, dir))
				if errors.Is(err, fs.ErrNotExist) {
					continue
				} else if err != nil {
					return nil, nil, err
				}
				for _, f := range files {
					if !f.IsDir() && !strings.HasPrefix(f.Name(), ".") && strings.HasSuffix(strings.ToLower(f.Name()), ".json") {
						addFile(filepath.Join(name, dir, f.Name()), true)
					}
				}
			}
		}
	}

	data := &Data{}
	s.paths, s.base = map[string]string{}, map[string]envelope{}
	for _, k := range slices.Sorted(maps.Keys(groups)) {
		g := groups[k]
		slices.SortFunc(g, func(a, b loaded) int { return strings.Compare(a.path, b.path) })
		env := g[0].env
		for _, other := range g[1:] {
			if env, err = merge(env, other.env); err != nil {
				return nil, nil, fmt.Errorf("%s: %w", other.path, err)
			}
		}
		if env.Data, err = canonical(env.Type, env.Data); err != nil {
			issues = append(issues, Issue{g[0].path, "Datensatz nicht lesbar: " + err.Error()})
			continue
		}
		path, err := pathFor(env.Type, env.Data)
		if err != nil {
			return nil, nil, err
		}
		if len(g) > 1 || g[0].path != path {
			if err := writeEnvelope(filepath.Join(s.root, path), env); err != nil {
				return nil, nil, err
			}
			for _, l := range g {
				if l.path == path {
					continue
				}
				if err := os.Remove(filepath.Join(s.root, l.path)); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return nil, nil, err
				}
				text := "Konfliktkopie zusammengeführt"
				if len(g) == 1 {
					text = "Datei an den richtigen Ort verschoben: " + path
				}
				issues = append(issues, Issue{l.path, text})
			}
		}
		s.paths[k], s.base[k] = path, env
		if err := appendRecord(data, env); err != nil {
			return nil, nil, err
		}
	}
	slices.SortFunc(data.Invoices, func(a, b model.Invoice) int {
		if c := a.ServiceDate().Compare(b.ServiceDate()); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return data, issues, nil
}

// canonical re-encodes data through its Go type (field order, defaults).
func canonical(kind Kind, data json.RawMessage) (json.RawMessage, error) {
	switch kind {
	case KindSettings:
		return recode[model.Settings](data)
	case KindInvoice:
		return recode[model.Invoice](data)
	case KindRefundDoc:
		return recode[model.RefundDoc](data)
	case KindSubmission:
		return recode[model.Submission](data)
	case KindAllowance:
		return recode[model.Allowance](data)
	}
	return nil, fmt.Errorf("unknown kind %q", kind)
}

func recode[T any](data json.RawMessage) (json.RawMessage, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func appendRecord(d *Data, env envelope) error {
	var err error
	switch env.Type {
	case KindSettings:
		err = json.Unmarshal(env.Data, &d.Settings)
	case KindInvoice:
		d.Invoices, err = appendDecoded(d.Invoices, env.Data)
	case KindRefundDoc:
		d.RefundDocs, err = appendDecoded(d.RefundDocs, env.Data)
	case KindSubmission:
		d.Submissions, err = appendDecoded(d.Submissions, env.Data)
	case KindAllowance:
		d.Allowances, err = appendDecoded(d.Allowances, env.Data)
	}
	return err
}

func appendDecoded[T any](list []T, data json.RawMessage) ([]T, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return list, err
	}
	return append(list, v), nil
}

// pathFor returns the canonical relative path of a record.
func pathFor(kind Kind, data json.RawMessage) (string, error) {
	if kind == KindSettings {
		return SettingsFile, nil
	}
	var id string
	var year int
	switch kind {
	case KindInvoice:
		var v model.Invoice
		if err := json.Unmarshal(data, &v); err != nil {
			return "", err
		}
		id, year = v.ID, v.ServiceYear()
	case KindRefundDoc:
		var v model.RefundDoc
		if err := json.Unmarshal(data, &v); err != nil {
			return "", err
		}
		id, year = v.ID, v.Date.Year()
	case KindSubmission:
		var v model.Submission
		if err := json.Unmarshal(data, &v); err != nil {
			return "", err
		}
		id, year = v.ID, v.Date.Year()
	case KindAllowance:
		var v model.Allowance
		if err := json.Unmarshal(data, &v); err != nil {
			return "", err
		}
		id, year = v.ID, v.Date.Year()
	default:
		return "", fmt.Errorf("unknown kind %q", kind)
	}
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return "", fmt.Errorf("invalid record ID %q", id)
	}
	if year < 0 || year > 9999 {
		year = 0
	}
	return filepath.Join(fmt.Sprintf("%04d", year), kindDirs[kind], id+".json"), nil
}

// SaveSettings writes the settings and returns them as stored (merged with
// changes another device made in the meantime).
func (s *Store) SaveSettings(v model.Settings) (model.Settings, error) {
	return save(s, KindSettings, settingsID, v)
}

// SaveInvoice writes an invoice (see SaveSettings for the return value).
func (s *Store) SaveInvoice(v model.Invoice) (model.Invoice, error) {
	return save(s, KindInvoice, v.ID, v)
}

// SaveRefundDoc writes a refund document.
func (s *Store) SaveRefundDoc(v model.RefundDoc) (model.RefundDoc, error) {
	return save(s, KindRefundDoc, v.ID, v)
}

// SaveSubmission writes a submission.
func (s *Store) SaveSubmission(v model.Submission) (model.Submission, error) {
	return save(s, KindSubmission, v.ID, v)
}

// SaveAllowance writes an allowance.
func (s *Store) SaveAllowance(v model.Allowance) (model.Allowance, error) {
	return save(s, KindAllowance, v.ID, v)
}

func save[T any](s *Store, kind Kind, id string, rec T) (T, error) {
	var zero T
	if id == "" {
		return zero, errors.New("record without ID")
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return zero, err
	}
	if raw, err = canonical(kind, raw); err != nil {
		return zero, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(kind, id)
	base := s.base[k]
	env, err := stamp(base, kind, raw, s.timestamp(base))
	if err != nil {
		return zero, err
	}
	oldPath := s.paths[k]
	if oldPath != "" {
		// Another device may have changed the file since we read it: merge
		// instead of overwriting its changes.
		disk, err := readEnvelope(filepath.Join(s.root, oldPath))
		switch {
		case err == nil && !maps.Equal(disk.Clock, base.Clock):
			if env, err = merge(env, disk); err != nil {
				return zero, err
			}
			if env.Data, err = canonical(kind, env.Data); err != nil {
				return zero, err
			}
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return zero, err
		}
	}
	path, err := pathFor(kind, env.Data)
	if err != nil {
		return zero, err
	}
	if err := writeEnvelope(filepath.Join(s.root, path), env); err != nil {
		return zero, err
	}
	if oldPath != "" && oldPath != path {
		if err := os.Remove(filepath.Join(s.root, oldPath)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return zero, err
		}
	}
	s.paths[k], s.base[k] = path, env
	var out T
	if err := json.Unmarshal(env.Data, &out); err != nil {
		return zero, err
	}
	return out, nil
}

// timestamp returns a clock value newer than every stamp in base.
func (s *Store) timestamp(base envelope) string {
	const layout = "2006-01-02T15:04:05.000Z"
	t := s.now().UTC().Truncate(time.Millisecond)
	for _, st := range base.Clock {
		tp, _, _ := strings.Cut(st, "@")
		if prev, err := time.Parse(layout, tp); err == nil && !t.After(prev) {
			t = prev.Add(time.Millisecond)
		}
	}
	return t.Format(layout) + "@" + s.device
}

// AddDocument stores a document under belege/ named by its SHA-256 and returns
// the file name. Storing the same content twice yields the same name.
func (s *Store) AddDocument(r io.Reader, ext string) (string, error) {
	ext = strings.ToLower(ext)
	if !extRe.MatchString(ext) {
		return "", fmt.Errorf("invalid file extension %q", ext)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	name := hex.EncodeToString(sum[:]) + ext
	path := filepath.Join(s.root, DocumentsDir, name)
	if _, err := os.Stat(path); err == nil {
		return name, nil
	}
	return name, writeFileAtomic(path, b)
}

// DocumentPath returns the full path of a stored document.
func (s *Store) DocumentPath(name string) string {
	return filepath.Join(s.root, DocumentsDir, filepath.Base(name))
}

func readEnvelope(path string) (envelope, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return envelope{}, err
	}
	var env envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return envelope{}, err
	}
	if env.Type == "" || len(env.Data) == 0 {
		return envelope{}, errors.New("not a record file")
	}
	if env.Schema > SchemaVersion {
		return envelope{}, ErrNewerSchema
	}
	return env, nil
}

func writeEnvelope(path string, env envelope) error {
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(b, '\n'))
}

// writeFileAtomic writes via a hidden temp file in the same folder and renames it.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp, path)
	}
	if werr != nil {
		os.Remove(tmp)
	}
	return werr
}
