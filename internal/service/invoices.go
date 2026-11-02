package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/calc"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/leaf"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// ListInvoices returns the invoices of a Leistungsjahr (0 = all), oldest first.
func (s *Service) ListInvoices(year int) ([]InvoiceRowDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return nil, err
	}
	rows := []InvoiceRowDTO{}
	today := s.today()
	for i := range data.Invoices {
		inv := &data.Invoices[i]
		if inv.Deleted || (year != 0 && inv.ServiceYear() != year) {
			continue
		}
		rows = append(rows, rowDTO(inv, calc.Invoice(&data.Settings, inv, today)))
	}
	return rows, nil
}

// GetInvoice returns one invoice for the form.
func (s *Service) GetInvoice(id string) (InvoiceDetailDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	inv, err := findInvoice(data, id)
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	return s.detail(&data.Settings, inv), nil
}

func (s *Service) detail(set *model.Settings, inv *model.Invoice) InvoiceDetailDTO {
	return InvoiceDetailDTO{Invoice: invoiceDTO(set, inv), View: rowDTO(inv, calc.Invoice(set, inv, s.today()))}
}

// NewInvoice returns an empty invoice dated today, with the claims of the current coverage.
func (s *Service) NewInvoice() (InvoiceDetailDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	inv := model.Invoice{InvoiceDate: s.today()}
	return s.detail(&data.Settings, &inv), nil
}

// PreviewInvoice computes the derived values of unsaved form input (live SOLL/IST).
func (s *Service) PreviewInvoice(d InvoiceDTO) (InvoiceDetailDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	inv, err := fromDTO(d, &data.Settings, s.newID)
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	out := s.detail(&data.Settings, &inv)
	// Keep the user's claim rows (also untouched ones) in the form.
	out.Invoice = mergeClaimRows(out.Invoice, d)
	return out, nil
}

// mergeClaimRows keeps form rows for payers whose claim is still empty.
func mergeClaimRows(computed, input InvoiceDTO) InvoiceDTO {
	have := map[string]bool{}
	for _, c := range computed.Claims {
		have[c.PayerID] = true
	}
	for _, c := range input.Claims {
		if !have[c.PayerID] && c.PayerID != "" {
			computed.Claims = append(computed.Claims, c)
		}
	}
	return computed
}

func validateInvoice(inv *model.Invoice) error {
	var errs []string
	if inv.Provider == "" {
		errs = append(errs, "Behandler fehlt")
	}
	if inv.InvoiceDate.IsZero() {
		errs = append(errs, "Rechnungsdatum fehlt")
	}
	if inv.Amount <= 0 {
		errs = append(errs, "Betrag fehlt")
	}
	if !inv.ServiceTo.IsZero() && !inv.ServiceFrom.IsZero() && inv.ServiceTo.Before(inv.ServiceFrom) {
		errs = append(errs, "Behandlung bis liegt vor Behandlung von")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}

// SaveInvoice creates (empty ID) or updates an invoice. original is the
// invoice as it was loaded into the form (nil for new invoices): if the
// record changed in the meantime – e.g. on another device – only the fields
// changed in the form are applied to the current version.
func (s *Service) SaveInvoice(d InvoiceDTO, original *InvoiceDTO) (InvoiceDetailDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, data, err := s.ready()
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	inv, err := fromDTO(d, &data.Settings, s.newID)
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	if inv.ID == "" {
		inv.ID = s.newID()
	} else {
		cur, err := findInvoice(data, inv.ID)
		if err != nil {
			return InvoiceDetailDTO{}, err
		}
		if original != nil && original.ID == inv.ID {
			base, err := fromDTO(*original, &data.Settings, s.newID)
			if err != nil {
				return InvoiceDetailDTO{}, err
			}
			if inv, err = rebaseInvoice(base, inv, *cur); err != nil {
				return InvoiceDetailDTO{}, err
			}
		}
	}
	if err := validateInvoice(&inv); err != nil {
		return InvoiceDetailDTO{}, err
	}
	saved, err := st.SaveInvoice(inv)
	if err != nil {
		return InvoiceDetailDTO{}, fmt.Errorf("Speichern fehlgeschlagen: %w", err)
	}
	replaceInvoice(data, saved)
	return s.detail(&data.Settings, &saved), nil
}

// rebaseInvoice applies the changes from base to mine onto theirs (field by field).
func rebaseInvoice(base, mine, theirs model.Invoice) (model.Invoice, error) {
	b, err := json.Marshal(base)
	if err != nil {
		return mine, err
	}
	m, err := json.Marshal(mine)
	if err != nil {
		return mine, err
	}
	t, err := json.Marshal(theirs)
	if err != nil {
		return mine, err
	}
	out, err := leaf.Rebase(b, m, t)
	if err != nil {
		return mine, err
	}
	var inv model.Invoice
	err = json.Unmarshal(out, &inv)
	return inv, err
}

// DeleteInvoice marks an invoice as deleted (it stays in the file, so the
// deletion also reaches other devices).
func (s *Service) DeleteInvoice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, data, err := s.ready()
	if err != nil {
		return err
	}
	inv, err := findInvoice(data, id)
	if err != nil {
		return err
	}
	del := *inv
	del.Deleted = true
	saved, err := st.SaveInvoice(del)
	if err != nil {
		return fmt.Errorf("Löschen fehlgeschlagen: %w", err)
	}
	replaceInvoice(data, saved)
	return nil
}

