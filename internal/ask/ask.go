// Package ask answers questions about the own invoices. The AI only turns
// a question into a Query – a fixed JSON shape with German keys, as the
// model writes it – and this package runs the query on the data with the
// same calculations as the invoice list, the dashboard and the year
// overview. Deterministic: no AI, no I/O.
package ask

import (
	"slices"
	"strings"
	"unicode"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/calc"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// Query is a question in a form the app can compute.
type Query struct {
	Kind    string   `json:"typ"`         // Kind…
	Measure string   `json:"messgroesse"` // Measure…; "" for counts and lists
	Years   []int    `json:"jahre"`       // Leistungsjahre; empty = all
	Filters []Filter `json:"filter"`      // only the conditions the question names
	GroupBy string   `json:"gruppierung"` // Group…; "" = none
	Reason  string   `json:"grund"`       // why a question is not supported
}

// Filter is one condition: a field (Field…) and its value.
type Filter struct {
	Field string `json:"feld"`
	Value string `json:"wert"`
}

// Kinds of queries.
const (
	KindCount       = "anzahl"
	KindSum         = "summe"
	KindList        = "liste"
	KindPremium     = "beitragsrueckerstattung" // the dashboard's comparison: claim from the PKV or keep the BRE
	KindOutstanding = "ausstand"                // what the payers still owe
	KindUnsupported = "nicht_unterstuetzt"      // not about the own data
)

// Measures.
const (
	MeasureAmount   = "betrag"      // invoice amount
	MeasureExpected = "soll"        // SOLL
	MeasureActual   = "ist"         // IST, refunded
	MeasureDiff     = "differenz"   // IST − SOLL of refunded claims
	MeasureOwed     = "offen"       // SOLL − IST still owed (as Ausstand)
	MeasureOwnShare = "eigenanteil" // amount − IST
)

// Filter fields.
const (
	FieldPayer     = "kostentraeger"   // payer name
	FieldStatus    = "status"          // Status…, per claim
	FieldDeviation = "abweichung"      // zu_wenig, zu_viel
	FieldClarify   = "klaerungsbedarf" // ja, nein
	FieldPaid      = "bezahlt"         // ja, nein: paid to the provider
	FieldDeadline  = "frist_bald"      // ja: claim deadline missed or close
	FieldCategory  = "kategorie"       // model.Category
	FieldProvider  = "behandler"       // text in the provider's (or payee's) name
	FieldFrom      = "von"             // treatment from this date (YYYY-MM-DD)
	FieldTo        = "bis"             // treatment until this date
)

// Claim statuses as the queries name them.
const (
	StatusOpen        = "nicht_eingereicht" // as the list's filter: open or held back
	StatusSubmitted   = "eingereicht"       // refund still open
	StatusRefunded    = "erstattet"
	StatusHeld        = "zurueckgehalten"
	StatusNotRefunded = "nicht_erstattet" // not yet refunded: open, held or submitted
)

// Values of the other filters.
const (
	Yes       = "ja"
	No        = "nein"
	TooLittle = "zu_wenig"
	TooMuch   = "zu_viel"
)

// Groupings.
const (
	GroupYear     = "jahr"
	GroupMonth    = "monat"
	GroupPayer    = "kostentraeger"
	GroupProvider = "behandler"
	GroupCategory = "kategorie"
)

// The values the query model knows, defaults first.
var (
	Kinds    = []string{KindCount, KindSum, KindList, KindPremium, KindOutstanding, KindUnsupported}
	Measures = []string{"", MeasureAmount, MeasureExpected, MeasureActual, MeasureDiff, MeasureOwed, MeasureOwnShare}
	Statuses = []string{StatusOpen, StatusSubmitted, StatusRefunded, StatusHeld, StatusNotRefunded}
	Groups   = []string{"", GroupYear, GroupMonth, GroupPayer, GroupProvider, GroupCategory}
	// Categories in the order of the app.
	Categories = []string{string(model.CatDoctor), string(model.CatDentist), string(model.CatPharmacy), string(model.CatTherapy),
		string(model.CatAids), string(model.CatHospital), string(model.CatNaturopath), string(model.CatOther)}
)

// FilterValues are the allowed values per filter field; nil = free text.
// Payer names come from the settings.
func FilterValues(s *model.Settings) map[string][]string {
	var payers []string
	for _, p := range s.Payers {
		payers = append(payers, p.Name)
	}
	return map[string][]string{
		FieldPayer: payers, FieldStatus: Statuses, FieldDeviation: {TooLittle, TooMuch}, FieldClarify: {Yes, No},
		FieldPaid: {Yes, No}, FieldDeadline: {Yes}, FieldCategory: Categories, FieldProvider: nil, FieldFrom: nil, FieldTo: nil,
	}
}

// FilterFields are the filter fields in the order the model is told about them.
var FilterFields = []string{FieldPayer, FieldStatus, FieldDeviation, FieldClarify, FieldPaid, FieldDeadline, FieldCategory,
	FieldProvider, FieldFrom, FieldTo}

// Get returns the value of a filter field ("" if the query has none).
func (q Query) Get(field string) string {
	for _, f := range q.Filters {
		if f.Field == field {
			return f.Value
		}
	}
	return ""
}

// Data is what a query runs on.
type Data struct {
	Settings   *model.Settings
	Invoices   []model.Invoice
	Allowances []model.Allowance
}

// match is an invoice that matches a query, with the claims that count.
type match struct {
	inv    *model.Invoice
	view   calc.InvoiceView
	claims []calc.ClaimView // of the payer asked for that meet the claim conditions
}

// selection finds the invoices of the query: years, invoice conditions
// (paid, category, provider, dates) and claim conditions (payer, status,
// deviation, Klärungsbedarf, deadline) – an invoice matches when one of its
// claims meets all claim conditions. Claims marked "wird nicht eingereicht"
// never count: the payer is out of it.
func selection(d Data, q Query, today civil.Date) []match {
	payer := payerID(d.Settings, q.Get(FieldPayer))
	from, _ := civil.Parse(q.Get(FieldFrom))
	to, _ := civil.Parse(q.Get(FieldTo))
	var out []match
	for i := range d.Invoices {
		inv := &d.Invoices[i]
		if inv.Deleted || len(q.Years) > 0 && !slices.Contains(q.Years, inv.ServiceYear()) {
			continue
		}
		switch q.Get(FieldPaid) {
		case Yes:
			if !inv.Paid.Done() {
				continue
			}
		case No:
			if inv.Paid.Done() {
				continue
			}
		}
		if c := q.Get(FieldCategory); c != "" && string(inv.Category) != c {
			continue
		}
		if p := q.Get(FieldProvider); p != "" && !nameMatches(p, inv.Provider) && !nameMatches(p, inv.Payee) {
			continue
		}
		date := inv.ServiceDate()
		if !from.IsZero() && (date.IsZero() || date.Before(from)) || !to.IsZero() && (date.IsZero() || date.After(to)) {
			continue
		}
		v := calc.Invoice(d.Settings, inv, today)
		var claims []calc.ClaimView
		for _, c := range v.Claims {
			if c.Status != calc.StatusSkipped && claimMatches(c, q, payer) {
				claims = append(claims, c)
			}
		}
		if len(claims) == 0 && (payer != "" || hasClaimFilter(q)) {
			continue
		}
		out = append(out, match{inv: inv, view: v, claims: claims})
	}
	return out
}

func hasClaimFilter(q Query) bool {
	return slices.ContainsFunc(q.Filters, func(f Filter) bool {
		return f.Field == FieldStatus || f.Field == FieldDeviation || f.Field == FieldClarify || f.Field == FieldDeadline
	})
}

func claimMatches(c calc.ClaimView, q Query, payer string) bool {
	if payer != "" && c.PayerID != payer {
		return false
	}
	switch q.Get(FieldStatus) {
	case StatusOpen:
		if c.Status != calc.StatusOpen && c.Status != calc.StatusHeld {
			return false
		}
	case StatusSubmitted:
		if c.Status != calc.StatusSubmitted {
			return false
		}
	case StatusRefunded:
		if c.Status != calc.StatusRefunded {
			return false
		}
	case StatusHeld:
		if c.Status != calc.StatusHeld {
			return false
		}
	case StatusNotRefunded:
		if c.Status != calc.StatusOpen && c.Status != calc.StatusHeld && c.Status != calc.StatusSubmitted {
			return false
		}
	}
	// A deviation of a cent is rounding, not a cut.
	switch q.Get(FieldDeviation) {
	case TooLittle:
		if c.Status != calc.StatusRefunded || c.Diff >= -1 {
			return false
		}
	case TooMuch:
		if c.Status != calc.StatusRefunded || c.Diff <= 1 {
			return false
		}
	}
	// Klärungsbedarf as the list's filter: marked, or suggested by a deviation above the tolerance.
	clarify := c.Clarify.Done() || c.SuggestClarify
	switch q.Get(FieldClarify) {
	case Yes:
		if !clarify {
			return false
		}
	case No:
		if clarify {
			return false
		}
	}
	if q.Get(FieldDeadline) == Yes && !c.DeadlineSoon() {
		return false
	}
	return true
}

// payerID finds the payer by name (ignoring case); "" if not asked for or unknown.
func payerID(s *model.Settings, name string) string {
	if name == "" {
		return ""
	}
	for _, p := range s.Payers {
		if strings.EqualFold(p.Name, name) {
			return p.ID
		}
	}
	return "-" // asked for a payer the settings do not know: nothing matches
}

// nameMatches: the words of want appear in name, ignoring case, umlauts,
// punctuation and titles ("Dr. Muster" finds "Praxis Dr. med. Muster").
func nameMatches(want, name string) bool {
	w, n := words(want), words(name)
	if len(w) == 0 || len(n) == 0 {
		return false
	}
	for _, x := range w {
		if !slices.ContainsFunc(n, func(y string) bool { return strings.HasPrefix(y, x) }) {
			return false
		}
	}
	return true
}

var titles = map[string]bool{"dr": true, "med": true, "dent": true, "prof": true, "praxis": true}

func words(s string) []string {
	s = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss").Replace(strings.ToLower(s))
	var out []string
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if !titles[w] {
			out = append(out, w)
		}
	}
	return out
}

// value is the measure for one matched invoice: invoice amounts as they
// are, payer figures over the claims that count.
func value(m match, measure string) money.Cents {
	var sum money.Cents
	switch measure {
	case MeasureAmount:
		return m.inv.Amount
	case MeasureOwnShare:
		return m.view.OwnShare
	}
	for _, c := range m.claims {
		switch measure {
		case MeasureExpected:
			sum += c.Expected
		case MeasureActual:
			sum += c.Actual
		case MeasureDiff:
			if c.Status == calc.StatusRefunded {
				sum += c.Diff
			}
		case MeasureOwed:
			sum += c.Owed()
		}
	}
	return sum
}
