// Package xlsximport reads the spreadsheet the app replaces ("Abrechnung
// Krankenkasse"): one row per invoice with dates of submission and refunded
// amounts for Beihilfe and the PKV.
//
// Columns are recognised by their header text, so typos and moved columns do
// not matter. Computed columns (50/50 share, difference) and the yearly
// summary blocks next to the table are ignored; dates are read from the raw
// cell values.
package xlsximport

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/xuri/excelize/v2"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// ErrNoTable means no sheet has the expected header row.
var ErrNoTable = errors.New("keine Tabelle mit den Spalten Rechnungsdatum und Gesamtbetrag gefunden")

// Options say which payers the Beihilfe and PKV columns belong to. An empty
// BeihilfePayerID drops the Beihilfe columns (100 % PKV); rows with data in
// them get a warning.
type Options struct {
	BeihilfePayerID string
	PKVPayerID      string
}

// Warning refers to a spreadsheet row (1-based, as shown in Excel).
type Warning struct {
	Sheet string `json:"sheet"`
	Row   int    `json:"row"`
	Text  string `json:"text"`
}

// Result is the preview of an import; nothing is saved yet.
type Result struct {
	Sheet      string            `json:"sheet"` // the imported sheets, comma separated
	Invoices   []model.Invoice   `json:"invoices"`
	Allowances []model.Allowance `json:"allowances"`
	Warnings   []Warning         `json:"warnings"`
	// Invoices[i] comes from row Rows[i] of sheet RowSheets[i].
	Rows      []int    `json:"rows"`
	RowSheets []string `json:"rowSheets"`
	// Allowances[i] was found in the row of Invoices[AllowanceOf[i]].
	AllowanceOf []int `json:"allowanceOf"`
}

type field int

const (
	fInvoiceDate field = iota
	fServiceDate
	fPaid
	fProvider
	fNote
	fAmount
	fBhSubmitted
	fBhRefund
	fPkvSubmitted
	fPkvRefund
	fAllowance
)

// columns maps header text to fields. Notes may appear several times.
type columns struct {
	idx   map[field]int
	notes []int
}

func normalizeHeader(s string) string {
	s = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss").Replace(strings.ToLower(s))
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}

func classify(h string) (field, bool) {
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(h, w) {
				return true
			}
		}
		return false
	}
	beihilfe := has("beihilfe")
	pkv := has("krankenkasse", "pkv", "versicherung")
	switch {
	case has("rechnungsdatum"):
		return fInvoiceDate, true
	case has("behandlungsdatum", "behandlung"):
		return fServiceDate, true
	case has("anweisung", "bezahlt", "ueberwiesen"):
		return fPaid, true
	case has("tagegeld"):
		return fAllowance, true
	case has("eingereicht") && beihilfe:
		return fBhSubmitted, true
	case has("eingereicht") && pkv:
		return fPkvSubmitted, true
	case has("erstatt") && beihilfe:
		return fBhRefund, true
	case has("erstatt") && pkv:
		return fPkvRefund, true
	case has("behandler", "arzt", "leistungserbringer"):
		return fProvider, true
	case has("bemerkung", "notiz", "kommentar"):
		return fNote, true
	case has("gesamtbetrag", "rechnungsbetrag"):
		return fAmount, true
	}
	return 0, false
}

func findHeader(rows [][]string) (int, columns, bool) {
	for r := 0; r < len(rows) && r < 10; r++ {
		cols := columns{idx: map[field]int{}}
		for c, cell := range rows[r] {
			f, ok := classify(normalizeHeader(cell))
			if !ok {
				continue
			}
			if f == fNote {
				cols.notes = append(cols.notes, c)
				continue
			}
			if _, dup := cols.idx[f]; !dup {
				cols.idx[f] = c
			}
		}
		_, hasDate := cols.idx[fInvoiceDate]
		_, hasAmount := cols.idx[fAmount]
		if hasDate && hasAmount {
			return r, cols, true
		}
	}
	return 0, columns{}, false
}

