package ask

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/calc"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// Result is the answer to a query.
type Result struct {
	Answer   string   // the answer as a sentence the app builds
	Figures  []Figure // the numbers of the answer
	Groups   []Group  // per year, month, payer … (with a grouping or for the Ausstand)
	Invoices []string // IDs of the matching invoices, in the order to show them
}

// Figure is a labelled number, formatted for the UI ("3.041,23 €", "12").
type Figure struct {
	Label string
	Value string
}

// Group is one line of a grouped answer.
type Group struct {
	Label string
	Count int    // invoices in the group
	Value string // the measure, formatted; "" for counts
}

// Labels of the measures.
var measureLabels = map[string]string{
	MeasureAmount: "Rechnungsbetrag", MeasureExpected: "SOLL", MeasureActual: "Erstattet (IST)",
	MeasureDiff: "Differenz (IST − SOLL)", MeasureOwed: "Noch offen", MeasureOwnShare: "Eigenanteil",
}

var months = []string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}

// Run answers the query from the data. today counts for deadlines, waiting
// times and "dieses Jahr".
func Run(d Data, q Query, today civil.Date) Result {
	switch q.Kind {
	case KindUnsupported:
		return unsupported(q)
	case KindPremium:
		return premium(d, q, today)
	case KindOutstanding:
		return outstanding(d, q, today)
	}
	ms := relevant(selection(d, q, today), q.Measure)
	if len(ms) == 0 {
		return Result{Answer: "Keine Rechnung passt dazu.", Invoices: []string{}}
	}
	// The sentence says it all; figures only where there are several numbers.
	r := Result{Invoices: order(ms, q.Measure)}
	n := invoices(len(ms))
	switch {
	case q.Kind == KindCount:
		r.Answer = n
	case q.Kind == KindSum && q.Measure == "":
		// "SOLL und IST der Beihilfe": the payer figures together.
		for _, m := range []string{MeasureExpected, MeasureActual, MeasureDiff, MeasureOwed} {
			r.Figures = append(r.Figures, Figure{measureLabels[m], total(ms, m).EUR()})
		}
		r.Answer = fmt.Sprintf("SOLL %s, IST %s (%s)", total(ms, MeasureExpected).EUR(), total(ms, MeasureActual).EUR(), n)
	case q.Kind == KindSum:
		r.Answer = fmt.Sprintf("%s: %s (%s)", measureLabels[q.Measure], total(ms, q.Measure).EUR(), n)
	default: // a list
		m := cmp.Or(q.Measure, MeasureAmount)
		r.Answer = fmt.Sprintf("%s, %s zusammen %s", n, measureLabels[m], total(ms, m).EUR())
	}
	if q.GroupBy != "" {
		r.Groups = groups(d.Settings, ms, q)
	}
	return r
}

// relevant drops the invoices a payer figure says nothing about: "Was hat
// die PKV erstattet?" counts the refunded ones, "Was ist noch offen?" those
// with something owed.
func relevant(ms []hit, measure string) []hit {
	keep := claimCounts(measure)
	return slices.DeleteFunc(ms, func(m hit) bool { return !slices.ContainsFunc(m.claims, keep) })
}

// claimCounts: whether a claim says something about the measure (IST and
// difference: refunded; open: something owed; the others: every claim).
func claimCounts(measure string) func(calc.ClaimView) bool {
	switch measure {
	case MeasureActual, MeasureDiff:
		return func(c calc.ClaimView) bool { return c.Status == calc.StatusRefunded }
	case MeasureOwed:
		return func(c calc.ClaimView) bool { return c.Owed() != 0 }
	}
	return func(calc.ClaimView) bool { return true }
}

func unsupported(q Query) Result {
	a := "Darauf kann die App nicht antworten – sie rechnet nur mit den eigenen Rechnungen und Erstattungen."
	if q.Reason != "" {
		a += " (" + strings.TrimSuffix(q.Reason, ".") + ")"
	}
	return Result{Answer: a, Invoices: []string{}}
}

func total(ms []hit, measure string) money.Cents {
	var sum money.Cents
	for _, m := range ms {
		sum += value(m, measure)
	}
	return sum
}

