package service

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/calc"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/export"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
)

// ExportExcel writes the invoices, yearly figures and allowances of a year
// (0 = all years) as an Excel file and returns its path.
func (s *Service) ExportExcel(year int, path string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", errors.New("kein Dateiname")
	}
	if !strings.HasSuffix(strings.ToLower(path), ".xlsx") {
		path += ".xlsx"
	}
	f, err := export.Workbook(&data.Settings, data.Invoices, data.Allowances, year, s.today())
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := f.SaveAs(path); err != nil {
		return "", fmt.Errorf("Datei konnte nicht gespeichert werden: %w", err)
	}
	return path, nil
}

// SubmissionPDFDTO reports the written submission bundle.
type SubmissionPDFDTO struct {
	Path      string   `json:"path"`
	Invoices  int      `json:"invoices"`
	Documents int      `json:"documents"`
	Missing   []string `json:"missing"` // providers of invoices without a document
}

// ExportSubmission writes one PDF for a submission to payerID: a cover page
// listing the invoices, followed by their documents.
func (s *Service) ExportSubmission(invoiceIDs []string, payerID, path string) (SubmissionPDFDTO, error) {
	if strings.TrimSpace(path) == "" {
		return SubmissionPDFDTO{}, errors.New("kein Dateiname")
	}
	if !strings.HasSuffix(strings.ToLower(path), ".pdf") {
		path += ".pdf"
	}
	b, err := s.bundle(invoiceIDs, payerID)
	if err != nil {
		return SubmissionPDFDTO{}, err
	}
	// Merging the documents can take a while: done without holding the lock.
	res, err := export.WriteBundle(b, path)
	if err != nil {
		return SubmissionPDFDTO{}, fmt.Errorf("PDF konnte nicht erstellt werden: %w", err)
	}
	out := SubmissionPDFDTO{Path: path, Invoices: len(b.Items), Documents: res.Documents, Missing: res.Missing}
	if out.Missing == nil {
		out.Missing = []string{}
	}
	return out, nil
}

// bundle collects what ExportSubmission writes.
func (s *Service) bundle(invoiceIDs []string, payerID string) (export.Bundle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, data, err := s.ready()
	if err != nil {
		return export.Bundle{}, err
	}
	p, ok := data.Settings.Payer(payerID)
	if !ok {
		return export.Bundle{}, fmt.Errorf("unbekannter Kostenträger %q", payerID)
	}
	b := export.Bundle{Payer: p.Name, Date: s.today()}
	for _, id := range invoiceIDs {
		inv, err := findInvoice(data, id)
		if err != nil {
			return export.Bundle{}, err
		}
		item := export.BundleItem{Provider: inv.Provider, ServiceDate: inv.ServiceDate(), InvoiceDate: inv.InvoiceDate,
			Reference: inv.Reference, Amount: inv.Amount}
		for _, a := range inv.Attachments {
			if path, err := st.DocumentPath(a); err == nil {
				item.Documents = append(item.Documents, path)
			}
		}
		b.Items = append(b.Items, item)
	}
	slices.SortStableFunc(b.Items, func(a, c export.BundleItem) int { return a.ServiceDate.Compare(c.ServiceDate) })
	return b, nil
}

// ReminderDTO is something to do.
type ReminderDTO struct {
	Kind      string `json:"kind"` // deadline, payment, refund, clarify, deviation, premium, inbox
	Text      string `json:"text"`
	InvoiceID string `json:"invoiceId"`
	PayerID   string `json:"payerId"`
	Year      int    `json:"year"`
	Urgent    bool   `json:"urgent"`
}

