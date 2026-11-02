package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/xlsximport"
)

type pendingImport struct {
	res *xlsximport.Result
}

// ImportRowDTO is one invoice of a spreadsheet preview.
type ImportRowDTO struct {
	Key         string   `json:"key"`
	Row         int      `json:"row"`
	Provider    string   `json:"provider"`
	InvoiceDate string   `json:"invoiceDate"`
	ServiceDate string   `json:"serviceDate"`
	Year        int      `json:"year"`
	Amount      string   `json:"amount"`
	Status      []string `json:"status"` // e.g. "Beihilfe: eingereicht 15.05.2023, erstattet 36,59 €"
	Note        string   `json:"note"`
	Duplicate   bool     `json:"duplicate"` // an invoice with the same date, amount and provider exists
	Warnings    []string `json:"warnings"`
}

// YearCountDTO counts invoices per Leistungsjahr.
type YearCountDTO struct {
	Year  int `json:"year"`
	Count int `json:"count"`
}

// ImportPreviewDTO is the result of reading a spreadsheet; nothing is saved yet.
type ImportPreviewDTO struct {
	Token      string         `json:"token"`
	File       string         `json:"file"`
	Sheet      string         `json:"sheet"`
	Rows       []ImportRowDTO `json:"rows"`
	Years      []YearCountDTO `json:"years"`
	Allowances int            `json:"allowances"`
	Duplicates int            `json:"duplicates"`
	Warnings   int            `json:"warnings"`
}

// ImportResultDTO reports what was imported.
type ImportResultDTO struct {
	Invoices   int `json:"invoices"`
	Allowances int `json:"allowances"`
	Skipped    int `json:"skipped"`
}

func sameProvider(a, b string) bool {
	norm := func(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }
	return norm(a) == norm(b)
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
	token := s.newID()
	s.pending = map[string]*pendingImport{token: {res: res}} // only the latest preview is kept

	out := ImportPreviewDTO{Token: token, File: filepath.Base(path), Sheet: res.Sheet, Rows: []ImportRowDTO{},
		Years: []YearCountDTO{}, Allowances: len(res.Allowances), Warnings: len(res.Warnings)}
	warnings := map[int][]string{}
	for _, w := range res.Warnings {
		warnings[w.Row] = append(warnings[w.Row], w.Text)
	}
	years := map[int]int{}
	for i := range res.Invoices {
		inv := &res.Invoices[i]
		row := ImportRowDTO{Key: strconv.Itoa(i), Row: res.Rows[i], Provider: inv.Provider,
			InvoiceDate: inv.InvoiceDate.String(), ServiceDate: inv.ServiceDate().String(), Year: inv.ServiceYear(),
			Amount: inv.Amount.String(), Note: inv.Note, Status: claimSummary(&data.Settings, inv),
			Warnings: append([]string{}, warnings[res.Rows[i]]...)}
		for j := range data.Invoices {
			e := &data.Invoices[j]
			if !e.Deleted && e.Amount == inv.Amount && e.InvoiceDate.Equal(inv.InvoiceDate) && sameProvider(e.Provider, inv.Provider) {
				row.Duplicate = true
				out.Duplicates++
				break
			}
		}
		years[row.Year]++
		out.Rows = append(out.Rows, row)
	}
	for y, n := range years {
		out.Years = append(out.Years, YearCountDTO{Year: y, Count: n})
	}
	slices.SortFunc(out.Years, func(a, b YearCountDTO) int { return a.Year - b.Year })
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

// CommitSpreadsheet saves the chosen rows of the last preview. Allowances
// are imported together with the invoice of their row.
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
	for _, k := range keys {
		i, err := strconv.Atoi(k)
		if err != nil || i < 0 || i >= len(p.res.Invoices) {
			return out, fmt.Errorf("ungültige Zeile %q", k)
		}
		chosen[i] = true
	}
	rows := map[int]bool{}
	for i := range p.res.Invoices {
		if !chosen[i] {
			out.Skipped++
			continue
		}
		saved, err := st.SaveInvoice(p.res.Invoices[i])
		if err != nil {
			return out, fmt.Errorf("Speichern fehlgeschlagen: %w", err)
		}
		replaceInvoice(data, saved)
		rows[p.res.Rows[i]] = true
		out.Invoices++
	}
	for i, a := range p.res.Allowances {
		if !rows[p.res.AllowanceRows[i]] {
			continue
		}
		saved, err := st.SaveAllowance(a)
		if err != nil {
			return out, fmt.Errorf("Speichern fehlgeschlagen: %w", err)
		}
		data.Allowances = append(data.Allowances, saved)
		out.Allowances++
	}
	delete(s.pending, token)
	return out, nil
}
