package check

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/extract"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/match"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// Position is one line of an invoice or a cost plan.
type Position struct {
	Date   civil.Date // zero in a cost plan
	Tooth  string
	Code   string // the number as printed: GOÄ/GOZ number ("2210", "Ä1") or a short code ("KG")
	Text   string
	Count  int // 0: not printed (once)
	Amount money.Cents
	Page   int // 1-based, 0: unknown
}

// times is how often the position was performed.
func (p Position) times() int { return max(p.Count, 1) }

// label names the position for a list: "Nr. 2210 (Zahn 36) am 06.10.2026".
func (p Position) label() string {
	s := "Nr. " + p.Code
	if p.Code == "" {
		s = short(p.Text)
	}
	if p.Tooth != "" {
		s += " (Zahn " + p.Tooth + ")"
	}
	if !p.Date.IsZero() {
		s += " am " + p.Date.DE()
	}
	return s
}

// line is the position as a detail line: label, text, count and amount.
func (p Position) line() string {
	s := p.label()
	if p.Code != "" && p.Text != "" {
		s += " – " + short(p.Text)
	}
	if p.Count > 1 {
		s += fmt.Sprintf(", %d×", p.Count)
	}
	return s + ": " + p.Amount.EUR()
}

// short cuts a text to a length that fits a list, at a space if there is one.
func short(s string) string {
	const n = 60
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	cut := string([]rune(s)[:n-1])
	if i := strings.LastIndexByte(cut, ' '); i > len(cut)/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;–-") + "…"
}

// Bill is an invoice as read from its pages.
type Bill struct {
	Source    string // file name, for the title
	Provider  string
	Payee     string
	Reference string
	IBAN      string
	Date      civil.Date
	From, To  civil.Date  // treatment period as printed (zero: none printed)
	Total     money.Cents // Rechnungsbetrag (0: not read)
	Fee       money.Cents // the fee line of a dental invoice, "Honorar" (0: not printed)
	Expenses  money.Cents // material and lab costs (Auslagen)
	Positions []Position
}

// Period is the treatment period: as printed, else from the first to the
// last dated position.
func (b *Bill) Period() (from, to civil.Date) {
	if !b.From.IsZero() {
		if b.To.Before(b.From) {
			return b.From, b.From
		}
		return b.From, b.To
	}
	for _, p := range b.Positions {
		if p.Date.IsZero() {
			continue
		}
		if from.IsZero() || p.Date.Before(from) {
			from = p.Date
		}
		if p.Date.After(to) {
			to = p.Date
		}
	}
	return from, to
}

// fee is the invoice's fee: the printed fee line, else the sum of the positions.
func (b *Bill) fee() money.Cents { return cmp.Or(b.Fee, sumOf(b.Positions)) }

// sumOf is the sum of the positions' amounts.
func sumOf(pos []Position) money.Cents {
	var s money.Cents
	for _, p := range pos {
		s += p.Amount
	}
	return s
}

func (c *checker) bill(b *Bill) Group {
	inv := c.Invoice
	provider, date := b.Provider, b.Date
	if provider == "" {
		provider = inv.Provider
	}
	if date.IsZero() {
		date = inv.InvoiceDate
	}
	g := Group{Kind: KindBill, Title: strings.TrimSpace("Rechnung " + provider), Source: b.Source, Status: OK}
	if !date.IsZero() {
		g.Title += " vom " + date.DE()
	}

	total, label := b.Total, "Rechnungsbetrag"
	if total == 0 && inv.Amount != 0 {
		total, label = inv.Amount, "Rechnungsbetrag in der App"
	}
	g.add(sums("sum", b.Positions, b.Fee, b.Expenses, total, label)...)
	positionDates(&g, b, date)
	duplicates(&g, b.Positions)
	header(&g, b, inv)
	return g
}

// positionDates flags positions outside the printed treatment period or
// after the invoice date – a wrong date on the invoice or a misread one.
func positionDates(g *Group, b *Bill, invoiceDate civil.Date) {
	var outside, late []Position
	for _, p := range b.Positions {
		switch {
		case p.Date.IsZero():
		case !b.From.IsZero() && !b.To.IsZero() && !p.Date.Between(b.From, b.To):
			outside = append(outside, p)
		case !invoiceDate.IsZero() && p.Date.After(invoiceDate):
			late = append(late, p)
		}
	}
	flag := func(ps []Position, where string) {
		if len(ps) == 0 {
			return
		}
		f := g.addf(Check, "position_date", "%s %s", count(len(ps), "Position liegt", "Positionen liegen"), where)
		f.Page = ps[0].Page
		for _, p := range ps {
			f.Detail = append(f.Detail, p.line()+pageNote(p.Page))
		}
	}
	flag(outside, "außerhalb des Behandlungszeitraums "+span(b.From, b.To))
	flag(late, "nach dem Rechnungsdatum "+invoiceDate.DE())
}