// Mark names accepted by SetMark.
const (
	MarkPaid               = "paid"
	MarkSubmitted          = "submitted"
	MarkRefunded           = "refunded"
	MarkClarify            = "clarify"
	MarkHeld               = "held"
	MarkSkip               = "skip"
	MarkKeepsPremiumRefund = "keepsPremiumRefund"
	// MarkAcceptDeviation accepts what the payer refunded: SOLL becomes IST
	// (e.g. a capped amount) and Klärungsbedarf is cleared.
	MarkAcceptDeviation = "acceptDeviation"
)

// MarkChangeDTO ticks or unticks one checkbox of an invoice.
type MarkChangeDTO struct {
	InvoiceID string `json:"invoiceId"`
	PayerID   string `json:"payerId"` // empty for "paid"
	Mark      string `json:"mark"`
	On        bool   `json:"on"`
	Date      string `json:"date"`   // empty = today
	Note      string `json:"note"`   // reason for Klärungsbedarf
	Amount    string `json:"amount"` // refund amount for "refunded"; empty = open SOLL
}

// SetMark ticks or unticks a checkbox. Ticking "refunded" records a refund of
// the given amount (default: what is still open of the SOLL); unticking it
// removes refunds that were entered by hand.
func (s *Service) SetMark(ch MarkChangeDTO) (InvoiceRowDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, data, err := s.ready()
	if err != nil {
		return InvoiceRowDTO{}, err
	}
	cur, err := findInvoice(data, ch.InvoiceID)
	if err != nil {
		return InvoiceRowDTO{}, err
	}
	inv := cloneInvoice(cur)
	date, err := parseDate("Datum", ch.Date)
	if err != nil {
		return InvoiceRowDTO{}, err
	}
	if date.IsZero() {
		date = s.today()
	}
	mark := func() model.Mark {
		if !ch.On {
			return model.Mark{}
		}
		return model.Mark{Date: date, Note: strings.TrimSpace(ch.Note)}
	}
	if ch.Mark == MarkPaid {
		inv.Paid = mark()
	} else {
		if !slices.Contains(inv.PayerIDs(&data.Settings), ch.PayerID) {
			return InvoiceRowDTO{}, fmt.Errorf("Kostenträger %q gehört nicht zu dieser Rechnung", ch.PayerID)
		}
		c := inv.Claims[ch.PayerID]
		switch ch.Mark {
		case MarkSubmitted:
			c.Submitted = mark()
			if ch.On {
				c.Held = false
			}
		case MarkRefunded:
			if err := s.setRefunded(&c, &data.Settings, &inv, ch, date); err != nil {
				return InvoiceRowDTO{}, err
			}
		case MarkClarify:
			c.Clarify = mark()
		case MarkHeld:
			c.Held = ch.On
		case MarkSkip:
			c.Skip = ch.On
		case MarkKeepsPremiumRefund:
			c.KeepsPremiumRefund = ch.On
		case MarkAcceptDeviation:
			var actual money.Cents
			for _, r := range c.Refunds {
				actual += r.Amount
			}
			if len(c.Refunds) == 0 {
				return InvoiceRowDTO{}, errors.New("noch keine Erstattung erfasst")
			}
			c.Expected = &actual
			c.Clarify = model.Mark{}
		default:
			return InvoiceRowDTO{}, fmt.Errorf("unbekanntes Häkchen %q", ch.Mark)
		}
		if c.IsZero() {
			delete(inv.Claims, ch.PayerID)
		} else {
			inv.Claims[ch.PayerID] = c
		}
	}
	saved, err := st.SaveInvoice(inv)
	if err != nil {
		return InvoiceRowDTO{}, fmt.Errorf("Speichern fehlgeschlagen: %w", err)
	}
	replaceInvoice(data, saved)
	return rowDTO(&saved, calc.Invoice(&data.Settings, &saved, s.today())), nil
}