// ImportFile reads a spreadsheet file.
func ImportFile(path string, opt Options, now time.Time) (*Result, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return importWorkbook(f, opt, now)
}

// Import reads a spreadsheet from r.
func Import(r io.Reader, opt Options, now time.Time) (*Result, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return importWorkbook(f, opt, now)
}

func importWorkbook(f *excelize.File, opt Options, now time.Time) (*Result, error) {
	if opt.PKVPayerID == "" {
		return nil, errors.New("PKV payer missing")
	}
	// Every sheet with an invoice table is imported (some keep one sheet per year).
	res := &Result{}
	var sheets []string
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet, excelize.Options{RawCellValue: true})
		if err != nil {
			return nil, err
		}
		hr, cols, ok := findHeader(rows)
		if !ok {
			continue
		}
		sheets = append(sheets, sheet)
		for r := hr + 1; r < len(rows); r++ {
			importRow(res, sheet, rows[r], r+1, cols, opt, now)
		}
	}
	if len(sheets) == 0 {
		return nil, ErrNoTable
	}
	res.Sheet = strings.Join(sheets, ", ")
	return res, nil
}

func importRow(res *Result, sheet string, row []string, rowNum int, cols columns, opt Options, now time.Time) {
	warn := func(format string, args ...any) {
		res.Warnings = append(res.Warnings, Warning{Sheet: sheet, Row: rowNum, Text: fmt.Sprintf(format, args...)})
	}
	cell := func(f field) string {
		c, ok := cols.idx[f]
		if !ok || c >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[c])
	}
	date := func(f field, label string) civil.Date {
		s := cell(f)
		if s == "" {
			return civil.Date{}
		}
		d, err := parseDate(s)
		if err != nil {
			warn("%s %q ist kein Datum", label, s)
		}
		return d
	}
	amount := func(f field, label string) (money.Cents, bool) {
		s := cell(f)
		if s == "" {
			return 0, false
		}
		v, err := money.ParseDecimal(s)
		if err != nil {
			if v, err = money.Parse(s); err != nil {
				warn("%s %q ist kein Betrag", label, s)
				return 0, false
			}
		}
		return v, true
	}

	invDate := date(fInvoiceDate, "Rechnungsdatum")
	amt, hasAmount := amount(fAmount, "Gesamtbetrag")
	provider := cell(fProvider)
	if invDate.IsZero() && provider == "" {
		return // empty or separator row, or a total below the table
	}
	var notes []string
	for _, c := range cols.notes {
		if c < len(row) && strings.TrimSpace(row[c]) != "" {
			notes = append(notes, strings.TrimSpace(row[c]))
		}
	}
	inv := model.Invoice{
		ID:          model.NewID(now),
		Provider:    provider,
		Category:    guessCategory(provider),
		InvoiceDate: invDate,
		ServiceFrom: date(fServiceDate, "Behandlungsdatum"),
		Amount:      amt,
		Paid:        model.Mark{Date: date(fPaid, "Anweisung")},
		Note:        strings.Join(notes, " · "),
		Claims:      map[string]model.Claim{},
	}
	if inv.ServiceFrom.Equal(inv.InvoiceDate) {
		inv.ServiceFrom = civil.Date{}
	}
	if !hasAmount {
		warn("Gesamtbetrag fehlt")
	}
	if invDate.IsZero() {
		warn("Rechnungsdatum fehlt")
	}

	type side struct {
		payerID           string
		label             string
		submitted, refund field
	}
	sides := []side{
		{opt.BeihilfePayerID, "Beihilfe", fBhSubmitted, fBhRefund},
		{opt.PKVPayerID, "PKV", fPkvSubmitted, fPkvRefund},
	}
	var refunded, rounding money.Cents // each payer may round its share up by a cent
	for _, sd := range sides {
		sub := date(sd.submitted, sd.label+" eingereicht")
		ref, hasRef := amount(sd.refund, sd.label+" erstattet")
		if sd.payerID == "" {
			if !sub.IsZero() || hasRef {
				warn("%s-Spalten enthalten Daten, aber kein Kostenträger ist zugeordnet – nicht übernommen", sd.label)
			}
			continue
		}
		c := model.Claim{Submitted: model.Mark{Date: sub}}
		if hasRef && ref != 0 { // a 0 is a placeholder, not a refund of 0 €
			c.Refunds = []model.Refund{{ID: model.NewID(now), Amount: ref, Note: "aus Excel übernommen"}}
			refunded += ref
			rounding++
		}
		if !c.IsZero() {
			inv.Claims[sd.payerID] = c
		}
		checkDate(warn, sd.label+" eingereicht", sub, invDate)
	}
	checkDate(warn, "Anweisung", inv.Paid.Date, invDate)
	if hasAmount && refunded > amt+rounding {
		warn("Erstattungen (%s €) höher als der Rechnungsbetrag (%s €)", refunded.DE(), amt.DE())
	}
	res.Invoices = append(res.Invoices, inv)
	res.Rows = append(res.Rows, rowNum)
	res.RowSheets = append(res.RowSheets, sheet)

	if a, ok := amount(fAllowance, "Krankenhaustagegeld"); ok && a != 0 {
		d := inv.ServiceDate()
		res.AllowanceOf = append(res.AllowanceOf, len(res.Invoices)-1)
		res.Allowances = append(res.Allowances, model.Allowance{
			ID: model.NewID(now), PayerID: opt.PKVPayerID, Kind: model.AllowanceHospitalDay,
			Date: d, Amount: a, Received: model.Mark{Date: d}, Note: "aus Excel übernommen",
		})
	}
}

