package extract

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// A prescription (Privatrezept) comes back from the pharmacy with a machine
// imprint: the pharmacy's IK number between plus signs ("+8517001+"), the
// date as DDMMYY ("020126"), the total ("27,21") and one line per drug with
// PZN, quantity and price in cents ("07116987 1 1376"). The pharmacy's short
// name is printed too ("Donau-Apo 86643"). It has been paid at the counter.
var (
	ikRe      = regexp.MustCompile(`\+\d{7}\+`)
	pznLineRe = regexp.MustCompile(`\b(\d{7,8})\s+(\d{1,2})\s+(\d{3,6})\b`) // PZN, quantity, price in cents
	date6Re   = regexp.MustCompile(`\b(\d{2})(\d{2})(\d{2})\b`)
	apoRe     = regexp.MustCompile(`(?i)apo(theke)?\b`)
	apoNameRe = regexp.MustCompile(`(?i)^\p{L}[\p{L}.\-]*apo(theke)?$`)
)

type prescriptionDraft struct {
	date     civil.Date
	amount   money.Cents
	pharmacy string
	notes    []string
}

// prescription recognises the pharmacy imprint of a prescription.
func prescription(text []string, now time.Time) (prescriptionDraft, bool) {
	var p prescriptionDraft
	all := strings.Join(text, "\n")
	if !apoRe.MatchString(all) {
		return p, false
	}
	signals := 0
	if ikRe.MatchString(all) {
		signals++
	}
	var taxe money.Cents
	pzn := 0
	for _, t := range text {
		for _, m := range pznLineRe.FindAllStringSubmatch(t, -1) {
			if v, err := strconv.Atoi(m[3]); err == nil {
				taxe += money.Cents(v)
				pzn++
			}
		}
	}
	if pzn > 0 {
		signals++
	}
	p.date = prescriptionDate(text, now)
	if !p.date.IsZero() {
		signals++
	}
	if signals < 2 {
		return p, false
	}

	// The total is the only amount with a decimal comma; the drug prices are in cents.
	for _, t := range text {
		for _, s := range amountRe.FindAllString(t, -1) {
			if v, err := money.Parse(s); err == nil && v > p.amount {
				p.amount = v
			}
		}
	}
	// The printed total is what was paid. The prices often come out
	// incomplete through the stamp; their sum only stands in for a missing total.
	if p.amount == 0 {
		p.amount = taxe
	}
	p.pharmacy = pharmacyName(text)
	if p.date.IsZero() {
		p.notes = append(p.notes, "Rezept erkannt, aber kein Abgabedatum gefunden")
	}
	return p, true
}

// prescriptionDate finds the first DDMMYY number that is a plausible date.
func prescriptionDate(text []string, now time.Time) civil.Date {
	today := civil.Of(now)
	for _, t := range text {
		for _, m := range date6Re.FindAllStringSubmatch(t, -1) {
			d, _ := strconv.Atoi(m[1])
			mo, _ := strconv.Atoi(m[2])
			y, _ := strconv.Atoi(m[3])
			if d < 1 || d > 31 || mo < 1 || mo > 12 {
				continue
			}
			dt := civil.New(2000+y, time.Month(mo), d)
			if dt.Time().Day() != d || dt.After(today.AddDays(1)) || dt.Before(today.AddYears(-4)) {
				continue // 31.02. rolled over, or not a date of the last years
			}
			return dt
		}
	}
	return civil.Date{}
}

// pharmacyName returns the printed short name ("Donau-Apo" → "Donau-Apotheke").
func pharmacyName(text []string) string {
	for i := len(text) - 1; i >= 0; i-- { // printed below the stamp, near the end
		for _, w := range strings.Fields(text[i]) {
			w = strings.Trim(w, `.,;:|"'`)
			if !apoNameRe.MatchString(w) {
				continue
			}
			if l := strings.ToLower(w); strings.HasSuffix(l, "apo") {
				w = w[:len(w)-3] + "Apotheke"
			}
			return w
		}
	}
	return "Apotheke"
}

// applyPrescription fills the draft from a recognised prescription.
func applyPrescription(d *Draft, p prescriptionDraft) {
	d.Kind = KindInvoice
	d.Category = model.CatPharmacy
	d.Provider = p.pharmacy
	d.Sources["provider"] = SrcGuess
	if p.amount > 0 {
		d.Amount = p.amount
		d.Sources["amount"] = SrcLabel
	}
	if !p.date.IsZero() {
		d.InvoiceDate = p.date
		d.Paid = p.date // paid at the pharmacy counter
		d.Sources["invoiceDate"] = SrcLabel
	}
	d.Notes = append(d.Notes, "Rezept erkannt – an der Apotheke bezahlt")
	d.Notes = append(d.Notes, p.notes...)
}
