// Package calc derives SOLL/IST values, statuses and yearly figures from the
// records. It is pure: no I/O, nothing is stored, everything is recomputed.
package calc

import (
	"fmt"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// DeadlineWarnDays: open claims are flagged this many days before their deadline.
const DeadlineWarnDays = 90

// LongWaitDays: a submitted claim without refund after this many days is
// waiting long (a reminder on the dashboard, a hint in the list).
const LongWaitDays = 42

// Status of a claim.
type Status string

// Claim statuses, in order of precedence (skipped wins over refunded …).
const (
	StatusSkipped   Status = "skipped"   // wird nicht eingereicht
	StatusRefunded  Status = "refunded"  // erstattet
	StatusSubmitted Status = "submitted" // eingereicht, Erstattung offen
	StatusHeld      Status = "held"      // zurückgehalten
	StatusOpen      Status = "open"      // noch nicht eingereicht
)

// Warning is a plausibility finding; Code is stable, Text is for display.
type Warning struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// ClaimView is one payer's side of an invoice with all derived values.
type ClaimView struct {
	PayerID   string          `json:"payerId"`
	PayerName string          `json:"payerName"`
	Kind      model.PayerKind `json:"kind"`
	Rate      money.Rate      `json:"rate"`
	Eligible  money.Cents     `json:"eligible"`
	Expected  money.Cents     `json:"expected"` // SOLL
	Actual    money.Cents     `json:"actual"`   // IST
	Diff      money.Cents     `json:"diff"`     // IST − SOLL, once refunded
	Status    Status          `json:"status"`

	Submitted model.Mark `json:"submitted"`
	Refunded  model.Mark `json:"refunded"`
	Clarify   model.Mark `json:"clarify"`

	// SuggestClarify: refunded with a deviation above the tolerance and not yet marked.
	SuggestClarify     bool `json:"suggestClarify"`
	KeepsPremiumRefund bool `json:"keepsPremiumRefund"`

	// Deadline to file the claim (only for open/held claims of payers with a deadline).
	Deadline     civil.Date `json:"deadline"`
	DeadlineDays int        `json:"deadlineDays"` // days left, negative when missed

	// WaitingDays: days since the claim was submitted, while it waits for the refund.
	WaitingDays int `json:"waitingDays"`
	// FromDoc: a refund was taken from a refund document (Bescheid).
	FromDoc bool `json:"fromDoc"`
}

// InvoiceView is an invoice with the derived values of all its claims.
type InvoiceView struct {
	ID          string      `json:"id"`
	Year        int         `json:"year"` // Leistungsjahr
	ServiceDate civil.Date  `json:"serviceDate"`
	Amount      money.Cents `json:"amount"`
	Claims      []ClaimView `json:"claims"`
	Expected    money.Cents `json:"expected"`
	Actual      money.Cents `json:"actual"`
	OwnShare    money.Cents `json:"ownShare"` // amount − IST
	Settled     bool        `json:"settled"`  // every claim refunded or skipped
	Warnings    []Warning   `json:"warnings,omitempty"`
}

// Invoice derives all values of inv under the settings s.
func Invoice(s *model.Settings, inv *model.Invoice, today civil.Date) InvoiceView {
	v := InvoiceView{ID: inv.ID, Year: inv.ServiceYear(), ServiceDate: inv.ServiceDate(), Amount: inv.Amount}
	settled := true
	for _, id := range inv.PayerIDs(s) {
		cv := claimView(s, inv, id, today)
		v.Claims = append(v.Claims, cv)
		v.Expected += cv.Expected
		v.Actual += cv.Actual
		if cv.Status != StatusRefunded && cv.Status != StatusSkipped {
			settled = false
		}
	}
	v.OwnShare = inv.Amount - v.Actual
	v.Settled = settled && len(v.Claims) > 0
	v.Warnings = invoiceWarnings(inv, &v)
	return v
}

func claimView(s *model.Settings, inv *model.Invoice, payerID string, today civil.Date) ClaimView {
	p, ok := s.Payer(payerID)
	if !ok {
		p = model.Payer{ID: payerID, Name: payerID}
	}
	c := inv.Claims[payerID]
	cv := ClaimView{
		PayerID: p.ID, PayerName: p.Name, Kind: p.Kind,
		Rate: s.ShareOf(payerID, inv.ServiceDate()), Eligible: inv.Amount,
		Submitted: c.Submitted, Refunded: c.Refunded, Clarify: c.Clarify,
		KeepsPremiumRefund: c.KeepsPremiumRefund,
	}
	if c.Rate != nil {
		cv.Rate = *c.Rate
	}
	if c.Eligible != nil {
		cv.Eligible = *c.Eligible
	}
	cv.Expected = cv.Eligible.MulRate(cv.Rate)
	if c.Expected != nil {
		cv.Expected = *c.Expected
	}
	for _, r := range c.Refunds {
		cv.Actual += r.Amount
		cv.FromDoc = cv.FromDoc || r.DocID != ""
	}
	cv.Status = ClaimStatus(c)
	if cv.Status == StatusRefunded {
		cv.Diff = cv.Actual - cv.Expected
		cv.SuggestClarify = !c.Clarify.Done() && cv.Diff.Abs() > s.Tolerance()
	}
	if p.ClaimDeadlineYears > 0 && !inv.InvoiceDate.IsZero() && (cv.Status == StatusOpen || cv.Status == StatusHeld) {
		cv.Deadline = inv.InvoiceDate.AddYears(p.ClaimDeadlineYears)
		cv.DeadlineDays = today.DaysUntil(cv.Deadline)
	}
	if cv.Status == StatusSubmitted && !c.Submitted.Date.IsZero() {
		cv.WaitingDays = -today.DaysUntil(c.Submitted.Date)
	}
	return cv
}

// ClaimStatus returns the status of a claim.
func ClaimStatus(c model.Claim) Status {
	switch {
	case c.Skip:
		return StatusSkipped
	case c.Refunded.Done() || len(c.Refunds) > 0:
		return StatusRefunded
	case c.Submitted.Done():
		return StatusSubmitted
	case c.Held:
		return StatusHeld
	default:
		return StatusOpen
	}
}

// plausible dates lie between one year before and four years after the invoice date.
func implausible(d, invoiceDate civil.Date) bool {
	return !d.IsZero() && !invoiceDate.IsZero() &&
		(d.Before(invoiceDate.AddYears(-1)) || d.After(invoiceDate.AddYears(4)))
}

func invoiceWarnings(inv *model.Invoice, v *InvoiceView) []Warning {
	var w []Warning
	add := func(code, format string, args ...any) {
		w = append(w, Warning{Code: code, Text: fmt.Sprintf(format, args...)})
	}
	if inv.Amount <= 0 {
		add("amount_missing", "Rechnungsbetrag fehlt")
	}
	if inv.InvoiceDate.IsZero() {
		add("invoice_date_missing", "Rechnungsdatum fehlt")
	}
	if !inv.InvoiceDate.IsZero() && inv.ServiceFrom.After(inv.InvoiceDate) {
		add("service_after_invoice", "Behandlung am %s liegt nach dem Rechnungsdatum %s",
			inv.ServiceFrom.DE(), inv.InvoiceDate.DE())
	}
	// Each payer may round its share up by a cent (150,69 + 150,69 for 301,37).
	var rounding money.Cents
	for _, cv := range v.Claims {
		if cv.Actual != 0 {
			rounding++
		}
	}
	if inv.Amount > 0 && v.Actual > inv.Amount+rounding {
		add("refund_exceeds_amount", "Erstattungen (%s €) sind höher als der Rechnungsbetrag (%s €)",
			v.Actual.DE(), inv.Amount.DE())
	}
	if implausible(inv.Paid.Date, inv.InvoiceDate) {
		add("date_implausible", "Bezahlt am %s – Tippfehler?", inv.Paid.Date.DE())
	}
	for _, cv := range v.Claims {
		for _, m := range []struct {
			label string
			date  civil.Date
		}{{"eingereicht", cv.Submitted.Date}, {"erstattet", cv.Refunded.Date}} {
			switch {
			case implausible(m.date, inv.InvoiceDate):
				add("date_implausible", "%s: %s am %s – Tippfehler?", cv.PayerName, m.label, m.date.DE())
			case !m.date.IsZero() && m.date.Before(inv.InvoiceDate):
				add("date_before_invoice", "%s: %s am %s, vor dem Rechnungsdatum %s",
					cv.PayerName, m.label, m.date.DE(), inv.InvoiceDate.DE())
			}
		}
		if cv.Submitted.Done() && cv.Refunded.Done() && cv.Refunded.Date.Before(cv.Submitted.Date) {
			add("refund_before_submission", "%s: erstattet am %s, vor der Einreichung am %s",
				cv.PayerName, cv.Refunded.Date.DE(), cv.Submitted.Date.DE())
		}
	}
	return w
}
