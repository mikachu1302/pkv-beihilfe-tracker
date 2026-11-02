package xlsximport

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
)

var now = time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// legacyWorkbook builds a synthetic spreadsheet in the layout of the old
// "Abrechnung Krankenkasse" (header typos included). No real data.
func legacyWorkbook(t *testing.T) *bytes.Buffer {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	const sh = "Auflistung"
	f.SetSheetName("Sheet1", sh)
	dateStyle, _ := f.NewStyle(&excelize.Style{NumFmt: 14})
	headers := []any{"Rechnungsdatum ", "Behandlungsdatum", "Anweisung am", "Behandler", "Bemerkung", "Gesamtbetrag",
		"Bretrag 50/50", "Eingereicht Beihilfe ", "Betrag Erstattet Beihilfe", "Eingereicht Krankenkasse ",
		"Betrag Erstattet Krankenkasse", "Diferenzbertag", "Bemerkung", "Krankenhaus Tagegeld", "Jahres Zusammenfassung "}
	f.SetSheetRow(sh, "A1", &headers)
	set := func(cell string, v any) {
		f.SetCellValue(sh, cell, v)
		if _, ok := v.(time.Time); ok {
			f.SetCellStyle(sh, cell, cell, dateStyle)
		}
	}
	// Row 2: complete, both payers refunded.
	set("A2", day("2023-04-11"))
	set("B2", day("2023-04-10"))
	set("C2", day("2023-04-20"))
	set("D2", "Apotheke Beispiel")
	set("F2", 73.18)
	f.SetCellFormula(sh, "G2", "F2/2")
	set("H2", day("2023-05-15"))
	set("I2", 36.59)
	set("J2", day("2023-10-02"))
	set("K2", 36.6)
	f.SetCellFormula(sh, "L2", "F2-I2-K2")
	set("O2", 2023)
	// Row 3: typo year 2014, many decimals, PKV not submitted.
	set("A3", day("2023-06-07"))
	set("B3", day("2023-06-07"))
	set("D3", "Dr. Beispiel")
	set("E3", "Behandlungszeitraum 02.05.–30.05.23")
	set("F3", 151.237142857143)
	set("H3", day("2013-09-17"))
	set("I3", 75)
	set("O3", "Auslage 100%")
	// Row 4: separator with a summary formula only.
	f.SetCellFormula(sh, "O4", "SUM(F2:F3)")
	// Row 5: PKV date before the invoice, refunds above the amount, Krankenhaustagegeld.
	set("A5", day("2024-02-14"))
	set("D5", "Zahnarztpraxis Muster")
	set("F5", 180.40)
	set("H5", day("2024-03-01"))
	set("I5", 250)
	set("J5", day("2024-02-02"))
	set("K5", 250)
	set("M5", "Krankenhaus")
	set("N5", 450)
	// Row 6: date and amount typed as text.
	set("A6", "18.06.2025")
	set("D6", "Physio Punkt")
	set("F6", "1.234,56")

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return &buf
}

