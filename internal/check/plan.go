package check

import (
	"cmp"
	"fmt"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/match"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// Plan is a Heil- und Kostenplan or a cost estimate as read.
type Plan struct {
	Source    string // file name, for the title
	Provider  string
	Date      civil.Date
	Positions []Position
	Fee       money.Cents // Honorar (0: not printed)
	Expenses  money.Cents // material and lab costs, as estimated
	Total     money.Cents // Gesamtbetrag (0: not printed)
	OwnShare  money.Cents // Eigenanteil as printed (0: not printed)
}

// plan compares the invoice with a cost plan. The differences are
// information, without a tolerance or a verdict (decided 2026-11-03).
func (c *checker) plan(p *Plan) Group {
	g := Group{Kind: KindPlan, Title: "Heil- und Kostenplan", Source: p.Source, Status: OK}
	if !p.Date.IsZero() {
		g.Title += " vom " + p.Date.DE()
	}
	if p.Provider != "" {
		g.Title += " – " + p.Provider
	}
	g.add(sums("plan_sum", p.Positions, p.Fee, p.Expenses, p.Total, "Gesamtbetrag")...)
	if c.Bill == nil {
		needsBill(&g, "dem Kostenplan")
		return g
	}
	b := c.Bill

	if start := c.treatmentStart(); !p.Date.IsZero() && !start.IsZero() {
		if p.Date.After(start) {
			g.addf(Check, "plan_date", "Kostenplan vom %s – die Behandlung begann schon am %s", p.Date.DE(), start.DE())
		} else {
			g.addf(OK, "plan_date", "Kostenplan vom %s, vor dem Behandlungsbeginn am %s", p.Date.DE(), start.DE())
		}
	}

	planPositions(&g, b.Positions, p.Positions)

	vsPlan(&g, "plan_fee", "Honorar", b.fee(), cmp.Or(p.Fee, sumOf(p.Positions)))
	if p.Expenses != 0 || b.Expenses != 0 {
		vsPlan(&g, "plan_expenses", "Material- und Laborkosten", b.Expenses, p.Expenses)
	}
	if total := cmp.Or(b.Total, c.Invoice.Amount); p.Total != 0 && total != 0 {
		vsPlan(&g, "plan_total", "Gesamtbetrag", total, p.Total)
	}

	// What is left to pay: after the refunds, or as expected before them.
	// Each payer may round its share up, so the shares can exceed the
	// invoice by a cent – that is no own share.
	if v := c.view; v.Amount != 0 {
		var text string
		if v.Actual != 0 {
			text = fmt.Sprintf("Eigenanteil bisher %s (Rechnung %s − Erstattungen %s)",
				max(v.OwnShare, 0).EUR(), v.Amount.EUR(), v.Actual.EUR())
		} else {
			text = fmt.Sprintf("Eigenanteil voraussichtlich %s (Rechnung %s − erwartete Erstattungen %s)",
				max(v.Amount-v.Expected, 0).EUR(), v.Amount.EUR(), v.Expected.EUR())
		}
		if p.OwnShare != 0 {
			text += ", laut Kostenplan " + p.OwnShare.EUR()
		}
		g.addf(Info, "plan_own_share", "%s", text)
	}
	return g
}

// vsPlan shows an amount of the invoice next to the planned one.
func vsPlan(g *Group, code, label string, billed, planned money.Cents) {
	switch d := billed - planned; {
	case d == 0:
		g.addf(OK, code, "%s %s wie geplant", label, billed.EUR())
	case planned == 0:
		g.addf(Info, code, "%s %s – im Kostenplan nicht vorgesehen", label, billed.EUR())
	default:
		g.addf(Info, code, "%s %s – geplant %s (%s, %s)", label, billed.EUR(), planned.EUR(), signed(d), pct(d, planned))
	}
}

// planned is one number (for one tooth) of an invoice or a plan, over all days.
type planned struct {
	pos    Position // the first, with count and amount of all
	count  int
	amount money.Cents
}

func (x *planned) line() string {
	p := x.pos
	p.Date, p.Count, p.Amount = civil.Date{}, x.count, x.amount
	return p.line()
}

// byNumber sums positions by number and tooth, in the order they first appear.
func byNumber(pos []Position) ([]string, map[string]*planned) {
	var keys []string
	m := map[string]*planned{}
	for _, p := range pos {
		k := codeKey(p.Code) + "|" + toothKey(p.Tooth)
		if p.Code == "" {
			k = "|" + match.Fold(p.Text)
		}
		x := m[k]
		if x == nil {
			x = &planned{pos: p}
			m[k] = x
			keys = append(keys, k)
		}
		x.count += p.times()
		x.amount += p.Amount
	}
	return keys, m
}

// planPositions lists what the invoice has that the plan does not, the other
// way round, and the numbers billed differently than planned.
func planPositions(g *Group, billed, plan []Position) {
	bKeys, bm := byNumber(billed)
	pKeys, pm := byNumber(plan)
	same := 0
	var changed, extra, missing []string
	var extraSum, missingSum money.Cents
	for _, k := range bKeys {
		b, p := bm[k], pm[k]
		switch {
		case p == nil:
			extra = append(extra, b.line())
			extraSum += b.amount
		case b.count == p.count && b.amount == p.amount:
			same++
		default:
			changed = append(changed, fmt.Sprintf("%s: berechnet %d× %s, geplant %d× %s (%s)",
				b.pos.label0(), b.count, b.amount.EUR(), p.count, p.amount.EUR(), signed(b.amount-p.amount)))
		}
	}
	for _, k := range pKeys {
		if bm[k] == nil {
			missing = append(missing, pm[k].line())
			missingSum += pm[k].amount
		}
	}
	if same > 0 {
		g.addf(OK, "plan_positions", "%s wie im Kostenplan", count(same, "Position", "Positionen"))
	}
	if len(changed) > 0 {
		g.addf(Info, "plan_positions", "Anders als im Kostenplan: %s", count(len(changed), "Position", "Positionen")).Detail = changed
	}
	if len(extra) > 0 {
		g.addf(Info, "plan_extra", "Berechnet, aber nicht im Kostenplan: %s, zusammen %s",
			count(len(extra), "Position", "Positionen"), extraSum.EUR()).Detail = extra
	}
	if len(missing) > 0 {
		g.addf(Info, "plan_missing", "Im Kostenplan, aber nicht berechnet: %s, zusammen %s",
			count(len(missing), "Position", "Positionen"), missingSum.EUR()).Detail = missing
	}
}

// label0 is the label without the date (for positions summed over days).
func (p Position) label0() string {
	p.Date = civil.Date{}
	return p.label()
}
