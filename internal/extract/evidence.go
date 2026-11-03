package extract

import (
	"regexp"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// Evidence is what the recognised text of a document contains – to check
// values read another way (by the AI): an amount or a number that is in the
// text was read twice. (Dates are not checked this way: a document has many.)
type Evidence struct {
	amounts map[money.Cents]bool
	tokens  [][]string // per line, the whitespace-separated pieces as normWord makes them
}

// NewEvidence collects the amounts and the text of the lines.
func NewEvidence(lines []Line) Evidence {
	e := Evidence{amounts: map[money.Cents]bool{}}
	for _, l := range lines {
		for _, a := range amounts(l.Text) {
			e.amounts[a] = true
		}
		var line []string
		for _, f := range strings.Fields(l.Text) {
			if w := normWord(f); w != "" {
				line = append(line, w)
			}
		}
		e.tokens = append(e.tokens, line)
	}
	return e
}

// HasAmount: the amount is in the text (German format, "1.234,56").
func (e Evidence) HasAmount(c money.Cents) bool { return e.amounts[c] }

// HasText: s is in the text as whole pieces – ignoring case and punctuation,
// and across spaces within a line (an IBAN printed in groups of four,
// "Re.-Nr. 2026 / 0815"). A part of a piece does not count ("0815" is not
// in "2026-0815"), and values of fewer than five letters and digits or
// without a digit say too little and never match.
func (e Evidence) HasText(s string) bool {
	w := normWord(s)
	if len(w) < 5 || !strings.ContainsAny(w, "0123456789") {
		return false
	}
	for _, line := range e.tokens {
		for i := range line {
			joined := ""
			for _, t := range line[i:] {
				joined += t
				if joined == w {
					return true
				}
				if len(joined) >= len(w) {
					break
				}
			}
		}
	}
	return false
}

var ibanShape = regexp.MustCompile(`^[A-Z]{2}\d{2}[A-Z0-9]{11,30}$`)

// NormIBAN tidies an IBAN (upper case, no spaces) and checks its form and
// ISO 13616 checksum; ok is false for anything else.
func NormIBAN(s string) (iban string, ok bool) {
	iban = strings.ToUpper(strings.Join(strings.Fields(s), ""))
	return iban, ibanShape.MatchString(iban) && validIBAN(iban)
}

// SameText compares two values as names and numbers are compared: letters
// and digits only, without case ("R-4711" = "r 4711").
func SameText(a, b string) bool { return normWord(a) == normWord(b) }