func pageNote(page int) string {
	if page == 0 {
		return ""
	}
	return fmt.Sprintf(" (Seite %d)", page)
}

// duplicates notes the same number for the same tooth twice on one day – a
// hint only, it can be right (two anaesthesias).
func duplicates(g *Group, pos []Position) {
	seen := map[string]int{}
	var order []string
	first := map[string]Position{}
	for _, p := range pos {
		if p.Date.IsZero() || p.Code == "" {
			continue
		}
		k := p.Date.String() + "|" + codeKey(p.Code) + "|" + toothKey(p.Tooth)
		if seen[k] == 0 {
			order = append(order, k)
			first[k] = p
		}
		seen[k]++
	}
	for _, k := range order {
		if n := seen[k]; n > 1 {
			p := first[k]
			f := g.addf(Info, "duplicate", "%s %d-mal berechnet", p.label(), n)
			f.Page = p.Page
		}
	}
}

// header compares the invoice's head with the invoice in the app. A
// difference means the stored data is wrong – or it is another invoice. The
// fields that agree are listed in one finding ("header"); each other one
// has its own code ("header_amount" …), for an action on that field.
func header(g *Group, b *Bill, inv *model.Invoice) {
	var same []string
	field := func(code, label string, onDoc, inApp string, equal bool) {
		switch {
		case onDoc == "":
		case inApp == "":
			g.addf(Info, code, "%s in der App fehlt – auf der Rechnung: %s", label, onDoc)
		case equal:
			same = append(same, label)
		default:
			g.addf(Check, code, "%s auf der Rechnung: %s, in der App: %s", label, onDoc, inApp)
		}
	}
	amount := func(c money.Cents) string {
		if c == 0 {
			return ""
		}
		return c.EUR()
	}
	field("header_amount", "Rechnungsbetrag", amount(b.Total), amount(inv.Amount), b.Total == inv.Amount)
	field("header_date", "Rechnungsdatum", b.Date.DE(), inv.InvoiceDate.DE(), b.Date.Equal(inv.InvoiceDate))
	field("header_reference", "Rechnungsnummer", b.Reference, inv.Reference, extract.SameText(b.Reference, inv.Reference))
	docIBAN, _ := extract.NormIBAN(b.IBAN)
	appIBAN, _ := extract.NormIBAN(inv.IBAN)
	field("header_iban", "IBAN", docIBAN, appIBAN, docIBAN == appIBAN)

	if from, to := b.Period(); !from.IsZero() {
		appFrom, appTo := inv.ServiceFrom, inv.ServiceTo
		equal := from.Equal(appFrom) && (appTo.IsZero() || to.Equal(appTo))
		inApp := ""
		if !appFrom.IsZero() {
			inApp = span(appFrom, appTo)
		}
		field("header_period", "Behandlungszeitraum", span(from, to), inApp, equal)
	}

	// Names are written in many ways: a difference is information only.
	if names := nonEmpty(b.Provider, b.Payee); len(names) > 0 && inv.Provider != "" {
		if slices.ContainsFunc(names, func(n string) bool { return sameProvider(n, inv.Provider) }) {
			same = append(same, "Behandler")
		} else {
			g.addf(Info, "header_provider", "Behandler auf der Rechnung: %s, in der App: %s", strings.Join(names, " / "), inv.Provider)
		}
	}
	if len(same) > 0 {
		g.addf(OK, "header", "Wie in der App: %s", list(same))
	}
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// sameProvider: all words of one name occur in the other ("Hans Probe" and
// "Dr. med. Hans Probe, Facharzt für Innere Medizin"), compared as names –
// without case, umlauts, titles and legal forms.
func sameProvider(a, b string) bool {
	x, y := match.NameWords(a), match.NameWords(b)
	if len(x) == 0 || len(y) == 0 {
		return false
	}
	if len(x) > len(y) {
		x, y = y, x
	}
	for _, w := range x {
		if !slices.Contains(y, w) {
			return false
		}
	}
	return true
}

// codeKey is a fee number as compared: upper case, without spaces, without
// leading zeros of a plain number ("0090" = "90").
func codeKey(s string) string {
	s = strings.ToUpper(strings.Join(strings.Fields(s), ""))
	if t := strings.TrimLeft(s, "0"); t != "" && strings.Trim(t, "0123456789") == "" {
		return t
	}
	return s
}

// toothKey is a tooth as compared: upper case, without spaces.
func toothKey(s string) string { return strings.ToUpper(strings.Join(strings.Fields(s), "")) }
