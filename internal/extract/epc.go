// Package extract turns the text of a scanned or photographed document into
// a draft: it recognises whether it is an invoice or a refund document and
// pre-fills the fields it can find. Everything it returns is a suggestion the
// user confirms.
package extract

import (
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// EPC is the content of a GiroCode / EPC QR code (EPC069-12).
type EPC struct {
	Name      string // beneficiary (often a billing service, not the doctor)
	IBAN      string
	BIC       string
	Amount    money.Cents // 0 if not given
	Reference string      // structured creditor reference or unstructured remittance text
}

// ParseEPC reads a GiroCode payload. ok is false for other QR codes.
func ParseEPC(payload string) (EPC, bool) {
	lines := strings.Split(strings.ReplaceAll(payload, "\r\n", "\n"), "\n")
	if len(lines) < 7 || strings.TrimSpace(lines[0]) != "BCD" || strings.TrimSpace(lines[3]) != "SCT" {
		return EPC{}, false
	}
	at := func(i int) string {
		if i < len(lines) {
			return strings.TrimSpace(lines[i])
		}
		return ""
	}
	e := EPC{BIC: at(4), Name: at(5), IBAN: strings.ToUpper(strings.ReplaceAll(at(6), " ", ""))}
	if a := at(7); strings.HasPrefix(a, "EUR") {
		if v, err := money.ParseDecimal(strings.TrimPrefix(a, "EUR")); err == nil {
			e.Amount = v
		}
	}
	// Line 9: structured reference, line 10: unstructured text (only one of them is used).
	e.Reference = at(9)
	if e.Reference == "" {
		e.Reference = at(10)
	}
	if e.IBAN == "" {
		return EPC{}, false
	}
	return e, true
}
