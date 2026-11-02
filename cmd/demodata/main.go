// Command demodata fills a new data folder with made-up invoices for trying
// out the app and for screenshots. Nothing in it is real.
//
//	go run ./cmd/demodata /tmp/pkv-demo
package main

import (
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"time"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/service"
)

var providers = []struct {
	name, category string
	min, max       float64
}{
	{"Dr. Muster (Allgemeinmedizin)", "arzt", 40, 180},
	{"Hautarztpraxis Dr. Klar", "arzt", 60, 240},
	{"Zahnarztpraxis Sonnenschein", "zahnarzt", 90, 1400},
	{"Apotheke am Markt", "apotheke", 8, 120},
	{"Physiotherapie Bewegung", "heilmittel", 180, 420},
	{"Radiologie Mitte", "arzt", 150, 900},
	{"Labor Nord", "arzt", 20, 140},
	{"Optik Scharf", "hilfsmittel", 120, 600},
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: demodata <empty folder>")
	}
	dir := os.Args[1]
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		log.Fatalf("%s is not empty", dir)
	}
	cfg, err := os.MkdirTemp("", "pkv-demo-config")
	if err != nil {
		log.Fatal(err)
	}
	cleanup = func() { _ = os.RemoveAll(cfg) }
	defer cleanup()

	s := service.NewWithConfigDir(cfg)
	must(s.SetDataDir(dir))
	must(s.Setup(service.SetupDTO{WithBeihilfe: true, BeihilfeName: "Beihilfe Bund", BeihilfeRate: "50", BundRules: true,
		PKVName: "Muster Krankenversicherung", From: "2024-01-01"}))
	// Retirement in 2026: Beihilfe 70 %, PKV 30 %.
	set, err := s.GetSettings()
	check(err)
	set.Coverage = append(set.Coverage, service.CoverageDTO{From: "2026-01-01", Shares: []service.ShareDTO{
		{PayerID: set.Payers[0].ID, Rate: "70"}, {PayerID: set.Payers[1].ID, Rate: "30"}}})
	must(s.SaveSettings(set, nil))
	bh, pkv := set.Payers[0].ID, set.Payers[1].ID

	r := rand.New(rand.NewPCG(2026, 11))
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	cutoff := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	count := 0
	for t := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC); t.Before(cutoff); t = t.AddDate(0, 0, 9+r.IntN(20)) {
		p := providers[r.IntN(len(providers))]
		amount := p.min + r.Float64()*(p.max-p.min)
		invoiceDate := t.AddDate(0, 0, 3+r.IntN(25))
		det, err := s.SaveInvoice(service.InvoiceDTO{Provider: p.name, Category: p.category, InvoiceDate: day(invoiceDate),
			ServiceFrom: day(t), Amount: fmt.Sprintf("%.2f", amount), Reference: fmt.Sprintf("R-%05d", 1000+count)}, nil)
		check(err)
		id := det.Invoice.ID
		count++
		if invoiceDate.Before(cutoff.AddDate(0, 0, -20)) || r.IntN(2) == 0 {
			must(s.SetMark(service.MarkChangeDTO{InvoiceID: id, Mark: service.MarkPaid, On: true, Date: day(invoiceDate.AddDate(0, 0, 5+r.IntN(10)))}))
		}
		age := cutoff.Sub(invoiceDate).Hours() / 24
		// Beihilfe: older invoices are submitted and refunded, some with deviations.
		if age > 25 {
			sub := invoiceDate.AddDate(0, 0, 10+r.IntN(30))
			if sub.After(cutoff) {
				sub = cutoff
			}
			must(s.SetMark(service.MarkChangeDTO{InvoiceID: id, PayerID: bh, Mark: service.MarkSubmitted, On: true, Date: day(sub)}))
			if age > 60 {
				refund := ""
				if r.IntN(6) == 0 { // part not eligible
					refund = fmt.Sprintf("%.2f", amount*0.5*(0.6+r.Float64()*0.3))
				}
				must(s.SetMark(service.MarkChangeDTO{InvoiceID: id, PayerID: bh, Mark: service.MarkRefunded, On: true,
					Date: day(sub.AddDate(0, 0, 14+r.IntN(20))), Amount: refund}))
				if refund != "" && r.IntN(2) == 0 {
					must(s.SetMark(service.MarkChangeDTO{InvoiceID: id, PayerID: bh, Mark: service.MarkClarify, On: true,
						Date: day(sub.AddDate(0, 0, 40)), Note: "Steigerungssatz gekürzt"}))
				}
			}
		}
		// PKV: 2024 claimed, 2025/2026 held back for the premium refund.
		switch {
		case t.Year() == 2024 && age > 40:
			sub := time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC)
			must(s.SetMark(service.MarkChangeDTO{InvoiceID: id, PayerID: pkv, Mark: service.MarkSubmitted, On: true, Date: day(sub)}))
			must(s.SetMark(service.MarkChangeDTO{InvoiceID: id, PayerID: pkv, Mark: service.MarkRefunded, On: true, Date: day(sub.AddDate(0, 0, 12))}))
		default:
			must(s.SetMark(service.MarkChangeDTO{InvoiceID: id, PayerID: pkv, Mark: service.MarkHeld, On: true}))
		}
	}
	check(s.SaveYearSetting(service.YearSettingDTO{Year: 2025, PayerID: pkv, ExpectedPremiumRefund: "1350", Deductible: "300"}))
	check(s.SaveYearSetting(service.YearSettingDTO{Year: 2026, PayerID: pkv, ExpectedPremiumRefund: "1350", Deductible: "300"}))
	must(s.SaveAllowance(service.AllowanceDTO{PayerID: pkv, Date: "2025-05-12", Amount: "300", Note: "Krankenhaustagegeld, 3 Tage"}))
	fmt.Printf("%d invoices written to %s\n", count, dir)
}

func must[T any](_ T, err error) { check(err) }

// cleanup removes the temporary config folder; log.Fatal skips deferred calls.
var cleanup = func() {}

func check(err error) {
	if err != nil {
		cleanup()
		log.Fatal(err)
	}
}