// order lists the invoices: by the size of the measure asked for (largest
// first, a cut as large as a surplus), else the newest treatment first.
func order(ms []hit, measure string) []string {
	sorted := slices.Clone(ms)
	slices.SortStableFunc(sorted, func(a, b hit) int {
		if measure != "" {
			if c := cmp.Compare(value(b, measure).Abs(), value(a, measure).Abs()); c != 0 {
				return c
			}
		}
		return b.inv.ServiceDate().Compare(a.inv.ServiceDate())
	})
	ids := make([]string, len(sorted))
	for i, m := range sorted {
		ids[i] = m.inv.ID
	}
	return ids
}

// groups splits the matches by the grouping of q. By payer, an invoice
// counts once for each payer it has a claim with that says something about
// the measure; an invoice amount has no share per payer, so SOLL and IST
// are shown instead.
func groups(s *model.Settings, ms []hit, q Query) []Group {
	measure := q.Measure
	if q.Kind == KindList {
		measure = cmp.Or(measure, MeasureAmount) // as the list's total
	}
	if q.GroupBy == GroupPayer && (measure == MeasureAmount || measure == MeasureOwnShare) {
		measure = ""
	}
	type acc struct {
		key, label string
		count      int
		sum        money.Cents
		expected   money.Cents // for the SOLL/IST overview
		actual     money.Cents
	}
	byKey := map[string]*acc{}
	add := func(key, label string, m hit) {
		a := byKey[key]
		if a == nil {
			a = &acc{key: key, label: label}
			byKey[key] = a
		}
		a.count++
		a.sum += value(m, measure)
		a.expected += value(m, MeasureExpected)
		a.actual += value(m, MeasureActual)
	}
	for _, m := range ms {
		switch q.GroupBy {
		case GroupYear:
			y := m.inv.ServiceYear()
			label := strconv.Itoa(y)
			if y == 0 {
				label = "ohne Datum"
			}
			add(fmt.Sprintf("%04d", y), label, m)
		case GroupMonth:
			d := m.inv.ServiceDate()
			if d.IsZero() {
				add("0000-00", "ohne Datum", m)
			} else {
				add(d.String()[:7], months[d.Time().Month()-1]+" "+strconv.Itoa(d.Year()), m)
			}
		case GroupCategory:
			add(string(m.inv.Category), cmp.Or(m.inv.Category.Label(), "ohne Kategorie"), m)
		case GroupProvider:
			p := strings.TrimSpace(m.inv.Provider)
			add(strings.ToLower(p), cmp.Or(p, "ohne Behandler"), m)
		case GroupPayer:
			for _, c := range slices.DeleteFunc(slices.Clone(m.claims), func(c calc.ClaimView) bool { return !claimCounts(measure)(c) }) {
				one := m
				one.claims = []calc.ClaimView{c}
				add(c.PayerID, payerName(s, c.PayerID), one)
			}
		}
	}
	list := make([]*acc, 0, len(byKey))
	for _, a := range byKey {
		list = append(list, a)
	}
	chronological := q.GroupBy == GroupYear || q.GroupBy == GroupMonth
	slices.SortFunc(list, func(a, b *acc) int {
		switch {
		case chronological:
			return strings.Compare(a.key, b.key)
		case q.Kind == KindCount || measure == "":
			if c := cmp.Compare(b.count, a.count); c != 0 {
				return c
			}
		default:
			if c := cmp.Compare(b.sum.Abs(), a.sum.Abs()); c != 0 {
				return c
			}
		}
		return strings.Compare(a.label, b.label)
	})
	out := make([]Group, len(list))
	for i, a := range list {
		g := Group{Label: a.label, Count: a.count}
		switch {
		case q.Kind == KindCount:
		case measure == "":
			g.Value = "SOLL " + a.expected.EUR() + " · IST " + a.actual.EUR()
		default:
			g.Value = a.sum.EUR()
		}
		out[i] = g
	}
	return out
}

func payerName(s *model.Settings, id string) string {
	if p, ok := s.Payer(id); ok {
		return p.Name
	}
	return id
}

// invoices: "1 Rechnung", "12 Rechnungen".
func invoices(n int) string {
	if n == 1 {
		return "1 Rechnung"
	}
	return strconv.Itoa(n) + " Rechnungen"
}
