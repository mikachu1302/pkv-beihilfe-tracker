package check

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/match"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// NoticeLine is one line of a Bescheid: an invoice as the payer settled it.
type NoticeLine struct {
	Date     civil.Date // invoice (or treatment) date as printed
	Provider string
	Billed   money.Cents // Rechnungsbetrag (0: not printed)
	Eligible money.Cents // erstattungsfähig / beihilfefähig (0: not printed)
	Rate     money.Rate  // Bemessungssatz (0: not printed)
	Refund   money.Cents
	Notes    []string // numbers of the notes the line refers to ("1", "A3")
	Page     int
}

// label names line i (0-based) for the user: "Zeile 1 (12.10.2026, Dr. Probe)".
func (l NoticeLine) label(i int) string {
	s := fmt.Sprintf("Zeile %d", i+1)
	if parts := nonEmpty(l.Date.DE(), l.Provider); len(parts) > 0 {
		s += " (" + strings.Join(parts, ", ") + ")"
	}
	return s
}

// Note is an explanation printed on a Bescheid, referred to by its number.
type Note struct {
	Nr   string
	Text string
}

// Notice is a Beihilfebescheid or a PKV-Leistungsabrechnung as read.
type Notice struct {
	Source string // file name, for the title
	// PayerID is the payer in the app ("" = not recognised); the caller
	// recognises it from the printed name, as the inbox does.
	PayerID         string
	Payer           string // the payer's name as printed
	Date            civil.Date
	ApplicationDate civil.Date
	Lines           []NoticeLine
	Notes           []Note
	Deductions      []model.Deduction
	PaidOut         money.Cents // Auszahlung (0: not read)
}

func (c *checker) notice(n *Notice) Group {
	payer, known := c.Settings.Payer(n.PayerID)
	g := Group{Kind: KindNotice, Title: noticeTitle(n, payer, known), Source: n.Source, Status: OK}
	if !known {
		name := ""
		if n.Payer != "" {
			name = " („" + n.Payer + "“)"
		}
		g.addf(Check, "notice_payer", "Kostenträger des Bescheids nicht erkannt%s – bitte zuordnen", name)
	} else {
		c.noticeInvoice(&g, n, payer)
	}
	noticeTotals(&g, n) // the document as a whole comes last
	return g
}