func TestImportLegacyLayout(t *testing.T) {
	res, err := Import(legacyWorkbook(t), Options{BeihilfePayerID: "bh", PKVPayerID: "pkv"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sheet != "Auflistung" || len(res.Invoices) != 4 || fmt.Sprint(res.Rows) != "[2 3 5 6]" || fmt.Sprint(res.AllowanceOf) != "[2]" {
		t.Fatalf("sheet %q, %d invoices", res.Sheet, len(res.Invoices))
	}
	r2 := res.Invoices[0]
	if r2.Provider != "Apotheke Beispiel" || r2.Category != model.CatPharmacy || r2.InvoiceDate.String() != "2023-04-11" ||
		r2.ServiceFrom.String() != "2023-04-10" || r2.Amount != 7318 || r2.Paid.Date.String() != "2023-04-20" {
		t.Errorf("row 2: %+v", r2)
	}
	bh, pkv := r2.Claims["bh"], r2.Claims["pkv"]
	if bh.Submitted.Date.String() != "2023-05-15" || len(bh.Refunds) != 1 || bh.Refunds[0].Amount != 3659 ||
		pkv.Submitted.Date.String() != "2023-10-02" || pkv.Refunds[0].Amount != 3660 {
		t.Errorf("row 2 claims: %+v", r2.Claims)
	}
	r3 := res.Invoices[1]
	if !r3.ServiceFrom.IsZero() || r3.Amount != 15124 || r3.Note != "Behandlungszeitraum 02.05.–30.05.23" || len(r3.Claims) != 1 {
		t.Errorf("row 3: %+v", r3)
	}
	r5 := res.Invoices[2]
	if r5.Category != model.CatDentist || r5.Note != "Krankenhaus" {
		t.Errorf("row 5: %+v", r5)
	}
	r6 := res.Invoices[3]
	if r6.InvoiceDate.String() != "2025-06-18" || r6.Amount != 123456 || r6.Category != model.CatTherapy {
		t.Errorf("row 6: %+v", r6)
	}
	if len(res.Allowances) != 1 || res.Allowances[0].Amount != 45000 || res.Allowances[0].Date.String() != "2024-02-14" ||
		res.Allowances[0].PayerID != "pkv" {
		t.Errorf("allowances: %+v", res.Allowances)
	}
	want := map[int][]string{3: {"ungewöhnliches Jahr"}, 5: {"vor dem Rechnungsdatum", "höher als der Rechnungsbetrag (180,40 €)"}}
	if len(res.Warnings) != 3 {
		t.Errorf("warnings: %+v", res.Warnings)
	}
	for _, w := range res.Warnings {
		found := false
		for _, frag := range want[w.Row] {
			found = found || strings.Contains(w.Text, frag)
		}
		if !found {
			t.Errorf("unexpected warning %+v", w)
		}
	}
}

func TestImportWithoutBeihilfe(t *testing.T) {
	res, err := Import(legacyWorkbook(t), Options{PKVPayerID: "pkv"}, now)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, w := range res.Warnings {
		if strings.Contains(w.Text, "Beihilfe-Spalten") {
			n++
		}
	}
	if n != 3 {
		t.Errorf("expected a warning for each of the 3 rows with Beihilfe data: %+v", res.Warnings)
	}
	for _, inv := range res.Invoices {
		if _, ok := inv.Claims["bh"]; ok {
			t.Error("Beihilfe claim created without payer")
		}
	}
}

func TestNoTable(t *testing.T) {
	f := excelize.NewFile()
	f.SetCellValue("Sheet1", "A1", "Einkaufsliste")
	var buf bytes.Buffer
	f.Write(&buf)
	f.Close()
	if _, err := Import(&buf, Options{PKVPayerID: "pkv"}, now); !errors.Is(err, ErrNoTable) {
		t.Errorf("err = %v", err)
	}
	if _, err := Import(legacyWorkbook(t), Options{}, now); err == nil {
		t.Error("missing PKV payer accepted")
	}
}

// TestImportFile runs the importer on a real spreadsheet when PKV_XLSX points
// to one. It only prints counts; nothing of the file ends up in the repo.
func TestImportFile(t *testing.T) {
	path := os.Getenv("PKV_XLSX")
	if path == "" {
		t.Skip("set PKV_XLSX to a spreadsheet to try the importer on it")
	}
	res, err := ImportFile(path, Options{BeihilfePayerID: "bh", PKVPayerID: "pkv"}, now)
	if err != nil {
		t.Fatal(err)
	}
	years := map[int]int{}
	for _, inv := range res.Invoices {
		years[inv.ServiceYear()]++
	}
	t.Logf("sheet %q: %d invoices %v, %d allowances, %d warnings", res.Sheet, len(res.Invoices), years, len(res.Allowances), len(res.Warnings))
	for _, w := range res.Warnings {
		t.Logf("row %d: %s", w.Row, w.Text)
	}
}

func TestSheetsTotalsAndZeroRefunds(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	headers := []any{"Rechnungsdatum", "Behandler", "Gesamtbetrag", "Eingereicht Krankenkasse", "Betrag Erstattet Krankenkasse"}
	for i, sh := range []string{"2024", "2025"} {
		if i == 0 {
			f.SetSheetName("Sheet1", sh)
		} else {
			f.NewSheet(sh)
		}
		f.SetSheetRow(sh, "A1", &headers)
		f.SetSheetRow(sh, "A2", &[]any{fmt.Sprintf("1%d.03.%s", i, sh), "Praxis " + sh, 100, "", 0})
		f.SetSheetRow(sh, "A4", &[]any{nil, nil, 100}) // total below the table
	}
	var buf bytes.Buffer
	f.Write(&buf)
	res, err := Import(&buf, Options{PKVPayerID: "pkv"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sheet != "2024, 2025" || len(res.Invoices) != 2 || fmt.Sprint(res.RowSheets) != "[2024 2025]" {
		t.Fatalf("sheets %q, invoices %+v", res.Sheet, res.Invoices)
	}
	if c := res.Invoices[0].Claims["pkv"]; len(c.Refunds) != 0 {
		t.Errorf("0 imported as a refund: %+v", c)
	}
}
