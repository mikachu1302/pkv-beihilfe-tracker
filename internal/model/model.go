// Package model defines the records kept in the data folder.
//
// Glossary (code → German domain term):
//
//	Payer          Kostenträger (Beihilfestelle or PKV)
//	CoveragePeriod Versicherungsschutz: which payer covers which share from a date on
//	Invoice        Rechnung
//	Claim          Anspruch einer Rechnung gegenüber einem Kostenträger
//	Mark           Häkchen mit Datum (eingereicht, erstattet, Klärungsbedarf, bezahlt)
//	Refund         Erstattung (IST)
//	RefundDoc      Erstattungsbeleg (Beihilfebescheid, PKV-Leistungsabrechnung)
//	Submission     Einreichung
//	Allowance      Leistung ohne Rechnung (z. B. Krankenhaustagegeld)
//	ServiceYear    Leistungsjahr
//	PremiumRefund  Beitragsrückerstattung (BRE)
package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// PayerKind distinguishes Beihilfe from private health insurance.
type PayerKind string

// Payer kinds.
const (
	Beihilfe PayerKind = "beihilfe"
	PKV      PayerKind = "pkv"
)

// DefaultClarifyTolerance is used when Settings.ClarifyTolerance is not set.
const DefaultClarifyTolerance money.Cents = 100

// Payer is a Beihilfestelle or a private health insurer. Rules differ per
// Land and insurer, so everything is configurable and nothing is hard-coded.
type Payer struct {
	ID   string    `json:"id"`
	Kind PayerKind `json:"kind"`
	Name string    `json:"name"`
	// ClaimDeadlineYears: claims must be filed within this many years after
	// the invoice date (Bund: 3, § 54 BBhV). 0 = no deadline.
	ClaimDeadlineYears int `json:"claimDeadlineYears,omitempty"`
	// MinClaimAmount: a claim must exceed this sum (Bund: 200 €, § 51 Abs. 8 BBhV). 0 = none.
	MinClaimAmount money.Cents `json:"minClaimAmount,omitzero"`
	Note           string      `json:"note,omitempty"`
}

// Share is the part of the costs one payer covers.
type Share struct {
	PayerID string     `json:"payerId"`
	Rate    money.Rate `json:"rate"`
}

// CoveragePeriod applies from From until the next period starts. The share
// that counts is the one at the date of treatment (§ 46 Abs. 1 BBhV).
type CoveragePeriod struct {
	From   civil.Date `json:"from"`
	Shares []Share    `json:"shares"`
}

// YearSetting holds per-year values of one payer.
type YearSetting struct {
	Year    int    `json:"year"`
	PayerID string `json:"payerId"`
	// Deductible is the yearly Selbstbehalt of a PKV tariff.
	Deductible money.Cents `json:"deductible,omitzero"`
	// AnnualDeduction is e.g. a Kostendämpfungspauschale of a Beihilfestelle.
	AnnualDeduction money.Cents `json:"annualDeduction,omitzero"`
	// ExpectedPremiumRefund is the BRE the PKV pays if nothing is claimed for the year.
	ExpectedPremiumRefund money.Cents `json:"expectedPremiumRefund,omitzero"`
}

// Alias maps a name as it appears on refund documents to the provider name used on invoices.
type Alias struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// RefundLayout is the remembered layout of one payer's refund documents:
// which amount of a position line is the invoice amount, the eligible amount
// and the refund (1-based position, -1 = last, 0 = not shown).
type RefundLayout struct {
	PayerID  string `json:"payerId"`
	Billed   int    `json:"billed"`
	Eligible int    `json:"eligible"`
	Refund   int    `json:"refund"`
}

// Settings are shared by all devices (stored in the data folder).
type Settings struct {
	Payers           []Payer          `json:"payers"`
	Coverage         []CoveragePeriod `json:"coverage"`
	Years            []YearSetting    `json:"years,omitempty"`
	Aliases          []Alias          `json:"aliases,omitempty"`
	RefundLayouts    []RefundLayout   `json:"refundLayouts,omitempty"`
	ClarifyTolerance money.Cents      `json:"clarifyTolerance,omitzero"`
}

// RefundLayoutFor returns the remembered layout of a payer.
func (s *Settings) RefundLayoutFor(payerID string) (RefundLayout, bool) {
	for _, l := range s.RefundLayouts {
		if l.PayerID == payerID {
			return l, true
		}
	}
	return RefundLayout{}, false
}

