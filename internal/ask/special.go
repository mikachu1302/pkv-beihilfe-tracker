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

// premium is the dashboard's comparison for the years asked for (default
// this year): claim everything from the PKV, or keep the Beitragsrückerstattung.
func premium(d Data, q Query, today civil.Date) Result {
	years := q.Years
	if len(years) == 0 {
		years = []int{today.Year()}
	}
	payer := payerID(d.Settings, q.Get(FieldPayer))
	r := Result{Invoices: []string{}}
	var answers []string
	for _, y := range calc.Years(d.Settings, d.Invoices, d.Allowances, today) {
		if !slices.Contains(years, y.Year) {
			continue
		}
		for _, p := range y.Payers {
			pr := p.PremiumRefund
			if pr == nil || payer != "" && p.PayerID != payer {
				continue
			}
			prefix := ""
			if len(years) > 1 {
				prefix = strconv.Itoa(y.Year) + ": "
			}
			r.Figures = append(r.Figures,
				Figure{prefix + "Alles einreichen: PKV zahlt", eur(pr.ClaimAll)},
				Figure{prefix + "Nicht einreichen: Rückerstattung", eur(pr.HoldTotal)})
			var a string
			switch {
			case pr.Advantage > 0:
				a = fmt.Sprintf("Einreichen lohnt sich: %s mehr als die Beitragsrückerstattung", eur(pr.Advantage))
			case pr.Advantage < 0:
				a = fmt.Sprintf("Nicht einreichen lohnt sich: die Beitragsrückerstattung bringt %s mehr", eur(-pr.Advantage))
			default:
				a = "Beide Wege ergeben gleich viel"
			}
			if pr.Forfeited {
				a += " – aber es ist schon eine Rechnung eingereicht oder erstattet, die Rückerstattung entfällt dann in der Regel"
			}
			answers = append(answers, fmt.Sprintf("%s (Leistungsjahr %d): %s.", p.PayerName, y.Year, a))
		}
	}
	if len(answers) == 0 {
		r.Answer = fmt.Sprintf("Für %s ist keine erwartete Beitragsrückerstattung eingetragen – im Dashboard oder unter Einstellungen → Werte pro Jahr.",
			yearList(years))
		return r
	}
	r.Answer = strings.Join(answers, " ")
	return r
}

// outstanding is what the payers still owe according to SOLL (as the
// dashboard's Ausstand): per payer, with the invoices it comes from.
func outstanding(d Data, q Query, today civil.Date) Result {
	type acc struct {
		id                          string
		total                       money.Cents
		submitted, open, deviations money.Cents
		count                       int
	}
	byPayer := map[string]*acc{}
	var owing []match
	for _, m := range selection(d, q, today) {
		var owed money.Cents
		for _, c := range m.claims {
			o := c.Owed()
			if o == 0 {
				continue
			}
			a := byPayer[c.PayerID]
			if a == nil {
				a = &acc{id: c.PayerID}
				byPayer[c.PayerID] = a
			}
			a.total += o
			a.count++
			switch c.Status {
			case calc.StatusSubmitted:
				a.submitted += o
			case calc.StatusRefunded:
				a.deviations += o
			default:
				a.open += o
			}
			owed += o
		}
		if owed != 0 {
			owing = append(owing, m)
		}
	}
	r := Result{Invoices: order(owing, MeasureOwed)}
	if len(byPayer) == 0 {
		r.Answer = "Nichts – die Kostenträger schulden nach SOLL nichts mehr."
		return r
	}
	payers := make([]*acc, 0, len(byPayer))
	var sum money.Cents
	for _, a := range byPayer {
		payers = append(payers, a)
		sum += a.total
	}
	// In the order of the settings (as on the dashboard).
	slices.SortFunc(payers, func(a, b *acc) int {
		return cmp.Compare(payerIndex(d.Settings, a.id), payerIndex(d.Settings, b.id))
	})
	var parts []string
	for _, a := range payers {
		name := payerName(d.Settings, a.id)
		parts = append(parts, name+" "+eur(a.total))
		r.Groups = append(r.Groups, Group{Label: name, Count: a.count, Value: eur(a.total)})
		for _, f := range []struct {
			label string
			v     money.Cents
		}{{"eingereicht, Erstattung offen", a.submitted}, {"noch nicht eingereicht", a.open}, {"Abweichungen erstatteter Rechnungen", a.deviations}} {
			if f.v != 0 {
				r.Figures = append(r.Figures, Figure{name + ": " + f.label, eur(f.v)})
			}
		}
	}
	r.Answer = fmt.Sprintf("Noch ausstehend: %s", eur(sum))
	if len(payers) > 1 {
		r.Answer += " – " + strings.Join(parts, ", ")
	}
	return r
}

func payerIndex(s *model.Settings, id string) int {
	i := slices.IndexFunc(s.Payers, func(p model.Payer) bool { return p.ID == id })
	if i < 0 {
		return len(s.Payers)
	}
	return i
}

// yearList: "2026", "2025 und 2026".
func yearList(years []int) string {
	s := make([]string, len(years))
	for i, y := range years {
		s[i] = strconv.Itoa(y)
	}
	if len(s) == 1 {
		return s[0]
	}
	return strings.Join(s[:len(s)-1], ", ") + " und " + s[len(s)-1]
}
