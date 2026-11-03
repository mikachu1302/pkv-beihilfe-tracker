package extract

import (
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// Evidence is what the recognised text of a document contains – to check
// values read another way (by the AI): an amount or a number that is in the
// text was read twice. (Dates are not checked this way: a document has many.)
type Evidence struct {
	amounts map[money.Cents]bool
	words   string // the text as normWord: letters and digits, upper case
}

// NewEvidence collects the amounts and the text of the lines.
func NewEvidence(lines []Line) Evidence {
	e := Evidence{amounts: map[money.Cents]bool{}}
	var words strings.Builder
	for _, l := range lines {
		for _, a := range amounts(l.Text) {
			e.amounts[a] = true
		}
		words.WriteString(normWord(l.Text))
		words.WriteByte(' ') // no match across lines
	}
	e.words = words.String()
	return e
}

// HasAmount: the amount is in the text (German format, "1.234,56").
func (e Evidence) HasAmount(c money.Cents) bool { return e.amounts[c] }

// HasText: s is in the text, ignoring case, spaces and punctuation (an IBAN
// printed in groups of four, "Re.-Nr. 2026/0815"). Short values (fewer than
// four letters or digits) say too little and never match.
func (e Evidence) HasText(s string) bool {
	w := normWord(s)
	return len(w) >= 4 && strings.Contains(e.words, w)
}

// ValidIBAN checks the ISO 13616 checksum of an IBAN (spaces allowed).
func ValidIBAN(iban string) bool {
	return validIBAN(strings.ToUpper(strings.ReplaceAll(iban, " ", "")))
}