// Payer returns the payer with the given ID.
func (s *Settings) Payer(id string) (Payer, bool) {
	for _, p := range s.Payers {
		if p.ID == id {
			return p, true
		}
	}
	return Payer{}, false
}

// SharesAt returns the shares of the coverage period that applies on d.
// For the zero date the latest period is used.
func (s *Settings) SharesAt(d civil.Date) []Share {
	var best *CoveragePeriod
	for i := range s.Coverage {
		p := &s.Coverage[i]
		if !d.IsZero() && p.From.After(d) {
			continue
		}
		if best == nil || p.From.After(best.From) {
			best = p
		}
	}
	if best == nil {
		return nil
	}
	return best.Shares
}

// ShareOf returns the rate of payerID on d (0 if the payer does not cover that date).
func (s *Settings) ShareOf(payerID string, d civil.Date) money.Rate {
	for _, sh := range s.SharesAt(d) {
		if sh.PayerID == payerID {
			return sh.Rate
		}
	}
	return 0
}

// Year returns the settings of payerID for year (zero values if none).
func (s *Settings) Year(year int, payerID string) YearSetting {
	for _, y := range s.Years {
		if y.Year == year && y.PayerID == payerID {
			return y
		}
	}
	return YearSetting{Year: year, PayerID: payerID}
}

// Tolerance returns the deviation between SOLL and IST above which
// Klärungsbedarf is suggested.
func (s *Settings) Tolerance() money.Cents {
	if s.ClarifyTolerance > 0 {
		return s.ClarifyTolerance
	}
	return DefaultClarifyTolerance
}

// Problem is a finding of Settings.Validate.
type Problem struct {
	Error bool   `json:"error"` // false: warning only
	Text  string `json:"text"`
}

// Validate checks the settings. Errors make calculations wrong; warnings are unusual but possible.
func (s *Settings) Validate() []Problem {
	var out []Problem
	add := func(isErr bool, format string, args ...any) {
		out = append(out, Problem{Error: isErr, Text: fmt.Sprintf(format, args...)})
	}
	ids := map[string]bool{}
	for _, p := range s.Payers {
		switch {
		case p.ID == "" || strings.ContainsAny(p.ID, "/~ "):
			add(true, "Kostenträger %q: ungültige ID", p.Name)
		case ids[p.ID]:
			add(true, "Kostenträger-ID %q doppelt vergeben", p.ID)
		}
		ids[p.ID] = true
		if p.Kind != Beihilfe && p.Kind != PKV {
			add(true, "Kostenträger %q: Art muss beihilfe oder pkv sein", p.ID)
		}
	}
	starts := map[civil.Date]bool{}
	for _, c := range s.Coverage {
		if c.From.IsZero() {
			add(true, "Versicherungszeitraum ohne Beginn")
			continue
		}
		if starts[c.From] {
			add(true, "Zwei Versicherungszeiträume beginnen am %s", c.From.DE())
		}
		starts[c.From] = true
		var sum money.Rate
		for _, sh := range c.Shares {
			if !ids[sh.PayerID] {
				add(true, "Zeitraum ab %s: unbekannter Kostenträger %q", c.From.DE(), sh.PayerID)
			}
			if sh.Rate < 0 || sh.Rate > money.Full {
				add(true, "Zeitraum ab %s: Anteil %s %% außerhalb 0–100 %%", c.From.DE(), sh.Rate)
			}
			sum += sh.Rate
		}
		if sum != money.Full {
			add(false, "Zeitraum ab %s: Anteile ergeben %s %% statt 100 %%", c.From.DE(), sum)
		}
	}
	if len(s.Coverage) == 0 {
		add(true, "Kein Versicherungszeitraum angelegt")
	}
	return out
}

// Mark is a ticked checkbox together with the day it was ticked.
// Ticking sets today's date; the date can be edited afterwards.
type Mark struct {
	Date civil.Date `json:"date,omitzero"`
	Note string     `json:"note,omitempty"`
}

// Done reports whether the box is ticked.
func (m Mark) Done() bool { return !m.Date.IsZero() }

// IsZero lets `omitzero` drop unticked marks without a note.
func (m Mark) IsZero() bool { return m.Date.IsZero() && m.Note == "" }

// Category is the kind of service; payers sometimes treat categories differently.
type Category string

// Categories.
const (
	CatDoctor     Category = "arzt"
	CatDentist    Category = "zahnarzt"
	CatPharmacy   Category = "apotheke"
	CatTherapy    Category = "heilmittel"
	CatAids       Category = "hilfsmittel"
	CatHospital   Category = "krankenhaus"
	CatNaturopath Category = "heilpraktiker"
	CatOther      Category = "sonstiges"
)