func (s *Service) setRefunded(c *model.Claim, set *model.Settings, inv *model.Invoice, ch MarkChangeDTO, date civil.Date) error {
	if !ch.On {
		c.Refunded = model.Mark{}
		var keep []model.Refund
		for _, r := range c.Refunds {
			if r.DocID != "" {
				keep = append(keep, r)
			}
		}
		c.Refunds = keep
		return nil
	}
	amount, err := parseAmount("Erstattung", ch.Amount)
	if err != nil {
		return err
	}
	if strings.TrimSpace(ch.Amount) == "" {
		// Default: what is still open of the SOLL.
		tmp := *inv
		tmp.Claims = map[string]model.Claim{ch.PayerID: *c}
		for _, cv := range calc.Invoice(set, &tmp, date).Claims {
			if cv.PayerID == ch.PayerID {
				amount = cv.Expected - cv.Actual
				if amount <= 0 {
					amount = cv.Expected
				}
			}
		}
	}
	if amount <= 0 {
		return errors.New("Erstattungsbetrag fehlt")
	}
	c.Refunds = append(c.Refunds, model.Refund{ID: s.newID(), Amount: amount, Date: date})
	c.Refunded = model.Mark{Date: date}
	return nil
}

func cloneInvoice(inv *model.Invoice) model.Invoice {
	out := *inv
	out.Claims = make(map[string]model.Claim, len(inv.Claims))
	for k, v := range inv.Claims {
		v.Refunds = slices.Clone(v.Refunds)
		out.Claims[k] = v
	}
	out.Attachments = slices.Clone(inv.Attachments)
	return out
}

// BulkSubmitDTO marks several invoices as submitted to one payer together.
type BulkSubmitDTO struct {
	InvoiceIDs []string `json:"invoiceIds"`
	PayerID    string   `json:"payerId"`
	Date       string   `json:"date"` // empty = today
	Channel    string   `json:"channel"`
	Note       string   `json:"note"`
}

// BulkSubmitResultDTO reports what BulkSubmit did.
type BulkSubmitResultDTO struct {
	Marked  int      `json:"marked"`
	Skipped []string `json:"skipped"` // providers of invoices that were not changed, with reason
}

// BulkSubmit ticks "eingereicht" for several invoices and records one Einreichung.
func (s *Service) BulkSubmit(b BulkSubmitDTO) (BulkSubmitResultDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := BulkSubmitResultDTO{Skipped: []string{}}
	st, data, err := s.ready()
	if err != nil {
		return res, err
	}
	if _, ok := data.Settings.Payer(b.PayerID); !ok {
		return res, fmt.Errorf("unbekannter Kostenträger %q", b.PayerID)
	}
	date, err := parseDate("Datum", b.Date)
	if err != nil {
		return res, err
	}
	if date.IsZero() {
		date = s.today()
	}
	sub := model.Submission{ID: s.newID(), PayerID: b.PayerID, Date: date,
		Channel: strings.TrimSpace(b.Channel), Note: strings.TrimSpace(b.Note)}
	var changed []model.Invoice
	for _, id := range b.InvoiceIDs {
		cur, err := findInvoice(data, id)
		if err != nil {
			return res, err
		}
		inv := cloneInvoice(cur)
		label := inv.Provider + " (" + inv.InvoiceDate.DE() + ")"
		c := inv.Claims[b.PayerID]
		switch {
		case !slices.Contains(inv.PayerIDs(&data.Settings), b.PayerID):
			res.Skipped = append(res.Skipped, label+": kein Anspruch bei diesem Kostenträger")
			continue
		case c.Submitted.Done():
			res.Skipped = append(res.Skipped, label+": schon eingereicht am "+c.Submitted.Date.DE())
			continue
		}
		c.Submitted = model.Mark{Date: date}
		c.Held = false
		c.SubmissionID = sub.ID
		inv.Claims[b.PayerID] = c
		changed = append(changed, inv)
		sub.InvoiceIDs = append(sub.InvoiceIDs, inv.ID)
	}
	if len(changed) == 0 {
		return res, nil
	}
	if _, err := st.SaveSubmission(sub); err != nil {
		return res, fmt.Errorf("Einreichung konnte nicht gespeichert werden: %w", err)
	}
	data.Submissions = append(data.Submissions, sub)
	for _, inv := range changed {
		saved, err := st.SaveInvoice(inv)
		if err != nil {
			return res, fmt.Errorf("Speichern fehlgeschlagen: %w", err)
		}
		replaceInvoice(data, saved)
		res.Marked++
	}
	return res, nil
}
