package export

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/phpdave11/gofpdf"
	"github.com/xuri/excelize/v2"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// excelize renders number formats with a decimal point; Excel and LibreOffice
// show them in the user's locale ("286,40 €").
var d = civil.MustParse

func settings() *model.Settings {
	return &model.Settings{
		Payers: []model.Payer{{ID: "bh", Kind: model.Beihilfe, Name: "Beihilfe"}, {ID: "pkv", Kind: model.PKV, Name: "PKV"}},
		Coverage: []model.CoveragePeriod{{From: d("2020-01-01"), Shares: []model.Share{
			{PayerID: "bh", Rate: money.Percent(70)}, {PayerID: "pkv", Rate: money.Percent(30)}}}},
	}
}

func TestWorkbook(t *testing.T) {
	invoices := []model.Invoice{
		{ID: "a", Provider: "Dr. Muster", Category: model.CatDoctor, InvoiceDate: d("2026-03-14"), ServiceFrom: d("2026-03-03"), Amount: 28640,
			Claims: map[string]model.Claim{"bh": {Submitted: model.Mark{Date: d("2026-04-02")}, Refunds: []model.Refund{{ID: "r", Amount: 18020, Date: d("2026-04-28")}},
				Refunded: model.Mark{Date: d("2026-04-28")}, Clarify: model.Mark{Date: d("2026-04-29"), Note: "Steigerungssatz"}}}},
		{ID: "b", Provider: "Apotheke", InvoiceDate: d("2025-12-10"), Amount: 1250},
		{ID: "c", Provider: "gelöscht", Deleted: true, InvoiceDate: d("2026-01-01"), Amount: 999},
	}
	allowances := []model.Allowance{{ID: "k", PayerID: "pkv", Kind: model.AllowanceHospitalDay, Date: d("2026-07-01"), Amount: 45000}}
	f, err := Workbook(settings(), invoices, allowances, 0, d("2026-11-02"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "export.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	r, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if got := r.GetSheetList(); len(got) != 3 || got[0] != "Rechnungen" || got[1] != "Übersicht" {
		t.Fatalf("sheets %v", got)
	}
	rows, _ := r.GetRows("Rechnungen")
	if len(rows) != 3 { // header + 2 invoices (deleted one left out)
		t.Fatalf("rows %d: %v", len(rows), rows)
	}
	want := map[string]string{"Behandler": "Dr. Muster", "Betrag": "286.40 €", "Beihilfe SOLL": "200.48 €", "Beihilfe IST": "180.20 €",
		"Beihilfe Status": "erstattet", "Beihilfe eingereicht": "02.04.2026", "Beihilfe Klärung": "Steigerungssatz", "PKV Status": "nicht eingereicht"}
	for col, h := range rows[0] {
		if w, ok := want[h]; ok {
			if col >= len(rows[1]) || rows[1][col] != w {
				t.Errorf("%s = %q, want %q", h, rows[1][col], w)
			}
		}
	}
	years, _ := r.GetRows("Übersicht")
	if len(years) != 4 || years[3][0] != "Gesamt" {
		t.Errorf("years %v", years)
	}
	al, _ := r.GetRows("Leistungen ohne Rechnung")
	if len(al) != 2 || al[1][3] != "450.00 €" || al[1][2] != "Krankenhaustagegeld" {
		t.Errorf("allowances %v", al)
	}

	one, _ := Workbook(settings(), invoices, allowances, 2025, d("2026-11-02"))
	if rows, _ := one.GetRows("Rechnungen"); len(rows) != 2 || rows[1][3] != "Apotheke" {
		t.Errorf("year filter: %v", rows)
	}
}

func TestBundle(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "rechnung.pdf")
	p := gofpdf.New("P", "mm", "A4", "")
	p.AddPage()
	p.SetFont("Helvetica", "", 12)
	p.Cell(40, 10, "Rechnung")
	if err := p.OutputFileAndClose(pdfPath); err != nil {
		t.Fatal(err)
	}
	imgPath := filepath.Join(dir, "foto.png")
	img := image.NewRGBA(image.Rect(0, 0, 60, 80))
	for x := 0; x < 60; x++ {
		img.Set(x, 40, color.Black)
	}
	fh, _ := os.Create(imgPath)
	png.Encode(fh, img)
	fh.Close()

	out := filepath.Join(dir, "einreichung.pdf")
	res, err := WriteBundle(Bundle{Payer: "Beihilfe Bund", Date: d("2026-11-02"), Items: []BundleItem{
		{Provider: "Dr. Muster", ServiceDate: d("2026-03-03"), InvoiceDate: d("2026-03-14"), Reference: "2026-004711", Amount: 28640, Documents: []string{pdfPath}},
		{Provider: "Apotheke am Markt", InvoiceDate: d("2026-03-05"), Amount: 4510, Documents: []string{imgPath}},
		{Provider: "Ohne Beleg", InvoiceDate: d("2026-03-06"), Amount: 1000},
	}}, out)
	if err != nil {
		t.Fatal(err)
	}
	if res.Documents != 2 || len(res.Missing) != 1 || res.Missing[0] != "Ohne Beleg" {
		t.Errorf("result %+v", res)
	}
	n, err := api.PageCountFile(context.Background(), out)
	if err != nil || n != 3 {
		t.Errorf("pages %d, %v", n, err)
	}

	// Without any documents the cover page alone is written.
	only := filepath.Join(dir, "nur-deckblatt.pdf")
	if _, err := WriteBundle(Bundle{Payer: "PKV", Date: d("2026-11-02"), Items: []BundleItem{{Provider: "X", Amount: 100}}}, only); err != nil {
		t.Fatal(err)
	}
	if n, _ := api.PageCountFile(context.Background(), only); n != 1 {
		t.Errorf("cover only: %d pages", n)
	}
}
