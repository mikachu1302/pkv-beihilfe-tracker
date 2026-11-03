package service

import (
	"fmt"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/calc"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// The frontend gets amounts as strings in the machine format ("286.40"),
// rates as percentage strings ("70") and dates as "YYYY-MM-DD" ("" = none).
// Input strings may also use the German format ("286,40").

// MarkDTO is a checkbox with its date.
type MarkDTO struct {
	Done bool   `json:"done"`
	Date string `json:"date"`
	Note string `json:"note"`
}

// RefundDTO is one refund (IST).
type RefundDTO struct {
	ID     string `json:"id"`
	Amount string `json:"amount"`
	Date   string `json:"date"`
	DocID  string `json:"docId"`
	LineID string `json:"lineId"`
	Note   string `json:"note"`
}

// ClaimDTO is the editable state of an invoice towards one payer. Empty
// Eligible, Rate and Expected mean "calculate".
type ClaimDTO struct {
	PayerID            string      `json:"payerId"`
	Eligible           string      `json:"eligible"`
	Rate               string      `json:"rate"`
	Expected           string      `json:"expected"`
	Submitted          MarkDTO     `json:"submitted"`
	Refunded           MarkDTO     `json:"refunded"`
	Clarify            MarkDTO     `json:"clarify"`
	Held               bool        `json:"held"`
	Skip               bool        `json:"skip"`
	KeepsPremiumRefund bool        `json:"keepsPremiumRefund"`
	SubmissionID       string      `json:"submissionId"`
	Refunds            []RefundDTO `json:"refunds"`
}

// InvoiceDTO is the editable invoice.
type InvoiceDTO struct {
	ID          string     `json:"id"`
	Provider    string     `json:"provider"`
	Payee       string     `json:"payee"`
	IBAN        string     `json:"iban"`
	Reference   string     `json:"reference"`
	Category    string     `json:"category"`
	InvoiceDate string     `json:"invoiceDate"`
	ServiceFrom string     `json:"serviceFrom"`
	ServiceTo   string     `json:"serviceTo"`
	ServiceYear int        `json:"serviceYear"` // 0 = from the dates
	Amount      string     `json:"amount"`
	Due         string     `json:"due"`
	Paid        MarkDTO    `json:"paid"`
	Claims      []ClaimDTO `json:"claims"`
	Attachments []string   `json:"attachments"`
	Note        string     `json:"note"`
}

// WarningDTO is a plausibility finding.
type WarningDTO struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// ClaimViewDTO holds the derived values of one claim.
type ClaimViewDTO struct {
	PayerID            string  `json:"payerId"`
	PayerName          string  `json:"payerName"`
	Kind               string  `json:"kind"`
	Rate               string  `json:"rate"`
	Eligible           string  `json:"eligible"`
	Expected           string  `json:"expected"`
	Actual             string  `json:"actual"`
	Diff               string  `json:"diff"`
	Status             string  `json:"status"`
	Submitted          MarkDTO `json:"submitted"`
	Refunded           MarkDTO `json:"refunded"`
	Clarify            MarkDTO `json:"clarify"`
	Held               bool    `json:"held"`
	Skip               bool    `json:"skip"`
	KeepsPremiumRefund bool    `json:"keepsPremiumRefund"`
	SuggestClarify     bool    `json:"suggestClarify"`
	Deadline           string  `json:"deadline"`
	DeadlineDays       int     `json:"deadlineDays"`
}

// InvoiceRowDTO is an invoice with its derived values, as shown in lists.
type InvoiceRowDTO struct {
	ID          string         `json:"id"`
	Provider    string         `json:"provider"`
	Category    string         `json:"category"`
	Reference   string         `json:"reference"`
	InvoiceDate string         `json:"invoiceDate"`
	ServiceDate string         `json:"serviceDate"`
	Year        int            `json:"year"`
	Amount      string         `json:"amount"`
	Paid        MarkDTO        `json:"paid"`
	Claims      []ClaimViewDTO `json:"claims"`
	Expected    string         `json:"expected"`
	Actual      string         `json:"actual"`
	OwnShare    string         `json:"ownShare"`
	Settled     bool           `json:"settled"`
	Warnings    []WarningDTO   `json:"warnings"`
	Note        string         `json:"note"`
	Attachments int            `json:"attachments"`
}

// InvoiceDetailDTO is the invoice for the form plus its derived values.
type InvoiceDetailDTO struct {
	Invoice InvoiceDTO    `json:"invoice"`
	View    InvoiceRowDTO `json:"view"`
	// MovedToInbox: documents removed in the form that went back into the
	// inbox (SaveInvoice only).
	MovedToInbox int `json:"movedToInbox"`
}

func markDTO(m model.Mark) MarkDTO {
	return MarkDTO{Done: m.Done(), Date: m.Date.String(), Note: m.Note}
}

func fmtRate(r money.Rate) string { return r.String() }

func optCents(c *money.Cents) string {
	if c == nil {
		return ""
	}
	return c.String()
}

func optRate(r *money.Rate) string {
	if r == nil {
		return ""
	}
	return r.String()
}

func invoiceDTO(s *model.Settings, inv *model.Invoice) InvoiceDTO {
	d := InvoiceDTO{
		ID: inv.ID, Provider: inv.Provider, Payee: inv.Payee, IBAN: inv.IBAN, Reference: inv.Reference,
		Category: string(inv.Category), InvoiceDate: inv.InvoiceDate.String(), ServiceFrom: inv.ServiceFrom.String(),
		ServiceTo: inv.ServiceTo.String(), ServiceYear: inv.ServiceYearOverride, Amount: inv.Amount.String(),
		Due: inv.Due.String(), Paid: markDTO(inv.Paid), Attachments: append([]string{}, inv.Attachments...),
		Note: inv.Note, Claims: []ClaimDTO{},
	}
	for _, id := range inv.PayerIDs(s) {
		c := inv.Claims[id]
		cd := ClaimDTO{
			PayerID: id, Eligible: optCents(c.Eligible), Rate: optRate(c.Rate), Expected: optCents(c.Expected),
			Submitted: markDTO(c.Submitted), Refunded: markDTO(c.Refunded), Clarify: markDTO(c.Clarify),
			Held: c.Held, Skip: c.Skip, KeepsPremiumRefund: c.KeepsPremiumRefund, SubmissionID: c.SubmissionID,
			Refunds: []RefundDTO{},
		}
		for _, r := range c.Refunds {
			cd.Refunds = append(cd.Refunds, RefundDTO{ID: r.ID, Amount: r.Amount.String(), Date: r.Date.String(),
				DocID: r.DocID, LineID: r.LineID, Note: r.Note})
		}
		d.Claims = append(d.Claims, cd)
	}
	return d
}

func rowDTO(inv *model.Invoice, v calc.InvoiceView) InvoiceRowDTO {
	r := InvoiceRowDTO{
		ID: inv.ID, Provider: inv.Provider, Category: string(inv.Category), Reference: inv.Reference,
		InvoiceDate: inv.InvoiceDate.String(), ServiceDate: v.ServiceDate.String(), Year: v.Year,
		Amount: inv.Amount.String(), Paid: markDTO(inv.Paid), Expected: v.Expected.String(),
		Actual: v.Actual.String(), OwnShare: v.OwnShare.String(), Settled: v.Settled, Note: inv.Note,
		Attachments: len(inv.Attachments), Claims: []ClaimViewDTO{}, Warnings: []WarningDTO{},
	}
	for _, c := range v.Claims {
		ic := inv.Claims[c.PayerID]
		r.Claims = append(r.Claims, ClaimViewDTO{
			PayerID: c.PayerID, PayerName: c.PayerName, Kind: string(c.Kind), Rate: fmtRate(c.Rate),
			Eligible: c.Eligible.String(), Expected: c.Expected.String(), Actual: c.Actual.String(),
			Diff: c.Diff.String(), Status: string(c.Status), Submitted: markDTO(c.Submitted),
			Refunded: markDTO(c.Refunded), Clarify: markDTO(c.Clarify), Held: ic.Held, Skip: ic.Skip,
			KeepsPremiumRefund: c.KeepsPremiumRefund, SuggestClarify: c.SuggestClarify,
			Deadline: c.Deadline.String(), DeadlineDays: c.DeadlineDays,
		})
	}
	for _, w := range v.Warnings {
		r.Warnings = append(r.Warnings, WarningDTO(w))
	}
	return r
}

// --- parsing of user input ---

func parseAmount(label, s string) (money.Cents, error) {
	if strings.TrimSpace(s) == "" {
		return 0, nil
	}
	v, err := money.Parse(s)
	if err != nil {
		return 0, fmt.Errorf("%s: „%s“ ist kein gültiger Betrag", label, s)
	}
	return v, nil
}

func parseOptAmount(label, s string) (*money.Cents, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	v, err := parseAmount(label, s)
	return &v, err
}

func parseRate(label, s string) (money.Rate, error) {
	var r money.Rate
	if err := r.UnmarshalJSON([]byte(`"` + strings.TrimSpace(s) + `"`)); err != nil || r < 0 || r > money.Full {
		return 0, fmt.Errorf("%s: „%s“ ist kein gültiger Prozentsatz (0–100)", label, s)
	}
	return r, nil
}

func parseOptRate(label, s string) (*money.Rate, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	r, err := parseRate(label, s)
	return &r, err
}

func parseDate(label, s string) (civil.Date, error) {
	if strings.TrimSpace(s) == "" {
		return civil.Date{}, nil
	}
	d, err := civil.Parse(s)
	if err != nil {
		return civil.Date{}, fmt.Errorf("%s: „%s“ ist kein gültiges Datum", label, s)
	}
	return d, nil
}

func parseMark(label string, m MarkDTO) (model.Mark, error) {
	if !m.Done {
		return model.Mark{Note: strings.TrimSpace(m.Note)}, nil
	}
	d, err := parseDate(label, m.Date)
	if err != nil {
		return model.Mark{}, err
	}
	if d.IsZero() {
		return model.Mark{}, fmt.Errorf("%s: Datum fehlt", label)
	}
	return model.Mark{Date: d, Note: strings.TrimSpace(m.Note)}, nil
}

// fromDTO converts form input into an invoice. Claims without any data are dropped.
func fromDTO(d InvoiceDTO, s *model.Settings, newID func() string) (model.Invoice, error) {
	var err error
	inv := model.Invoice{
		ID: d.ID, Provider: strings.TrimSpace(d.Provider), Payee: strings.TrimSpace(d.Payee),
		IBAN: strings.ToUpper(strings.ReplaceAll(d.IBAN, " ", "")), Reference: strings.TrimSpace(d.Reference),
		Category: model.Category(d.Category), ServiceYearOverride: d.ServiceYear, Note: strings.TrimSpace(d.Note),
		Attachments: d.Attachments, Claims: map[string]model.Claim{},
	}
	if len(inv.Attachments) == 0 {
		inv.Attachments = nil
	}
	errs := []error{}
	collect := func(e error) {
		if e != nil {
			errs = append(errs, e)
		}
	}
	inv.InvoiceDate, err = parseDate("Rechnungsdatum", d.InvoiceDate)
	collect(err)
	inv.ServiceFrom, err = parseDate("Behandlung von", d.ServiceFrom)
	collect(err)
	inv.ServiceTo, err = parseDate("Behandlung bis", d.ServiceTo)
	collect(err)
	inv.Due, err = parseDate("Fällig am", d.Due)
	collect(err)
	inv.Amount, err = parseAmount("Betrag", d.Amount)
	collect(err)
	inv.Paid, err = parseMark("Bezahlt", d.Paid)
	collect(err)
	for _, cd := range d.Claims {
		p, _ := s.Payer(cd.PayerID)
		name := p.Name
		if name == "" {
			name = cd.PayerID
		}
		var c model.Claim
		c.Eligible, err = parseOptAmount(name+" – erstattungsfähig", cd.Eligible)
		collect(err)
		c.Rate, err = parseOptRate(name+" – Satz", cd.Rate)
		collect(err)
		c.Expected, err = parseOptAmount(name+" – SOLL", cd.Expected)
		collect(err)
		c.Submitted, err = parseMark(name+" – eingereicht", cd.Submitted)
		collect(err)
		c.Refunded, err = parseMark(name+" – erstattet", cd.Refunded)
		collect(err)
		c.Clarify, err = parseMark(name+" – Klärungsbedarf", cd.Clarify)
		collect(err)
		c.Held, c.Skip, c.KeepsPremiumRefund, c.SubmissionID = cd.Held, cd.Skip, cd.KeepsPremiumRefund, cd.SubmissionID
		for _, rd := range cd.Refunds {
			amount, err := parseAmount(name+" – Erstattung", rd.Amount)
			collect(err)
			date, err := parseDate(name+" – Erstattungsdatum", rd.Date)
			collect(err)
			if amount == 0 && rd.ID == "" {
				continue // empty new row
			}
			id := rd.ID
			if id == "" {
				id = newID()
			}
			c.Refunds = append(c.Refunds, model.Refund{ID: id, Amount: amount, Date: date, DocID: rd.DocID,
				LineID: rd.LineID, Note: strings.TrimSpace(rd.Note)})
		}
		if !c.IsZero() {
			inv.Claims[cd.PayerID] = c
		}
	}
	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return inv, fmt.Errorf("%s", strings.Join(msgs, "\n"))
	}
	return inv, nil
}
