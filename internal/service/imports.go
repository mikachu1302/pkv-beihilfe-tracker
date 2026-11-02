package service

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/xlsximport"
)

type pendingImport struct {
	res     *xlsximport.Result
	file    string
	payers  []string            // payers of the spreadsheet columns
	fields  []map[string]string // spreadsheet values per row
	matches []rowMatch          // existing invoice per row ("" = new)
	states  []string            // rowNew, rowChanged, rowUnchanged
	removed []string            // imported from this file before, no longer in it
}

// ImportRowDTO is one invoice of a spreadsheet preview.
type ImportRowDTO struct {
	Key         string   `json:"key"`
	Sheet       string   `json:"sheet"`
	Row         int      `json:"row"`
	Provider    string   `json:"provider"`
	InvoiceDate string   `json:"invoiceDate"`
	ServiceDate string   `json:"serviceDate"`
	Year        int      `json:"year"`
	Amount      string   `json:"amount"`
	Status      []string `json:"status"` // e.g. "Beihilfe: eingereicht 15.05.2023, erstattet 36,59 €"
	Note        string   `json:"note"`
	Warnings    []string `json:"warnings"`
	// State: "new" (not in the app yet), "changed" (an existing invoice gets
	// the changes listed) or "unchanged".
	State     string            `json:"state"`
	InvoiceID string            `json:"invoiceId"` // the existing invoice
	Uncertain bool              `json:"uncertain"` // matched by similarity only: please check
	Changes   []ImportChangeDTO `json:"changes"`
}

// ImportChangeDTO is a field an import would change in an existing invoice.
type ImportChangeDTO struct {
	Label    string `json:"label"`
	From     string `json:"from"`
	To       string `json:"to"`
	Conflict bool   `json:"conflict"` // also changed in the app since the last import
}

// ImportRemovedDTO is an invoice imported from this file before that is no
// longer in it (offered for deletion).
type ImportRemovedDTO struct {
	Key       string `json:"key"`
	InvoiceID string `json:"invoiceId"`
	Label     string `json:"label"`
	Sheet     string `json:"sheet"`
	Row       int    `json:"row"`
}

// YearCountDTO counts invoices per Leistungsjahr.
type YearCountDTO struct {
	Year  int `json:"year"`
	Count int `json:"count"`
}

// ImportPreviewDTO is the result of reading a spreadsheet; nothing is saved yet.
type ImportPreviewDTO struct {
	Token        string             `json:"token"`
	File         string             `json:"file"`
	Sheet        string             `json:"sheet"`
	Rows         []ImportRowDTO     `json:"rows"`
	Years        []YearCountDTO     `json:"years"`
	Allowances   int                `json:"allowances"`
	Warnings     int                `json:"warnings"`
	New          int                `json:"new"`
	Changed      int                `json:"changed"`
	Unchanged    int                `json:"unchanged"`
	DeletedInApp int                `json:"deletedInApp"` // rows of invoices deleted in the app (not created again)
	Removed      []ImportRemovedDTO `json:"removed"`
}

// ImportResultDTO reports what was imported.
type ImportResultDTO struct {
	Invoices   int `json:"invoices"` // created
	Updated    int `json:"updated"`
	Deleted    int `json:"deleted"`
	Allowances int `json:"allowances"`
	Skipped    int `json:"skipped"`
}

