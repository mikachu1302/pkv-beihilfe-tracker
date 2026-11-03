package airead

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/extract"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// Limits of plausible values.
const (
	maxText      = 120          // characters of a name or number
	maxAmount    = 1_000_000_00 // cents: a million euros
	treatGrace   = 31           // days the treatment may end after the invoice date (invoices issued in advance)
	maxDueAfter  = 366          // days between invoice date and due date
	pastYears    = 10           // how old a document may be
	futureMargin = 31           // days a date may lie in the future
	ibanNote     = "Die IBAN der KI (%s) hat eine falsche Prüfziffer – nicht übernommen."
)

// check turns the model's answer into values the app can take: tidied up,
// implausible ones dropped (with a note), each with its source – confirmed
// when something independent backs it:
//
//   - amount, invoice number, IBAN: also in the recognised text;
//   - provider: the name of an earlier invoice.
//
// Dates are never confirmed by the text alone (a document has many); they
// are confirmed when the text recognition read the same (see Merge).
func check(a Answer, ocr extract.Draft, ev extract.Evidence, in Input) (map[string]field, []string) {
	out := map[string]field{}
	var notes []string
	put := func(f, v string, confirmed bool) {
		if v == "" {
			return
		}
		src := extract.SrcAI
		if confirmed {
			src = extract.SrcAIConfirmed
		}
		out[f] = field{v, src}
	}

	provider := text(a.Behandler)
	put("provider", provider, isKnown(in.Known, provider))
	if payee := text(a.Zahlungsempfaenger); !strings.EqualFold(compact(payee), compact(provider)) {
		put("payee", payee, false)
	}
	ref := text(a.Rechnungsnummer)
	put("reference", ref, ev.HasText(ref))
	if iban := strings.ToUpper(strings.ReplaceAll(text(a.IBAN), " ", "")); iban != "" {
		if extract.ValidIBAN(iban) {
			put("iban", iban, ev.HasText(iban))
		} else {
			notes = append(notes, fmt.Sprintf(ibanNote, iban))
		}
	}
	if a.Kategorie != "" && slices.Contains(categories, a.Kategorie) {
		put("category", a.Kategorie, false)
	}

	if s := text(a.Betrag); s != "" {
		c, err := money.Parse(s)
		if err != nil || c <= 0 || c >= maxAmount {
			notes = append(notes, fmt.Sprintf("Den Betrag der KI („%s“) kann die App nicht verwenden.", s))
		} else {
			put("amount", c.String(), ev.HasAmount(c))
		}
	}

	dates, dropped := checkDates(a, ocr, civil.Of(in.Now))
	for f, d := range dates {
		put(f, d.String(), false)
	}
	if dropped != "" {
		notes = append(notes, dropped)
	}
	return out, notes
}

// checkDates reads the dates of the answer and keeps the plausible ones: not
// more than pastYears old or more than a month ahead, the treatment not
// after the invoice (a month's grace), its end not before its start, the
// due date within a year after the invoice. dropped tells what was dropped.
func checkDates(a Answer, ocr extract.Draft, today civil.Date) (map[string]civil.Date, string) {
	var bad []string
	read := func(s, label string) civil.Date {
		s = text(s)
		if s == "" {
			return civil.Date{}
		}
		d, err := civil.Parse(s)
		if err != nil || d.Before(today.AddYears(-pastYears)) || d.After(today.AddDays(futureMargin)) {
			bad = append(bad, fmt.Sprintf("%s „%s“", label, s))
			return civil.Date{}
		}
		return d
	}
	inv := read(a.Rechnungsdatum, "Rechnungsdatum")
	from := read(a.BehandlungVon, "Behandlung von")
	to := read(a.BehandlungBis, "Behandlung bis")
	due := read(a.FaelligAm, "Fällig am")

	// Checked against the invoice date that will most likely be taken.
	invoice := inv
	if invoice.IsZero() {
		invoice = ocr.InvoiceDate
	}
	late := func(d civil.Date) bool { return !invoice.IsZero() && d.After(invoice.AddDays(treatGrace)) }
	if !from.IsZero() && late(from) {
		bad = append(bad, "Behandlung von "+from.DE()+" (nach der Rechnung)")
		from = civil.Date{}
	}
	if !to.IsZero() && (late(to) || !from.IsZero() && to.Before(from)) {
		bad = append(bad, "Behandlung bis "+to.DE())
		to = civil.Date{}
	}
	if !due.IsZero() && !invoice.IsZero() && (due.Before(invoice) || due.After(invoice.AddDays(maxDueAfter))) {
		bad = append(bad, "Fällig am "+due.DE())
		due = civil.Date{}
	}
	out := map[string]civil.Date{}
	for f, d := range map[string]civil.Date{"invoiceDate": inv, "serviceFrom": from, "serviceTo": to, "due": due} {
		if !d.IsZero() {
			out[f] = d
		}
	}
	if len(bad) == 0 {
		return out, ""
	}
	return out, "Unplausible Datumsangaben der KI nicht übernommen: " + strings.Join(bad, ", ") + "."
}

// text tidies a value of the answer: single spaces, no quotes around it;
// overlong values (a model rambling) are dropped.
func text(s string) string {
	s = strings.Trim(strings.Join(strings.Fields(s), " "), `"'„“`)
	if utf8.RuneCountInString(s) > maxText {
		return ""
	}
	return s
}

// isKnown: name is the provider of an earlier invoice (ignoring case, spaces and punctuation).
func isKnown(known []string, name string) bool {
	if name == "" {
		return false
	}
	c := compact(name)
	return slices.ContainsFunc(known, func(k string) bool { return strings.EqualFold(compact(k), c) })
}
