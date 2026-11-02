package extract

import (
	"strings"
	"time"
	"unicode"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// Layout says which amount of a position line means what: the 1-based
// position, -1 for the last amount, 0 if the document does not show it.
// Documents of one payer always look the same, so a layout is kept per payer.
type Layout struct {
	Billed   int `json:"billed"`
	Eligible int `json:"eligible"`
	Refund   int `json:"refund"`
}

// Apply picks the amounts of a line according to the layout.
func (l Layout) Apply(amounts []money.Cents) (billed, eligible, refund money.Cents) {
	pick := func(pos int) money.Cents {
		switch {
		case pos == 0 || len(amounts) == 0:
			return 0
		case pos < 0:
			return amounts[len(amounts)-1]
		case pos <= len(amounts):
			return amounts[pos-1]
		}
		return 0
	}
	billed, eligible, refund = pick(l.Billed), pick(l.Eligible), pick(l.Refund)
	if len(amounts) == 1 { // only one amount: it is the refund
		billed, eligible = 0, 0
		refund = amounts[0]
	}
	return billed, eligible, refund
}

// GuessLayout chooses a layout from the typical number of amounts per line:
// three or more → invoice amount, eligible amount, …, refund; two → invoice
// amount and refund.
func GuessLayout(lines []RefundPosition) Layout {
	counts := map[int]int{}
	for _, l := range lines {
		counts[min(len(l.Amounts), 3)]++
	}
	switch {
	case counts[3] > 0 && counts[3] >= counts[2]:
		return Layout{Billed: 1, Eligible: 2, Refund: -1}
	default:
		return Layout{Billed: 1, Refund: -1}
	}
}

// RefundPosition is a line of a refund document that refers to one invoice.
type RefundPosition struct {
	Date     civil.Date
	Provider string
	Amounts  []money.Cents // all amounts on the line, left to right (percentages excluded)
	Text     string
}

// RefundDraft is what was recognised on a refund document.
type RefundDraft struct {
	Date            civil.Date // date of the notice / statement
	ApplicationDate civil.Date // "Ihr Antrag vom"
	PayoutDate      civil.Date
	PaidOut         money.Cents
	Deductions      []model.Deduction
	Positions       []RefundPosition
	Text            string // all text, lower case (to find the payer)
	Notes           []string
}

var (
	summaryWords   = []string{"summe", "gesamt", "auszahlung", "überweis", "zwischensumme", "übertrag", "insgesamt", "zahlbetrag"}
	deductionWords = []string{"kostendämpfung", "eigenbehalt", "selbstbehalt", "abzug", "abzüglich", "eigenanteil", "kürzung"}
	payoutLabels   = []string{"auszahlungsbetrag", "überweisungsbetrag", "zahlbetrag", "wird überwiesen", "wir überweisen", "werden überwiesen",
		"gesamterstattung", "erstattungsbetrag gesamt", "erstattung insgesamt", "summe der beihilfe", "beihilfe insgesamt",
		"gesamtbetrag der beihilfe", "auszahlung", "gesamtbetrag", "summe"}
	applicationLabels = []string{"antrag vom", "anträge vom", "ihr antrag", "ihre einreichung vom", "eingang am", "eingegangen am", "eingereicht am"}
	docDateLabels     = []string{"bescheid vom", "abrechnung vom", "leistungsabrechnung vom", "datum:", "datum", "vom"}
	payoutDateLabels  = []string{"überweisung am", "zahlung am", "überwiesen am", "valuta", "auszahlung am"}
)

func hasAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

type amountPos struct {
	v          money.Cents
	start, end int
}

// amountsAt returns the amounts of s with their positions; percentages ("50,00 %") are left out.
func amountsAt(s string) []amountPos {
	var out []amountPos
	isDigit := func(c byte) bool { return c >= '0' && c <= '9' }
	for _, loc := range amountRe.FindAllStringIndex(s, -1) {
		a, b := loc[0], loc[1]
		if a > 0 && (isDigit(s[a-1]) || s[a-1] == ',' || s[a-1] == '.') || b < len(s) && isDigit(s[b]) {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(s[b:]), "%") {
			continue
		}
		if v, err := money.Parse(s[a:b]); err == nil && v > 0 {
			out = append(out, amountPos{v, a, b})
		}
	}
	return out
}

// AnalyzeRefund reads a Beihilfebescheid or a PKV-Leistungsabrechnung.
func AnalyzeRefund(lines []Line, now time.Time) RefundDraft {
	d := RefundDraft{}
	maxYear := now.Year() + 1
	text := make([]string, len(lines))
	lower := make([]string, len(lines))
	for i, l := range lines {
		text[i] = strings.Join(strings.Fields(l.Text), " ")
		lower[i] = strings.ToLower(text[i])
	}
	d.Text = strings.Join(lower, "\n")

	firstDate := func(labels []string) civil.Date {
		for _, lab := range labels {
			for i, l := range lower {
				at := labelIndex(l, lab)
				if at < 0 {
					continue
				}
				// the date after the label on the same line, else on the next line
				after := text[i][at:]
				if ds := datesIn(after, maxYear); len(ds) > 0 {
					return ds[0]
				}
				if i+1 < len(text) {
					if ds := datesIn(text[i+1], maxYear); len(ds) > 0 {
						return ds[0]
					}
				}
			}
		}
		return civil.Date{}
	}
	d.ApplicationDate = firstDate(applicationLabels)
	d.PayoutDate = firstDate(payoutDateLabels)

	headerLine := map[int]bool{}
	for i, l := range lower {
		if hasAny(l, applicationLabels) || hasAny(l, payoutDateLabels) || strings.Contains(l, "bescheid vom") || strings.Contains(l, "abrechnung vom") {
			headerLine[i] = true
		}
	}

	// Positions, deductions and the payout.
	for i, t := range text {
		l := lower[i]
		amts := amountsAt(t)
		if len(amts) == 0 {
			continue
		}
		// A line with a date and several amounts is a position, even if
		// it says "Kürzung" (a remark on that position).
		dateLoc := dateRe.FindStringIndex(t)
		position := dateLoc != nil && dateLoc[1] <= amts[0].start && len(amts) >= 2
		switch {
		case !position && hasAny(l, deductionWords):
			label := strings.TrimSpace(strings.TrimRight(t[:amts[0].start], " :.-–"))
			if label == "" {
				label = "Abzug"
			}
			d.Deductions = append(d.Deductions, model.Deduction{Label: label, Amount: amts[len(amts)-1].v})
			continue
		case hasAny(l, summaryWords):
			continue
		case headerLine[i]:
			continue
		}
		ds := dateLoc
		if ds == nil {
			continue
		}
		dt := datesIn(t[ds[0]:ds[1]], maxYear)
		if len(dt) == 0 || ds[1] > amts[0].start {
			continue
		}
		p := RefundPosition{Date: dt[0], Text: t}
		p.Provider = cleanProvider(t[ds[1]:amts[0].start])
		for _, a := range amts {
			p.Amounts = append(p.Amounts, a.v)
		}
		d.Positions = append(d.Positions, p)
	}
	for _, lab := range payoutLabels {
		if d.PaidOut != 0 {
			break
		}
		for i, l := range lower {
			if !strings.Contains(l, lab) || hasAny(l, deductionWords) {
				continue
			}
			if a := amountsAt(text[i]); len(a) > 0 {
				d.PaidOut = a[len(a)-1].v
			} else if i+1 < len(text) {
				if a := amountsAt(text[i+1]); len(a) > 0 {
					d.PaidOut = a[0].v
				}
			}
			if d.PaidOut != 0 {
				break
			}
		}
	}

	// Document date: labelled, else the latest date in the header (before the first position).
	for _, lab := range docDateLabels {
		if !d.Date.IsZero() {
			break
		}
		for i, l := range lower {
			if i >= 15 || !strings.Contains(l, lab) || hasAny(l, applicationLabels) {
				continue
			}
			if ds := datesIn(text[i][strings.Index(l, lab):], maxYear); len(ds) > 0 {
				d.Date = ds[0]
				break
			}
		}
	}
	if d.Date.IsZero() {
		for i := 0; i < len(text) && i < 15; i++ {
			if hasAny(lower[i], applicationLabels) {
				continue
			}
			if len(amountsAt(text[i])) > 0 {
				break
			}
			for _, dt := range datesIn(text[i], maxYear) {
				if dt.After(d.Date) {
					d.Date = dt
				}
			}
		}
	}
	if len(d.Positions) == 0 {
		d.Notes = append(d.Notes, "Keine Positionen erkannt – bitte die Zeilen von Hand eintragen")
	}
	return d
}

// cleanProvider trims GOÄ numbers, separators and the like from the text between date and amounts.
func cleanProvider(s string) string {
	s = strings.TrimFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' })
	words := strings.Fields(s)
	for len(words) > 0 && strings.IndexFunc(words[len(words)-1], unicode.IsLetter) < 0 {
		words = words[:len(words)-1] // trailing numbers (counts, codes)
	}
	return strings.Join(words, " ")
}
