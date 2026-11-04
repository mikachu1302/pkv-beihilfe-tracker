package check

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/match"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// Remedy is a prescribed therapy with its number of sessions.
type Remedy struct {
	Name  string // "Krankengymnastik", "KG", "Fango"
	Count int    // 0: not read
}

// Prescription is a prescription for therapy (Heilmittelverordnung) as read.
type Prescription struct {
	Source     string // file name, for the title
	Prescriber string
	Date       civil.Date
	ValidUntil civil.Date // zero: not printed
	Remedies   []Remedy
}

// prescription compares the invoice of a therapist with the prescription.
// Private rules vary, so the findings are hints (Check), never a verdict.
func (c *checker) prescription(rx *Prescription) Group {
	g := Group{Kind: KindPrescription, Title: "Verordnung", Source: rx.Source, Status: OK}
	if !rx.Date.IsZero() {
		g.Title += " vom " + rx.Date.DE()
	}
	if rx.Prescriber != "" {
		g.Title += " – " + rx.Prescriber
	}
	if c.Bill == nil {
		needsBill(&g, "der Verordnung")
		return g
	}

	sessions := make([][]civil.Date, len(rx.Remedies))
	var other []string
	var otherSum money.Cents
	var first civil.Date
	for _, p := range c.Bill.Positions {
		i := bestRemedy(rx.Remedies, p)
		if i < 0 {
			other = append(other, p.line())
			otherSum += p.Amount
			continue
		}
		for range p.times() {
			sessions[i] = append(sessions[i], p.Date)
		}
		if !p.Date.IsZero() && (first.IsZero() || p.Date.Before(first)) {
			first = p.Date
		}
	}

	if len(rx.Remedies) == 0 {
		g.addf(Info, "rx_count", "Keine Heilmittel auf der Verordnung gelesen – die Anzahl lässt sich nicht prüfen")
	}
	for i, r := range rx.Remedies {
		dates := sessions[i]
		slices.SortStableFunc(dates, func(a, b civil.Date) int { return a.Compare(b) })
		n := len(dates)
		switch {
		case r.Count == 0:
			g.addf(Info, "rx_count", "%s: Anzahl auf der Verordnung nicht gelesen – %s berechnet", r.Name, count(n, "Behandlung", "Behandlungen"))
		case n == 0:
			g.addf(Info, "rx_count", "%s: %d× verordnet, aber nicht berechnet", r.Name, r.Count)
		case n > r.Count:
			f := g.addf(Check, "rx_count", "%s: %s berechnet, %d verordnet", r.Name, count(n, "Behandlung", "Behandlungen"), r.Count)
			for k := r.Count; k < n; k++ {
				if dates[k].IsZero() {
					f.Detail = append(f.Detail, fmt.Sprintf("%d. Behandlung", k+1))
				} else {
					f.Detail = append(f.Detail, fmt.Sprintf("%d. Behandlung am %s", k+1, dates[k].DE()))
				}
			}
		default:
			g.addf(OK, "rx_count", "%s: %d von %d verordneten Behandlungen berechnet", r.Name, n, r.Count)
		}
	}
	if len(other) > 0 {
		g.addf(Info, "rx_extra", "Nicht auf der Verordnung: %s, zusammen %s",
			count(len(other), "Position", "Positionen"), otherSum.EUR()).Detail = other
	}

	switch {
	case first.IsZero() || rx.Date.IsZero():
	case first.Before(rx.Date):
		g.addf(Check, "rx_start", "Erste Behandlung am %s, vor der Verordnung vom %s", first.DE(), rx.Date.DE())
	case !rx.ValidUntil.IsZero() && first.After(rx.ValidUntil):
		g.addf(Check, "rx_start", "Erste Behandlung am %s – die Verordnung galt bis %s", first.DE(), rx.ValidUntil.DE())
	case rx.ValidUntil.IsZero():
		g.addf(OK, "rx_start", "Erste Behandlung am %s, nach der Verordnung vom %s", first.DE(), rx.Date.DE())
	default:
		g.addf(OK, "rx_start", "Erste Behandlung am %s, nach der Verordnung vom %s und innerhalb ihrer Gültigkeit (bis %s)",
			first.DE(), rx.Date.DE(), rx.ValidUntil.DE())
	}
	return g
}

// remedyAbbrev expands the abbreviations of remedies that invoices and
// prescriptions print (folded words).
var remedyAbbrev = map[string][]string{
	"kg":  {"krankengymnastik"},
	"kgg": {"krankengymnastik", "geraet"},
	"mt":  {"manuelle", "therapie"},
	"mld": {"manuelle", "lymphdrainage"},
	"kmt": {"klassische", "massagetherapie"},
	"wp":  {"waermepackung"},
}

// remedyWords are the words of a remedy as compared: folded, abbreviations
// written out, without short filler words ("am", "je", "x").
func remedyWords(s string) []string {
	var out []string
	for _, w := range match.NameWords(s) {
		exp, ok := remedyAbbrev[w]
		if !ok {
			exp = []string{w}
		}
		for _, x := range exp {
			if utf8.RuneCountInString(x) >= 3 {
				out = append(out, x)
			}
		}
	}
	return out
}

// bestRemedy returns the remedy a position belongs to: every word of the
// remedy's name is a whole word of the position's number and text
// ("Fango" in "Wärmepackung (Fango)", "KG" = "Krankengymnastik"); of
// several, the one with the most words. -1 for none.
func bestRemedy(rs []Remedy, p Position) int {
	have := remedyWords(p.Code + " " + p.Text)
	best, bestN := -1, 0
	for i, r := range rs {
		want := remedyWords(r.Name)
		if len(want) <= bestN {
			continue
		}
		if !slices.ContainsFunc(want, func(w string) bool { return !slices.Contains(have, w) }) {
			best, bestN = i, len(want)
		}
	}
	return best
}