// noticeInvoice compares the invoice's line(s) of the Bescheid with the invoice.
func (c *checker) noticeInvoice(g *Group, n *Notice, payer model.Payer) {
	cv, ok := c.claim(payer.ID)
	if !ok {
		g.addf(Check, "notice_payer", "Laut App hat %s an dieser Rechnung keinen Anteil", payer.Name)
		return
	}
	idx := c.linesFor(n, payer.ID)
	if len(idx) == 0 {
		f := g.addf(Check, "notice_line", "Keine Zeile des Bescheids passt zu dieser Rechnung (%s)",
			strings.Join(nonEmpty(c.Invoice.InvoiceDate.DE(), c.Invoice.Amount.EUR()), ", "))
		for i, l := range n.Lines {
			f.Detail = append(f.Detail, l.label(i)+": "+l.Billed.EUR())
		}
		return
	}

	var billed, eligible, refund money.Cents
	billedKnown, eligibleKnown := true, true
	var notes []string
	rates := map[money.Rate]bool{}
	for _, i := range idx {
		l := n.Lines[i]
		billed += l.Billed
		billedKnown = billedKnown && l.Billed != 0
		eligible += l.Eligible
		eligibleKnown = eligibleKnown && l.Eligible != 0
		refund += l.Refund
		if l.Rate != 0 {
			rates[l.Rate] = true
		}
		for _, nr := range l.Notes {
			if !slices.Contains(notes, nr) {
				notes = append(notes, nr)
			}
		}
	}
	first := n.Lines[idx[0]]
	which := first.label(idx[0])
	if len(idx) > 1 {
		nums := make([]string, len(idx))
		for k, i := range idx {
			nums[k] = strconv.Itoa(i + 1)
		}
		which = "Zeilen " + list(nums) + " zusammen"
	}
	switch {
	case !billedKnown:
		g.addf(OK, "notice_line", "%s gehört zu dieser Rechnung", which).Page = first.Page
	case billed == c.Invoice.Amount:
		g.addf(OK, "notice_line", "%s: Rechnungsbetrag %s wie in der App", which, billed.EUR()).Page = first.Page
	default:
		g.addf(Check, "notice_line", "%s: Rechnungsbetrag im Bescheid %s, in der App %s – die richtige Zeile, oder ein Betrag falsch gelesen?",
			which, billed.EUR(), c.Invoice.Amount.EUR()).Page = first.Page
	}
	eligibleWord := "erstattungsfähig"
	if payer.Kind == model.Beihilfe {
		eligibleWord = "beihilfefähig"
	}
	for _, i := range idx {
		lineValues(g, n.Lines[i], i, eligibleWord)
	}

	// The refund against SOLL, with what the Bescheid says about it.
	var detail []string
	if eligibleKnown && billedKnown && eligible < billed {
		detail = append(detail, fmt.Sprintf("Nicht %s: %s (%s %s von %s)",
			eligibleWord, (billed-eligible).EUR(), eligibleWord, eligible.EUR(), billed.EUR()))
	}
	var rate money.Rate
	if len(rates) == 1 {
		for r := range rates {
			rate = r
		}
	}
	if rate != 0 && rate != cv.Rate {
		detail = append(detail, fmt.Sprintf("Satz im Bescheid %s, in der App %s", rateDE(rate), rateDE(cv.Rate)))
	}
	for _, nr := range notes {
		detail = append(detail, noteText(n, nr))
	}
	how := fmt.Sprintf("%s von %s", rateDE(cv.Rate), cv.Eligible.EUR())
	if c.Invoice.Claims[payer.ID].Expected != nil {
		how = "SOLL in der App"
	}
	var f *Finding
	switch diff := refund - cv.Expected; {
	case diff.Abs() <= 1: // a payer may round its share by a cent
		f = g.addf(OK, "refund", "Erstattung %s wie erwartet (%s)", refund.EUR(), how)
	case diff.Abs() <= c.Settings.Tolerance():
		f = g.addf(Info, "refund", "Erstattung %s statt erwartet %s (%s) – Differenz %s, unter der Toleranz von %s",
			refund.EUR(), cv.Expected.EUR(), how, signed(diff), c.Settings.Tolerance().EUR())
	default:
		f = g.addf(Deviation, "refund", "Erstattung %s statt erwartet %s (%s) – Differenz %s",
			refund.EUR(), cv.Expected.EUR(), how, signed(diff))
	}
	f.Detail, f.Page = detail, first.Page

	if rate != 0 && rate != cv.Rate {
		g.addf(Check, "notice_rate", "Bemessungssatz im Bescheid %s, in der App %s – stimmt der Versicherungsschutz in den Einstellungen?",
			rateDE(rate), rateDE(cv.Rate))
	}
	switch {
	case cv.Actual == 0:
		g.addf(Info, "refund_recorded", "In der App ist noch keine Erstattung von %s erfasst", payer.Name)
	case cv.Actual != refund:
		g.addf(Check, "refund_recorded", "In der App sind %s als Erstattung von %s erfasst, im Bescheid stehen %s",
			cv.Actual.EUR(), payer.Name, refund.EUR())
	}
}

func noticeTitle(n *Notice, payer model.Payer, known bool) string {
	kind, name := "Bescheid", n.Payer
	if known {
		name = payer.Name
		switch payer.Kind {
		case model.Beihilfe:
			kind = "Beihilfebescheid"
		case model.PKV:
			kind = "Leistungsabrechnung"
		}
	}
	if !n.Date.IsZero() {
		kind += " vom " + n.Date.DE()
	}
	if name != "" {
		kind += " – " + name
	}
	return kind
}

