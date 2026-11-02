// Package export writes the data out of the app: an Excel workbook (for the
// tax return, an archive or anyone without the app) and a PDF bundle of the
// documents for a submission to the Beihilfe or the PKV.
package export

import (
	"fmt"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/calc"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

var statusText = map[calc.Status]string{
	calc.StatusOpen:      "nicht eingereicht",
	calc.StatusHeld:      "zurückgehalten",
	calc.StatusSubmitted: "eingereicht",
	calc.StatusRefunded:  "erstattet",
	calc.StatusSkipped:   "verzichtet",
}

var categoryText = map[model.Category]string{
	model.CatDoctor: "Arzt", model.CatDentist: "Zahnarzt", model.CatPharmacy: "Apotheke", model.CatTherapy: "Heilmittel",
	model.CatAids: "Hilfsmittel", model.CatHospital: "Krankenhaus", model.CatNaturopath: "Heilpraktiker", model.CatOther: "Sonstiges",
}

type sheet struct {
	f    *excelize.File
	name string
	row  int
	eur  int
	date int
	bold int
}

func (s *sheet) cell(col int) string {
	c, _ := excelize.CoordinatesToCellName(col, s.row)
	return c
}

// put writes values into the current row and moves to the next row.
func (s *sheet) put(values ...any) {
	for i, v := range values {
		c := s.cell(i + 1)
		switch x := v.(type) {
		case money.Cents:
			_ = s.f.SetCellValue(s.name, c, float64(x)/100)
			_ = s.f.SetCellStyle(s.name, c, c, s.eur)
		case civil.Date:
			if !x.IsZero() {
				_ = s.f.SetCellValue(s.name, c, x.Time())
				_ = s.f.SetCellStyle(s.name, c, c, s.date)
			}
		default:
			_ = s.f.SetCellValue(s.name, c, v)
		}
	}
	s.row++
}

func (s *sheet) header(values ...string) {
	for i, v := range values {
		c := s.cell(i + 1)
		_ = s.f.SetCellValue(s.name, c, v)
		_ = s.f.SetCellStyle(s.name, c, c, s.bold)
		w := float64(len([]rune(v))) + 2
		if w < 11 {
			w = 11
		}
		col, _ := excelize.ColumnNumberToName(i + 1)
		_ = s.f.SetColWidth(s.name, col, col, w)
	}
	_ = s.f.SetPanes(s.name, &excelize.Panes{Freeze: true, YSplit: s.row, TopLeftCell: s.cell(1)[:1] + fmt.Sprint(s.row+1), ActivePane: "bottomLeft"})
	s.row++
}

// Workbook builds the workbook for one Leistungsjahr (0 = all years).
func Workbook(set *model.Settings, invoices []model.Invoice, allowances []model.Allowance, year int, today civil.Date) (*excelize.File, error) {
	f := excelize.NewFile()
	eur, err := f.NewStyle(&excelize.Style{CustomNumFmt: ptr(`#,##0.00 "€";-#,##0.00 "€"`)})
	if err != nil {
		return nil, err
	}
	date, err := f.NewStyle(&excelize.Style{CustomNumFmt: ptr("dd.mm.yyyy")})
	if err != nil {
		return nil, err
	}
	bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, err
	}
	newSheet := func(name string) *sheet {
		if f.SheetCount == 1 && f.GetSheetName(0) == "Sheet1" {
			_ = f.SetSheetName("Sheet1", name)
		} else {
			_, _ = f.NewSheet(name)
		}
		return &sheet{f: f, name: name, row: 1, eur: eur, date: date, bold: bold}
	}

	// Invoices.
	inv := newSheet("Rechnungen")
	hdr := []string{"Leistungsjahr", "Behandlung", "Rechnungsdatum", "Behandler", "Kategorie", "Rechnungsnr.", "Betrag", "Bezahlt am"}
	for _, p := range set.Payers {
		hdr = append(hdr, p.Name+" Satz %", p.Name+" SOLL", p.Name+" IST", p.Name+" Status", p.Name+" eingereicht", p.Name+" erstattet", p.Name+" Klärung")
	}
	hdr = append(hdr, "Nicht erstattet", "Notiz")
	inv.header(hdr...)
	for i := range invoices {
		in := &invoices[i]
		if in.Deleted || year != 0 && in.ServiceYear() != year {
			continue
		}
		v := calc.Invoice(set, in, today)
		row := []any{v.Year, v.ServiceDate, in.InvoiceDate, in.Provider, categoryText[in.Category], in.Reference, in.Amount, in.Paid.Date}
		for _, p := range set.Payers {
			var cv *calc.ClaimView
			for j := range v.Claims {
				if v.Claims[j].PayerID == p.ID {
					cv = &v.Claims[j]
				}
			}
			if cv == nil {
				row = append(row, "", "", "", "", "", "", "")
				continue
			}
			clarify := ""
			if cv.Clarify.Done() {
				clarify = "ja"
				if cv.Clarify.Note != "" {
					clarify = cv.Clarify.Note
				}
			}
			row = append(row, cv.Rate.Percent(), cv.Expected, cv.Actual, statusText[cv.Status], cv.Submitted.Date, cv.Refunded.Date, clarify)
		}
		row = append(row, v.OwnShare, in.Note)
		inv.put(row...)
	}

	// Years.
	years := calc.Years(set, invoices, allowances, today)
	if year != 0 {
		var one []calc.Year
		for _, y := range years {
			if y.Year == year {
				one = append(one, y)
			}
		}
		years = one
	}
	ys := newSheet("Übersicht")
	hdr = []string{"Leistungsjahr", "Rechnungen", "Rechnungsbeträge"}
	for _, p := range set.Payers {
		hdr = append(hdr, p.Name+" SOLL", p.Name+" IST")
	}
	hdr = append(hdr, "Nicht erstattet", "Leistungen ohne Rechnung")
	ys.header(hdr...)
	writeYear := func(label any, y calc.Year) {
		row := []any{label, y.Invoices, y.Billed}
		for _, p := range set.Payers {
			var py calc.PayerYear
			for _, x := range y.Payers {
				if x.PayerID == p.ID {
					py = x
				}
			}
			row = append(row, py.Expected, py.Actual)
		}
		ys.put(append(row, y.OwnShare, y.Allowances)...)
	}
	for _, y := range years {
		writeYear(y.Year, y)
	}
	if len(years) > 1 {
		writeYear("Gesamt", calc.Total(set, years))
	}

	// Allowances.
	al := newSheet("Leistungen ohne Rechnung")
	al.header("Datum", "Kostenträger", "Art", "Betrag", "Notiz")
	for _, a := range allowances {
		if a.Deleted || year != 0 && a.Date.Year() != year {
			continue
		}
		name := a.PayerID
		if p, ok := set.Payer(a.PayerID); ok {
			name = p.Name
		}
		kind := a.Kind
		if kind == model.AllowanceHospitalDay {
			kind = "Krankenhaustagegeld"
		}
		al.put(a.Date, name, kind, a.Amount, a.Note)
	}
	_ = f.SetDocProps(&excelize.DocProperties{Title: "PKV Beihilfe Tracker – Export", Created: time.Now().Format(time.RFC3339)})
	f.SetActiveSheet(0)
	return f, nil
}

func ptr[T any](v T) *T { return &v }