// Reminders lists what needs attention: claim deadlines, unpaid invoices,
// refunds that take long, Klärungsbedarf, the premium refund decision and
// documents in the inbox. Urgent items come first.
func (s *Service) Reminders() ([]ReminderDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, data, err := s.ready()
	if err != nil {
		return nil, err
	}
	today := s.today()
	set := &data.Settings
	var out []ReminderDTO
	add := func(r ReminderDTO) { out = append(out, r) }
	heldYears := map[[2]any]bool{}
	for i := range data.Invoices {
		inv := &data.Invoices[i]
		if inv.Deleted {
			continue
		}
		v := calc.Invoice(set, inv, today)
		label := fmt.Sprintf("%s vom %s (%s €)", inv.Provider, inv.InvoiceDate.DE(), inv.Amount.DE())
		if !inv.Paid.Done() {
			switch {
			case !inv.Due.IsZero() && !inv.Due.After(today):
				add(ReminderDTO{Kind: "payment", Text: "Zahlung fällig seit " + inv.Due.DE() + ": " + label, InvoiceID: inv.ID, Year: v.Year, Urgent: true})
			case inv.Due.IsZero() && !inv.InvoiceDate.IsZero() && today.DaysUntil(inv.InvoiceDate) < -30 && today.DaysUntil(inv.InvoiceDate) > -120:
				add(ReminderDTO{Kind: "payment", Text: "Noch nicht als bezahlt markiert: " + label, InvoiceID: inv.ID, Year: v.Year})
			}
		}
		for _, c := range v.Claims {
			switch {
			case !c.Deadline.IsZero() && c.DeadlineDays < 0:
				add(ReminderDTO{Kind: "deadline", Text: fmt.Sprintf("Antragsfrist bei %s abgelaufen (%s): %s", c.PayerName, c.Deadline.DE(), label),
					InvoiceID: inv.ID, PayerID: c.PayerID, Year: v.Year, Urgent: true})
			case c.DeadlineSoon():
				add(ReminderDTO{Kind: "deadline", Text: fmt.Sprintf("Bei %s bis %s einreichen (noch %d Tage): %s", c.PayerName, c.Deadline.DE(), c.DeadlineDays, label),
					InvoiceID: inv.ID, PayerID: c.PayerID, Year: v.Year, Urgent: c.DeadlineDays <= 30})
			}
			if c.LongWait() {
				add(ReminderDTO{Kind: "refund", Text: fmt.Sprintf("Erstattung von %s seit %d Wochen offen: %s", c.PayerName, c.WaitingDays/7, label),
					InvoiceID: inv.ID, PayerID: c.PayerID, Year: v.Year})
			}
			if c.Clarify.Done() {
				note := ""
				if c.Clarify.Note != "" {
					note = " – " + c.Clarify.Note
				}
				add(ReminderDTO{Kind: "clarify", Text: fmt.Sprintf("Klärungsbedarf bei %s%s: %s", c.PayerName, note, label),
					InvoiceID: inv.ID, PayerID: c.PayerID, Year: v.Year})
			} else if c.SuggestClarify {
				add(ReminderDTO{Kind: "deviation", Text: fmt.Sprintf("%s hat %s € statt %s € erstattet: %s", c.PayerName, c.Actual.DE(), c.Expected.DE(), label),
					InvoiceID: inv.ID, PayerID: c.PayerID, Year: v.Year})
			}
			if c.Kind == model.PKV && c.Status == calc.StatusHeld {
				heldYears[[2]any{v.Year, c.PayerID}] = true
			}
		}
	}
	// Premium refund: decide early in the following year.
	if today.Time().Month() <= 6 {
		y := today.Year() - 1
		for _, p := range set.Payers {
			if p.Kind == model.PKV && heldYears[[2]any{y, p.ID}] && set.Year(y, p.ID).ExpectedPremiumRefund > 0 {
				add(ReminderDTO{Kind: "premium", Text: fmt.Sprintf("%s %d: entscheiden, ob die zurückgehaltenen Rechnungen eingereicht werden (Vergleich im Dashboard %d)", p.Name, y, y), Year: y})
			}
		}
	}
	if files, err := st.ListInbox(); err == nil && len(files) > 0 {
		add(ReminderDTO{Kind: "inbox", Text: fmt.Sprintf("%d %s im Posteingang", len(files), map[bool]string{true: "Beleg", false: "Belege"}[len(files) == 1])})
	}
	slices.SortStableFunc(out, func(a, b ReminderDTO) int {
		switch {
		case a.Urgent && !b.Urgent:
			return -1
		case !a.Urgent && b.Urgent:
			return 1
		}
		return 0
	})
	if out == nil {
		out = []ReminderDTO{}
	}
	return out, nil
}