// PreviewSpreadsheet reads the old spreadsheet. beihilfePayerID may be empty
// (PKV only); pkvPayerID is required.
func (s *Service) PreviewSpreadsheet(path, beihilfePayerID, pkvPayerID string) (ImportPreviewDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return ImportPreviewDTO{}, err
	}
	for _, id := range []string{beihilfePayerID, pkvPayerID} {
		if _, ok := data.Settings.Payer(id); id != "" && !ok {
			return ImportPreviewDTO{}, fmt.Errorf("unbekannter Kostenträger %q", id)
		}
	}
	if pkvPayerID == "" {
		return ImportPreviewDTO{}, errors.New("bitte die PKV wählen")
	}
	res, err := xlsximport.ImportFile(path, xlsximport.Options{BeihilfePayerID: beihilfePayerID, PKVPayerID: pkvPayerID}, s.now())
	if err != nil {
		return ImportPreviewDTO{}, fmt.Errorf("Tabelle kann nicht gelesen werden: %w", err)
	}
	file := filepath.Base(path)
	payers := []string{pkvPayerID}
	if beihilfePayerID != "" {
		payers = []string{beihilfePayerID, pkvPayerID}
	}
	p := &pendingImport{res: res, file: file, payers: payers}
	for i := range res.Invoices {
		p.fields = append(p.fields, excelFields(&res.Invoices[i], payers))
	}
	p.matches = matchRows(p.fields, res.RowSheets, res.Rows, file, data.Invoices, payers)
	byID := map[string]*model.Invoice{}
	for i := range data.Invoices {
		byID[data.Invoices[i].ID] = &data.Invoices[i]
	}

	token := s.newID()
	s.pending = map[string]*pendingImport{token: p} // only the latest preview is kept
	out := ImportPreviewDTO{Token: token, File: file, Sheet: res.Sheet, Rows: []ImportRowDTO{},
		Years: []YearCountDTO{}, Allowances: len(res.Allowances), Warnings: len(res.Warnings), Removed: []ImportRemovedDTO{}}
	type rowKey struct {
		sheet string
		row   int
	}
	warnings := map[rowKey][]string{}
	for _, w := range res.Warnings {
		k := rowKey{w.Sheet, w.Row}
		warnings[k] = append(warnings[k], w.Text)
	}
	years := map[int]int{}
	matched := map[string]bool{}
	for i := range res.Invoices {
		inv := &res.Invoices[i]
		row := ImportRowDTO{Key: strconv.Itoa(i), Sheet: res.RowSheets[i], Row: res.Rows[i], Provider: inv.Provider,
			InvoiceDate: inv.InvoiceDate.String(), ServiceDate: inv.ServiceDate().String(), Year: inv.ServiceYear(),
			Amount: inv.Amount.String(), Note: inv.Note, Status: claimSummary(&data.Settings, inv),
			Warnings: append([]string{}, warnings[rowKey{res.RowSheets[i], res.Rows[i]}]...),
			State:    rowNew, Changes: []ImportChangeDTO{}}
		if m := p.matches[i]; m.invoiceID != "" {
			cur := byID[m.invoiceID]
			matched[m.invoiceID] = true
			row.InvoiceID, row.Uncertain, row.State = m.invoiceID, m.uncertain, rowUnchanged
			if cur.Deleted {
				row.State = rowDeleted // deleted in the app: not created again
			} else {
				for _, c := range rowChanges(p.fields[i], cur, payers) {
					row.Changes = append(row.Changes, ImportChangeDTO{Label: fieldLabel(&data.Settings, c.key),
						From: fieldText(c.key, c.from), To: fieldText(c.key, c.to), Conflict: c.conflict})
				}
				if len(row.Changes) > 0 {
					row.State = rowChanged
				}
			}
		}
		p.states = append(p.states, row.State)
		switch row.State {
		case rowNew:
			out.New++
		case rowChanged:
			out.Changed++
		case rowDeleted:
			out.DeletedInApp++
		default:
			out.Unchanged++
		}
		years[row.Year]++
		out.Rows = append(out.Rows, row)
	}
	// Invoices imported from this file before whose row is gone.
	sheets := map[string]bool{}
	for _, sh := range res.RowSheets {
		sheets[sh] = true
	}
	for i := range data.Invoices {
		inv := &data.Invoices[i]
		if inv.Deleted || matched[inv.ID] || inv.Source == nil || !strings.EqualFold(inv.Source.File, file) || !sheets[inv.Source.Sheet] {
			continue
		}
		p.removed = append(p.removed, inv.ID)
		out.Removed = append(out.Removed, ImportRemovedDTO{Key: "del:" + inv.ID, InvoiceID: inv.ID, Label: invoiceLabel(inv),
			Sheet: inv.Source.Sheet, Row: inv.Source.Row})
	}
	for y, n := range years {
		out.Years = append(out.Years, YearCountDTO{Year: y, Count: n})
	}
	slices.SortFunc(out.Years, func(a, b YearCountDTO) int { return a.Year - b.Year })
	out.Allowances = 0 // only those of new rows are imported
	for _, row := range res.AllowanceOf {
		if p.states[row] == rowNew {
			out.Allowances++
		}
	}
	return out, nil
}

func claimSummary(set *model.Settings, inv *model.Invoice) []string {
	out := []string{}
	ids := make([]string, 0, len(inv.Claims))
	for id := range inv.Claims {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		c := inv.Claims[id]
		name := id
		if p, ok := set.Payer(id); ok {
			name = p.Name
		}
		var parts []string
		if c.Submitted.Done() {
			parts = append(parts, "eingereicht "+c.Submitted.Date.DE())
		}
		var refunded money.Cents
		for _, r := range c.Refunds {
			refunded += r.Amount
		}
		if len(c.Refunds) > 0 {
			parts = append(parts, "erstattet "+refunded.DE()+" €")
		}
		if len(parts) > 0 {
			out = append(out, name+": "+strings.Join(parts, ", "))
		}
	}
	return out
}