// checkDate flags dates that are almost certainly typos.
func checkDate(warn func(string, ...any), label string, d, invoiceDate civil.Date) {
	if d.IsZero() || invoiceDate.IsZero() {
		return
	}
	switch {
	case d.Before(invoiceDate.AddYears(-1)) || d.After(invoiceDate.AddYears(4)):
		warn("%s am %s – ungewöhnliches Jahr, Tippfehler?", label, d.DE())
	case label != "Anweisung" && d.Before(invoiceDate):
		warn("%s am %s liegt vor dem Rechnungsdatum %s", label, d.DE(), invoiceDate.DE())
	}
}

// parseDate reads an Excel serial number or a date written as text.
func parseDate(s string) (civil.Date, error) {
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		if f < 1 || f > 2958465 { // 1900-01-01 … 9999-12-31
			return civil.Date{}, civil.ErrInvalid
		}
		t, err := excelize.ExcelDateToTime(f, false)
		if err != nil {
			return civil.Date{}, err
		}
		return civil.Of(t), nil
	}
	return civil.Parse(s)
}

func guessCategory(provider string) model.Category {
	p := strings.ToLower(provider)
	rules := []struct {
		cat   model.Category
		words []string
	}{
		{model.CatPharmacy, []string{"apothek"}},
		{model.CatDentist, []string{"zahn", "kieferorth"}},
		{model.CatHospital, []string{"klinik", "krankenhaus", "hospital"}},
		{model.CatTherapy, []string{"physio", "ergo", "logopäd", "krankengym", "massage"}},
		{model.CatNaturopath, []string{"heilprakt"}},
		{model.CatAids, []string{"optik", "sanitätshaus", "hörgerät", "akustik"}},
		{model.CatDoctor, []string{"dr.", "dr ", "arzt", "ärzt", "praxis", "labor", "radiolog"}},
	}
	for _, r := range rules {
		if slices.ContainsFunc(r.words, func(w string) bool { return strings.Contains(p, w) }) {
			return r.cat
		}
	}
	return ""
}