// AllowanceHospitalDay is the Allowance.Kind of a Krankenhaustagegeld.
const AllowanceHospitalDay = "krankenhaustagegeld"

// Refund is money received from a payer for one invoice (IST).
type Refund struct {
	ID     string      `json:"id"`
	Amount money.Cents `json:"amount"`
	Date   civil.Date  `json:"date,omitzero"`
	DocID  string      `json:"docId,omitempty"`  // refund document it came from
	LineID string      `json:"lineId,omitempty"` // line on that document
	Note   string      `json:"note,omitempty"`
}

// Claim is the state of one invoice towards one payer. Unset overrides fall
// back to the invoice amount and the coverage share at the date of treatment.
type Claim struct {
	Eligible *money.Cents `json:"eligible,omitempty"` // erstattungsfähiger Betrag
	Rate     *money.Rate  `json:"rate,omitempty"`
	Expected *money.Cents `json:"expected,omitempty"` // SOLL, overrides the calculation

	Submitted Mark `json:"submitted,omitzero"`
	Refunded  Mark `json:"refunded,omitzero"`
	Clarify   Mark `json:"clarify,omitzero"` // Klärungsbedarf; Note holds the reason

	Held bool `json:"held,omitempty"` // zurückgehalten, e.g. to keep the BRE
	Skip bool `json:"skip,omitempty"` // will not be claimed
	// KeepsPremiumRefund: claiming this does not cost the BRE (e.g. Vorsorge, tariff-dependent).
	KeepsPremiumRefund bool `json:"keepsPremiumRefund,omitempty"`

	SubmissionID string   `json:"submissionId,omitempty"`
	Refunds      []Refund `json:"refunds,omitempty"`
}

// IsZero lets `omitzero` drop untouched claims.
func (c Claim) IsZero() bool {
	return c.Eligible == nil && c.Rate == nil && c.Expected == nil &&
		c.Submitted.IsZero() && c.Refunded.IsZero() && c.Clarify.IsZero() &&
		!c.Held && !c.Skip && !c.KeepsPremiumRefund && c.SubmissionID == "" && len(c.Refunds) == 0
}

// Invoice is one medical bill.
type Invoice struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted,omitempty"`

	Provider  string   `json:"provider"`            // Behandler
	Payee     string   `json:"payee,omitempty"`     // Zahlungsempfänger, e.g. a billing service
	IBAN      string   `json:"iban,omitempty"`      // from the GiroCode
	Reference string   `json:"reference,omitempty"` // invoice number / Verwendungszweck
	Category  Category `json:"category,omitempty"`

	InvoiceDate civil.Date `json:"invoiceDate,omitzero"`
	ServiceFrom civil.Date `json:"serviceFrom,omitzero"`
	ServiceTo   civil.Date `json:"serviceTo,omitzero"`
	// ServiceYearOverride replaces the Leistungsjahr derived from the dates.
	ServiceYearOverride int `json:"serviceYear,omitempty"`

	Amount money.Cents `json:"amount"`
	Due    civil.Date  `json:"due,omitzero"`
	Paid   Mark        `json:"paid,omitzero"` // paid to the provider (Anweisung/überwiesen)

	Claims      map[string]Claim `json:"claims,omitempty"`      // by payer ID
	Attachments []string         `json:"attachments,omitempty"` // file names in belege/
	Note        string           `json:"note,omitempty"`
}

// ServiceDate is the first day of treatment, or the invoice date if unknown.
func (inv *Invoice) ServiceDate() civil.Date {
	if !inv.ServiceFrom.IsZero() {
		return inv.ServiceFrom
	}
	return inv.InvoiceDate
}

// ServiceYear is the Leistungsjahr: the year of treatment unless overridden.
// The PKV premium refund and the Beihilfe rate both follow the date of treatment.
func (inv *Invoice) ServiceYear() int {
	if inv.ServiceYearOverride > 0 {
		return inv.ServiceYearOverride
	}
	return inv.ServiceDate().Year()
}

// Matches reports whether d is the invoice date, a treatment date, or within the treatment period.
func (inv *Invoice) Matches(d civil.Date) bool {
	if d.IsZero() {
		return false
	}
	if d.Equal(inv.InvoiceDate) || d.Equal(inv.ServiceFrom) || d.Equal(inv.ServiceTo) {
		return true
	}
	return !inv.ServiceFrom.IsZero() && !inv.ServiceTo.IsZero() && d.Between(inv.ServiceFrom, inv.ServiceTo)
}