// CommitSpreadsheet applies the chosen rows of the last preview: new rows
// become invoices, changed rows update their invoice (only the fields listed
// in the preview). Keys "del:<invoice ID>" delete invoices that are no longer
// in the file. Allowances are imported together with the new invoice of
// their row. Every matched row remembers where it came from in the file.
func (s *Service) CommitSpreadsheet(token string, keys []string) (ImportResultDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out ImportResultDTO
	st, data, err := s.ready()
	if err != nil {
		return out, err
	}
	p := s.pending[token]
	if p == nil {
		return out, errors.New("die Vorschau ist abgelaufen – bitte die Datei erneut einlesen")
	}
	chosen := map[int]bool{}
	var remove []string
	for _, k := range keys {
		if id, ok := strings.CutPrefix(k, "del:"); ok {
			if !slices.Contains(p.removed, id) {
				return out, fmt.Errorf("ungültige Zeile %q", k)
			}
			remove = append(remove, id)
			continue
		}
		i, err := strconv.Atoi(k)
		if err != nil || i < 0 || i >= len(p.res.Invoices) {
			return out, fmt.Errorf("ungültige Zeile %q", k)
		}
		chosen[i] = true
	}
	// The origin with the values from the file; values of payers not mapped
	// in this import are kept from before.
	source := func(i int, prev *model.ImportSource) *model.ImportSource {
		values := maps.Clone(p.fields[i])
		if prev != nil {
			for k, v := range prev.Values {
				if _, ok := values[k]; !ok {
					values[k] = v
				}
			}
		}
		return &model.ImportSource{File: p.file, Sheet: p.res.RowSheets[i], Row: p.res.Rows[i], Values: values}
	}
	save := func(inv model.Invoice) error {
		saved, err := st.SaveInvoice(inv)
		if err != nil {
			return fmt.Errorf("Speichern fehlgeschlagen: %w", err)
		}
		replaceInvoice(data, saved)
		return nil
	}
	// New records keep the IDs from the preview, so committing again after
	// an error overwrites instead of duplicating.
	for i := range p.res.Invoices {
		switch {
		case p.states[i] == rowNew && chosen[i]:
			inv := p.res.Invoices[i]
			inv.Source = source(i, nil)
			if err := save(inv); err != nil {
				return out, err
			}
			out.Invoices++
		case p.states[i] == rowChanged && chosen[i], p.states[i] == rowUnchanged:
			// Unchanged rows only remember their origin (rows may have moved).
			cur, err := findInvoice(data, p.matches[i].invoiceID)
			if err != nil {
				out.Skipped++ // deleted meanwhile (e.g. on another device)
				continue
			}
			inv := cloneInvoice(cur)
			if p.states[i] == rowChanged {
				for _, c := range rowChanges(p.fields[i], cur, p.payers) {
					applyField(&inv, c.key, c.to, s.newID)
				}
				out.Updated++
			}
			if src := source(i, cur.Source); p.states[i] == rowChanged || !sameSource(cur.Source, src) {
				inv.Source = src
				if err := save(inv); err != nil {
					return out, err
				}
			}
		default:
			out.Skipped++
		}
	}
	for _, id := range remove {
		cur, err := findInvoice(data, id)
		if err != nil {
			continue // deleted meanwhile
		}
		inv := cloneInvoice(cur)
		inv.Deleted = true
		if err := save(inv); err != nil {
			return out, err
		}
		out.Deleted++
	}
	for i, a := range p.res.Allowances {
		if row := p.res.AllowanceOf[i]; !chosen[row] || p.states[row] != rowNew {
			continue // allowances of existing invoices are there already
		}
		saved, err := st.SaveAllowance(a)
		if err != nil {
			return out, fmt.Errorf("Speichern fehlgeschlagen: %w", err)
		}
		if j := slices.IndexFunc(data.Allowances, func(x model.Allowance) bool { return x.ID == saved.ID }); j >= 0 {
			data.Allowances[j] = saved
		} else {
			data.Allowances = append(data.Allowances, saved)
		}
		out.Allowances++
	}
	delete(s.pending, token)
	return out, nil
}

func sameSource(a, b *model.ImportSource) bool {
	return a != nil && b != nil && a.File == b.File && a.Sheet == b.Sheet && a.Row == b.Row && maps.Equal(a.Values, b.Values)
}