// linesFor returns the lines of the Bescheid that belong to the invoice, as
// the matcher of the inbox assigns them (one line, or several that add up to
// the invoice).
func (c *checker) linesFor(n *Notice, payerID string) []int {
	lines := make([]match.Line, len(n.Lines))
	for i, l := range n.Lines {
		lines[i] = match.Line{ID: strconv.Itoa(i), Date: l.Date, Provider: l.Provider, Billed: l.Billed, Refund: l.Refund}
	}
	// The invoice is the only candidate, and as it was before the Bescheid
	// was booked: a refund already recorded must not count against it.
	inv := *c.Invoice
	inv.Claims = maps.Clone(inv.Claims)
	if inv.Claims == nil {
		inv.Claims = map[string]model.Claim{}
	}
	cl := inv.Claims[payerID]
	cl.Refunds, cl.Refunded, cl.Skip = nil, model.Mark{}, false
	inv.Claims[payerID] = cl
	var out []int
	for i, r := range match.Match(lines, []model.Invoice{inv}, c.Settings, match.Options{PayerID: payerID}) {
		if slices.Contains(r.InvoiceIDs, inv.ID) {
			out = append(out, i)
		}
	}
	return out
}

// lineValues checks a line in itself: eligible at most the billed amount,
// the refund the rate of the eligible amount. A difference is most likely a
// misread number.
func lineValues(g *Group, l NoticeLine, i int, eligibleWord string) {
	if l.Eligible != 0 && l.Billed != 0 && l.Eligible > l.Billed {
		g.addf(Check, "notice_values", "%s: %s %s ist mehr als der Rechnungsbetrag %s – falsch gelesen?",
			l.label(i), eligibleWord, l.Eligible.EUR(), l.Billed.EUR()).Page = l.Page
	}
	if l.Rate != 0 && l.Eligible != 0 {
		if want := l.Eligible.MulRate(l.Rate); (l.Refund - want).Abs() > 1 {
			g.addf(Check, "notice_values", "%s: Erstattung %s ist nicht %s von %s (%s) – falsch gelesen?",
				l.label(i), l.Refund.EUR(), rateDE(l.Rate), l.Eligible.EUR(), want.EUR()).Page = l.Page
		}
	}
}

// noteText is a note of the Bescheid for a detail line.
func noteText(n *Notice, nr string) string {
	for _, x := range n.Notes {
		if strings.EqualFold(strings.TrimSpace(x.Nr), strings.TrimSpace(nr)) && x.Text != "" {
			return "Hinweis " + nr + ": " + x.Text
		}
	}
	return "Hinweis " + nr + " (Text nicht gelesen)"
}

// noticeTotals checks the Bescheid as a whole: the refunds of all lines minus
// the deductions are the payout.
func noticeTotals(g *Group, n *Notice) {
	var sum, cut money.Cents
	for _, l := range n.Lines {
		sum += l.Refund
	}
	var cuts []string
	for _, d := range n.Deductions {
		cut += d.Amount
		cuts = append(cuts, strings.TrimSpace(d.Label+" "+d.Amount.EUR()))
	}
	if len(cuts) > 0 {
		g.addf(Info, "notice_deduction", "Vom Bescheid abgezogen: %s – betrifft die Auszahlung, nicht die einzelne Rechnung", list(cuts))
	}
	if n.PaidOut == 0 {
		g.addf(Info, "notice_sum", "Auszahlungsbetrag nicht gelesen – die Summe lässt sich nicht prüfen")
		return
	}
	left := "Erstattungen " + sum.EUR()
	if cut != 0 {
		left += " − Abzüge " + cut.EUR()
	}
	if d := sum - cut - n.PaidOut; d != 0 {
		g.addf(Check, "notice_sum", "%s, Auszahlung %s (Differenz %s) – fehlt eine Zeile, oder ist ein Betrag falsch gelesen?",
			left, n.PaidOut.EUR(), signed(d))
		return
	}
	g.addf(OK, "notice_sum", "%s = Auszahlung %s", left, n.PaidOut.EUR())
}