// PayerIDs returns the payers with a claim on this invoice under s: those of
// the coverage at the date of treatment, then any other payer that has claim
// data (sorted by ID).
func (inv *Invoice) PayerIDs(s *Settings) []string {
	var ids []string
	for _, sh := range s.SharesAt(inv.ServiceDate()) {
		ids = append(ids, sh.PayerID)
	}
	var extra []string
	for id := range inv.Claims {
		if !slices.Contains(ids, id) {
			extra = append(extra, id)
		}
	}
	slices.Sort(extra)
	return append(ids, extra...)
}

// Deduction is an amount a refund document subtracts from the sum of its lines.
type Deduction struct {
	Label  string      `json:"label"`
	Amount money.Cents `json:"amount"`
}

// Match states of a refund document line.
const (
	MatchAuto      = "auto"
	MatchSuggest   = "suggest"
	MatchConfirmed = "confirmed"
	MatchManual    = "manual"
	MatchNone      = "none"
)

// RefundLine is one position on a refund document.
type RefundLine struct {
	ID         string      `json:"id"`
	Date       civil.Date  `json:"date,omitzero"`
	Provider   string      `json:"provider,omitempty"`
	Billed     money.Cents `json:"billed,omitzero"`   // Rechnungsbetrag
	Eligible   money.Cents `json:"eligible,omitzero"` // anerkannt / beihilfefähig
	Refund     money.Cents `json:"refund"`
	InvoiceIDs []string    `json:"invoiceIds,omitempty"`
	Match      string      `json:"match,omitempty"`
	Page       int         `json:"page,omitempty"`
	Box        []float64   `json:"box,omitempty"` // x, y, w, h on the page (0..1)
}

// Refund document states.
const (
	DocInbox     = "inbox"
	DocConfirmed = "confirmed"
)

// RefundDoc is a Beihilfebescheid or a PKV-Leistungsabrechnung.
type RefundDoc struct {
	ID              string       `json:"id"`
	Deleted         bool         `json:"deleted,omitempty"`
	PayerID         string       `json:"payerId"`
	Date            civil.Date   `json:"date,omitzero"`
	ApplicationDate civil.Date   `json:"applicationDate,omitzero"` // "Ihr Antrag vom"
	PayoutDate      civil.Date   `json:"payoutDate,omitzero"`
	PaidOut         money.Cents  `json:"paidOut,omitzero"`
	Deductions      []Deduction  `json:"deductions,omitempty"`
	Lines           []RefundLine `json:"lines,omitempty"`
	Attachment      string       `json:"attachment,omitempty"`
	State           string       `json:"state,omitempty"`
	Note            string       `json:"note,omitempty"`
}

// LinesTotal is the sum of the refunds of all lines.
func (d *RefundDoc) LinesTotal() money.Cents {
	var t money.Cents
	for _, l := range d.Lines {
		t += l.Refund
	}
	return t
}

// ChecksumOK reports whether lines minus deductions equal the payout.
func (d *RefundDoc) ChecksumOK() bool {
	t := d.LinesTotal()
	for _, x := range d.Deductions {
		t -= x.Amount
	}
	return t == d.PaidOut
}

// Submission records that several invoices were sent to a payer together.
type Submission struct {
	ID         string     `json:"id"`
	Deleted    bool       `json:"deleted,omitempty"`
	PayerID    string     `json:"payerId"`
	Date       civil.Date `json:"date"`
	InvoiceIDs []string   `json:"invoiceIds"`
	Channel    string     `json:"channel,omitempty"` // app, portal, post …
	Note       string     `json:"note,omitempty"`
}

// Allowance is a benefit without an invoice, e.g. Krankenhaustagegeld.
type Allowance struct {
	ID       string      `json:"id"`
	Deleted  bool        `json:"deleted,omitempty"`
	PayerID  string      `json:"payerId"`
	Kind     string      `json:"kind"`
	Date     civil.Date  `json:"date"`
	Amount   money.Cents `json:"amount"`
	Received Mark        `json:"received,omitzero"`
	Note     string      `json:"note,omitempty"`
}

// NewID returns a new record ID: the creation day plus 32 random bits.
func NewID(now time.Time) string {
	var b [4]byte
	_, _ = rand.Read(b[:]) // never fails since Go 1.24
	return now.Format("20060102") + "-" + hex.EncodeToString(b[:])
}
