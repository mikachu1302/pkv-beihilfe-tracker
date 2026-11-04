// Package check compares an invoice with the documents read for it – the
// invoice itself, a Beihilfebescheid or PKV-Leistungsabrechnung, a Heil- und
// Kostenplan, a prescription for therapy – and lists what fits and what to
// look at (the "Abgleich", docs/PLAN-v0.6.md).
//
// The documents come in already read and interpreted (internal/airead):
// dates, amounts and columns are parsed. Every sum, comparison and rule is
// code here, and the package is pure: no I/O, nothing is stored.
//
// Verdicts are careful. A sum that does not add up is Check, never Deviation
// – it may be a reading error as well as a wrong document. Differences to a
// cost plan and the counts of a prescription are information and hints, not
// verdicts. Fee rules (GOÄ/GOZ factors, thresholds) are not checked: the
// payers check them, and their cuts show up through the Bescheid.
package check

import (
	"fmt"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/calc"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// Status of a finding, in increasing severity.
type Status string

// Statuses.
const (
	OK        Status = "ok"        // ✓ passt
	Info      Status = "info"      // ℹ zur Information, no verdict
	Check     Status = "check"     // ⚠ prüfen: may be a reading error or a wrong document
	Deviation Status = "deviation" // ✗ Abweichung
)

var severity = map[Status]int{OK: 0, Info: 1, Check: 2, Deviation: 3}

// worse returns the more severe of a and b.
func worse(a, b Status) Status {
	if severity[b] > severity[a] {
		return b
	}
	return a
}

// Finding is one result of the check. Code is stable (tests, UI), Text is
// German and carries the numbers, so it can be taken over as a note.
type Finding struct {
	Status Status   `json:"status"`
	Code   string   `json:"code"`
	Text   string   `json:"text"`
	Detail []string `json:"detail,omitempty"` // further lines: positions, the notes of a Bescheid
	Page   int      `json:"page,omitempty"`   // page of the document it refers to (1-based)
}

// Kind of a document.
type Kind string

// Document kinds (the names the AI test uses for them).
const (
	KindBill         Kind = "rechnung"
	KindNotice       Kind = "bescheid"
	KindPlan         Kind = "kostenplan"
	KindPrescription Kind = "verordnung"
)

// Group holds the findings about one document.
type Group struct {
	Kind     Kind      `json:"kind"`
	Title    string    `json:"title"`
	Source   string    `json:"source,omitempty"` // file name of the document
	Status   Status    `json:"status"`           // the most severe of the findings
	Findings []Finding `json:"findings"`
}

func (g *Group) add(fs ...Finding) {
	for _, f := range fs {
		g.Findings = append(g.Findings, f)
		g.Status = worse(g.Status, f.Status)
	}
}

// addf adds a finding and returns it for details; the pointer is valid
// until the next finding is added.
func (g *Group) addf(st Status, code, format string, args ...any) *Finding {
	g.add(Finding{Status: st, Code: code, Text: fmt.Sprintf(format, args...)})
	return &g.Findings[len(g.Findings)-1]
}

// Result is the outcome of a check: one group per document.
type Result struct {
	Status Status  `json:"status"` // the most severe of all findings
	Groups []Group `json:"groups"`
}

// Input is an invoice of the app with the documents read for it. Settings
// and Invoice are required; any of the documents may be missing.
type Input struct {
	Settings *model.Settings
	Invoice  *model.Invoice

	Bill          *Bill // the invoice document itself
	Notices       []Notice
	Plans         []Plan
	Prescriptions []Prescription
}

// Run checks the invoice against the documents.
func Run(in Input) Result {
	// Deadlines and waiting times are not used, so no "today" is needed.
	view := calc.Invoice(in.Settings, in.Invoice, civil.Date{})
	c := checker{Input: in, view: view}
	var groups []Group
	if in.Bill != nil {
		groups = append(groups, c.bill(in.Bill))
	}
	for i := range in.Notices {
		groups = append(groups, c.notice(&in.Notices[i]))
	}
	for i := range in.Plans {
		groups = append(groups, c.plan(&in.Plans[i]))
	}
	for i := range in.Prescriptions {
		groups = append(groups, c.prescription(&in.Prescriptions[i]))
	}
	r := Result{Status: OK, Groups: groups}
	if r.Groups == nil {
		r.Groups = []Group{} // JSON: [] rather than null
	}
	for _, g := range groups {
		r.Status = worse(r.Status, g.Status)
	}
	return r
}

type checker struct {
	Input
	view calc.InvoiceView
}

// claim returns the derived values of the invoice towards a payer.
func (c *checker) claim(payerID string) (calc.ClaimView, bool) {
	for _, cv := range c.view.Claims {
		if cv.PayerID == payerID {
			return cv, true
		}
	}
	return calc.ClaimView{}, false
}

// treatmentStart is the first day of treatment: the first dated position of
// the invoice document, else its printed period, else the app's date.
func (c *checker) treatmentStart() civil.Date {
	if c.Bill != nil {
		if from, _ := c.Bill.Period(); !from.IsZero() {
			return from
		}
	}
	return c.Invoice.ServiceDate()
}

// needsBill is the finding for a comparison with the invoice's positions
// when the invoice document was not read.
func needsBill(g *Group, what string) {
	g.addf(Info, "needs_bill", "Für den Vergleich mit %s wird die Rechnung selbst gebraucht – bitte die Rechnung mit auswählen", what)
}

// --- formatting ----------------------------------------------------------------------------

// signed is a difference with its sign: "+99,51 €", "-5,25 €".
func signed(c money.Cents) string {
	if c > 0 {
		return "+" + c.EUR()
	}
	return c.EUR()
}

// pct is diff relative to base with one decimal and its sign: "+29,6 %".
func pct(diff, base money.Cents) string {
	bp, sign := int64(money.RateOf(diff, base)), "+"
	if bp < 0 {
		bp, sign = -bp, "-"
	}
	tenths := (bp + 5) / 10 // basis points → tenths of a per cent, rounded half up
	return fmt.Sprintf("%s%d,%d %%", sign, tenths/10, tenths%10)
}

// rateDE is a rate as written in German: "50 %", "30,5 %".
func rateDE(r money.Rate) string { return strings.ReplaceAll(r.String(), ".", ",") + " %" }

// span is a period: "08.09.2026–29.09.2026", or one day.
func span(from, to civil.Date) string {
	if to.IsZero() || from.Equal(to) {
		return from.DE()
	}
	return from.DE() + "–" + to.DE()
}

// list joins items as German prose: "a, b und c".
func list(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " und " + items[len(items)-1]
}

// count is a number with a noun: "1 Position", "3 Positionen".
func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// --- sums ----------------------------------------------------------------------------------

// sums checks that the positions add up: to the fee when a fee line is
// printed (dental invoices and plans) and the fee plus material and lab
// costs to the total; otherwise the positions plus those costs to the total.
//
// A position's amount may be rounded by up to half a cent, so a total
// computed before rounding may differ from their sum by half a cent per
// position – within that the sum fits ("Rundung").
func sums(code string, pos []Position, fee, expenses, total money.Cents, totalLabel string) []Finding {
	if len(pos) == 0 {
		return []Finding{{Status: Info, Code: code, Text: "Keine Positionen gelesen – die Summe lässt sich nicht prüfen"}}
	}
	sum := sumOf(pos)
	rounding := money.Cents((len(pos) + 1) / 2)
	const missing = "fehlt eine Position, oder ist ein Betrag falsch gelesen?"
	if fee != 0 {
		out := []Finding{compare(code, "Summe der Positionen", sum, "Honorar", fee, rounding, missing)}
		if total != 0 {
			label := "Honorar + Material- und Laborkosten"
			if expenses == 0 {
				label = "Honorar"
			}
			out = append(out, compare(code, label, fee+expenses, totalLabel, total, 0, "ist ein Betrag falsch gelesen?"))
		}
		return out
	}
	if total == 0 {
		return []Finding{{Status: Info, Code: code, Text: "Kein Gesamtbetrag gelesen – die Summe lässt sich nicht prüfen"}}
	}
	label := "Summe der Positionen"
	if expenses != 0 {
		label = "Positionen + Material- und Laborkosten"
	}
	return []Finding{compare(code, label, sum+expenses, totalLabel, total, rounding, missing)}
}

// compare is the finding for two amounts that should be equal (within tolerance).
func compare(code, labelA string, a money.Cents, labelB string, b, tolerance money.Cents, hint string) Finding {
	d := a - b
	switch {
	case d == 0:
		return Finding{Status: OK, Code: code, Text: fmt.Sprintf("%s %s = %s %s", labelA, a.EUR(), labelB, b.EUR())}
	case d.Abs() <= tolerance:
		return Finding{Status: OK, Code: code, Text: fmt.Sprintf("%s %s ≈ %s %s (Rundung)", labelA, a.EUR(), labelB, b.EUR())}
	}
	return Finding{Status: Check, Code: code,
		Text: fmt.Sprintf("%s %s, %s %s (Differenz %s) – %s", labelA, a.EUR(), labelB, b.EUR(), signed(d), hint)}
}
