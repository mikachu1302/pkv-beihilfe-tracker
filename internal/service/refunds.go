package service

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/calc"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/extract"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/match"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/store"
)

// LayoutDTO says which amount of a position line means what (see extract.Layout).
type LayoutDTO struct {
	Billed   int `json:"billed"`
	Eligible int `json:"eligible"`
	Refund   int `json:"refund"`
}

// CandidateDTO is an invoice a refund line could belong to.
type CandidateDTO struct {
	InvoiceID string   `json:"invoiceId"`
	Label     string   `json:"label"` // "Dr. Muster · 03.03.2026 · 286,40 €"
	Score     int      `json:"score"`
	Exact     bool     `json:"exact"`
	Reasons   []string `json:"reasons"`
}

// RefundLineDTO is one position of a refund document in the review.
type RefundLineDTO struct {
	ID       string   `json:"id"`
	Date     string   `json:"date"`
	Provider string   `json:"provider"`
	Amounts  []string `json:"amounts"` // all amounts read on the line
	Billed   string   `json:"billed"`
	Eligible string   `json:"eligible"`
	Refund   string   `json:"refund"`
	// Edited: amounts were changed by hand, the layout is not applied again.
	Edited     bool           `json:"edited"`
	InvoiceIDs []string       `json:"invoiceIds"`
	Match      string         `json:"match"` // auto, suggest, none, confirmed (chosen by the user)
	Score      int            `json:"score"`
	Candidates []CandidateDTO `json:"candidates"`
}

// DeductionDTO is an amount the document subtracts from the sum of its lines.
type DeductionDTO struct {
	Label  string `json:"label"`
	Amount string `json:"amount"`
}

// RefundReviewDTO is a refund document being checked before it is applied.
type RefundReviewDTO struct {
	PayerID         string          `json:"payerId"`
	Date            string          `json:"date"`
	ApplicationDate string          `json:"applicationDate"`
	PayoutDate      string          `json:"payoutDate"`
	PaidOut         string          `json:"paidOut"`
	Deductions      []DeductionDTO  `json:"deductions"`
	Lines           []RefundLineDTO `json:"lines"`
	Layout          LayoutDTO       `json:"layout"`
	LinesTotal      string          `json:"linesTotal"`
	ChecksumOK      bool            `json:"checksumOk"`
	ChecksumDiff    string          `json:"checksumDiff"` // lines − deductions − payout
	Notes           []string        `json:"notes"`
	// Invoices of this payer that are not refunded yet (for choosing by hand).
	Invoices []CandidateDTO `json:"invoices"`
}

// RefundConfirmDTO reports what ConfirmRefund changed.
type RefundConfirmDTO struct {
	DocID    string `json:"docId"`
	Invoices int    `json:"invoices"`
	Aliases  int    `json:"aliases"`
}

func invoiceLabel(inv *model.Invoice) string {
	return fmt.Sprintf("%s · %s · %s €", inv.Provider, inv.ServiceDate().DE(), inv.Amount.DE())
}

// guessPayer finds the payer named on the document, else one of the right kind.
func guessPayer(set *model.Settings, text string) string {
	norm := func(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }
	t := norm(text)
	for _, p := range set.Payers {
		if n := norm(p.Name); len(n) >= 4 && strings.Contains(t, n) {
			return p.ID
		}
	}
	want := model.PKV
	if strings.Contains(t, "beihilfe") {
		want = model.Beihilfe
	}
	for _, p := range set.Payers {
		if p.Kind == want {
			return p.ID
		}
	}
	if len(set.Payers) > 0 {
		return set.Payers[0].ID
	}
	return ""
}

// AnalyzeRefund reads a refund document and matches its lines to invoices.
func (s *Service) AnalyzeRefund(in AnalyzeInputDTO) (RefundReviewDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return RefundReviewDTO{}, err
	}
	d := extract.AnalyzeRefund(in.Lines, s.now())
	r := RefundReviewDTO{PayerID: guessPayer(&data.Settings, d.Text), Date: d.Date.String(),
		ApplicationDate: d.ApplicationDate.String(), PayoutDate: d.PayoutDate.String(), Notes: d.Notes}
	if d.PaidOut != 0 {
		r.PaidOut = d.PaidOut.String()
	}
	for _, x := range d.Deductions {
		r.Deductions = append(r.Deductions, DeductionDTO{Label: x.Label, Amount: x.Amount.String()})
	}
	layout := extract.GuessLayout(d.Positions)
	if l, ok := data.Settings.RefundLayoutFor(r.PayerID); ok {
		layout = extract.Layout{Billed: l.Billed, Eligible: l.Eligible, Refund: l.Refund}
	}
	r.Layout = LayoutDTO(layout)
	for i, p := range d.Positions {
		line := RefundLineDTO{ID: fmt.Sprintf("p%d", i+1), Date: p.Date.String(), Provider: p.Provider}
		for _, a := range p.Amounts {
			line.Amounts = append(line.Amounts, a.String())
		}
		r.Lines = append(r.Lines, line)
	}
	return s.review(data, r)
}

// ReviewRefund recomputes amounts, matches and the checksum after changes in the review.
func (s *Service) ReviewRefund(r RefundReviewDTO) (RefundReviewDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return RefundReviewDTO{}, err
	}
	return s.review(data, r)
}

type parsedLine struct {
	date                     civil.Date
	billed, eligible, refund money.Cents
}

func parseRefundLine(l RefundLineDTO) (parsedLine, error) {
	var p parsedLine
	var err error
	if p.date, err = parseDate("Datum", l.Date); err != nil {
		return p, err
	}
	if p.billed, err = parseAmount("Rechnungsbetrag", l.Billed); err != nil {
		return p, err
	}
	if p.eligible, err = parseAmount("erstattungsfähig", l.Eligible); err != nil {
		return p, err
	}
	p.refund, err = parseAmount("Erstattung", l.Refund)
	return p, err
}

func (s *Service) review(data *store.Data, r RefundReviewDTO) (RefundReviewDTO, error) {
	set := &data.Settings
	if _, ok := set.Payer(r.PayerID); !ok {
		return r, fmt.Errorf("unbekannter Kostenträger %q", r.PayerID)
	}
	layout := extract.Layout(r.Layout)
	var lines []match.Line
	var fixedInvoices []string
	var total money.Cents
	for i := range r.Lines {
		l := &r.Lines[i]
		if l.ID == "" {
			l.ID = fmt.Sprintf("m%d", i+1)
		}
		if !l.Edited && len(l.Amounts) > 0 {
			var amounts []money.Cents
			for _, a := range l.Amounts {
				v, err := money.Parse(a)
				if err != nil {
					return r, fmt.Errorf("Betrag „%s“ ist ungültig", a)
				}
				amounts = append(amounts, v)
			}
			b, e, rf := layout.Apply(amounts)
			l.Billed, l.Eligible, l.Refund = amountOrEmpty(b), amountOrEmpty(e), amountOrEmpty(rf)
		}
		p, err := parseRefundLine(*l)
		if err != nil {
			return r, fmt.Errorf("Zeile %d: %w", i+1, err)
		}
		total += p.refund
		if l.Match == model.MatchConfirmed {
			fixedInvoices = append(fixedInvoices, l.InvoiceIDs...)
			continue
		}
		lines = append(lines, match.Line{ID: l.ID, Date: p.date, Provider: l.Provider, Billed: p.billed, Refund: p.refund})
	}

	var cands []model.Invoice
	for _, inv := range data.Invoices {
		if !slices.Contains(fixedInvoices, inv.ID) {
			cands = append(cands, inv)
		}
	}
	byID := map[string]*model.Invoice{}
	for i := range data.Invoices {
		byID[data.Invoices[i].ID] = &data.Invoices[i]
	}
	results := match.Match(lines, cands, set, match.Options{PayerID: r.PayerID})
	res := map[string]match.Result{}
	for _, m := range results {
		res[m.LineID] = m
	}
	for i := range r.Lines {
		l := &r.Lines[i]
		m, ok := res[l.ID]
		if !ok {
			continue // confirmed by the user
		}
		l.InvoiceIDs, l.Match, l.Score = m.InvoiceIDs, string(m.Confidence), m.Score
		l.Candidates = []CandidateDTO{}
		for _, c := range m.Candidates {
			if inv := byID[c.InvoiceID]; inv != nil {
				l.Candidates = append(l.Candidates, CandidateDTO{InvoiceID: c.InvoiceID, Label: invoiceLabel(inv), Score: c.Score,
					Exact: c.Exact, Reasons: c.Reasons})
			}
		}
	}

	// Checksum: lines − deductions = payout.
	var ded money.Cents
	for _, d := range r.Deductions {
		v, err := parseAmount("Abzug", d.Amount)
		if err != nil {
			return r, err
		}
		ded += v.Abs()
	}
	paid, err := parseAmount("Auszahlung", r.PaidOut)
	if err != nil {
		return r, err
	}
	r.LinesTotal = total.String()
	r.ChecksumDiff = (total - ded - paid).String()
	r.ChecksumOK = paid != 0 && total-ded == paid

	// Invoices still open towards this payer, newest first, for choosing by hand.
	r.Invoices = []CandidateDTO{}
	for i := len(data.Invoices) - 1; i >= 0 && len(r.Invoices) < 300; i-- {
		inv := &data.Invoices[i]
		if inv.Deleted || !slices.Contains(inv.PayerIDs(set), r.PayerID) {
			continue
		}
		if st := calc.ClaimStatus(inv.Claims[r.PayerID]); st == calc.StatusRefunded || st == calc.StatusSkipped {
			continue
		}
		r.Invoices = append(r.Invoices, CandidateDTO{InvoiceID: inv.ID, Label: invoiceLabel(inv)})
	}
	if r.Notes == nil {
		r.Notes = []string{}
	}
	if r.Deductions == nil {
		r.Deductions = []DeductionDTO{}
	}
	return r, nil
}

// ConfirmRefund stores the refund document and books its lines as refunds on
// the assigned invoices. inboxName (may be empty) is the scanned document,
// which moves to the documents folder. With saveLayout the column layout is
// remembered for this payer.
func (s *Service) ConfirmRefund(inboxName string, r RefundReviewDTO, saveLayout bool) (RefundConfirmDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out RefundConfirmDTO
	st, data, err := s.ready()
	if err != nil {
		return out, err
	}
	set := &data.Settings
	if _, ok := set.Payer(r.PayerID); !ok {
		return out, fmt.Errorf("unbekannter Kostenträger %q", r.PayerID)
	}
	doc := model.RefundDoc{ID: s.newID(), PayerID: r.PayerID, State: model.DocConfirmed}
	if doc.Date, err = parseDate("Datum des Bescheids", r.Date); err != nil {
		return out, err
	}
	if doc.ApplicationDate, err = parseDate("Antrag vom", r.ApplicationDate); err != nil {
		return out, err
	}
	if doc.PayoutDate, err = parseDate("Auszahlung am", r.PayoutDate); err != nil {
		return out, err
	}
	if doc.PaidOut, err = parseAmount("Auszahlung", r.PaidOut); err != nil {
		return out, err
	}
	if doc.Date.IsZero() {
		doc.Date = s.today()
	}
	for _, d := range r.Deductions {
		v, err := parseAmount("Abzug", d.Amount)
		if err != nil {
			return out, err
		}
		doc.Deductions = append(doc.Deductions, model.Deduction{Label: strings.TrimSpace(d.Label), Amount: v.Abs()})
	}
	refundDate := doc.PayoutDate
	if refundDate.IsZero() {
		refundDate = doc.Date
	}

	changed := map[string]*model.Invoice{}
	get := func(id string) (*model.Invoice, error) {
		if inv, ok := changed[id]; ok {
			return inv, nil
		}
		cur, err := findInvoice(data, id)
		if err != nil {
			return nil, err
		}
		inv := cloneInvoice(cur)
		changed[id] = &inv
		return &inv, nil
	}
	known := map[string]bool{}
	for _, a := range set.Aliases {
		known[strings.ToLower(a.From)] = true
	}
	var newAliases []model.Alias
	for i, l := range r.Lines {
		p, err := parseRefundLine(l)
		if err != nil {
			return out, fmt.Errorf("Zeile %d: %w", i+1, err)
		}
		line := model.RefundLine{ID: l.ID, Date: p.date, Provider: strings.TrimSpace(l.Provider), Billed: p.billed,
			Eligible: p.eligible, Refund: p.refund, InvoiceIDs: l.InvoiceIDs, Match: model.MatchNone}
		if line.ID == "" {
			line.ID = fmt.Sprintf("m%d", i+1)
		}
		if len(l.InvoiceIDs) > 0 {
			line.Match = model.MatchConfirmed
			if err := s.bookLine(get, line, doc, refundDate); err != nil {
				return out, err
			}
			if len(l.InvoiceIDs) == 1 && line.Provider != "" && !known[strings.ToLower(line.Provider)] {
				inv, _ := get(l.InvoiceIDs[0])
				if !strings.EqualFold(inv.Provider, line.Provider) && !strings.EqualFold(inv.Payee, line.Provider) {
					newAliases = append(newAliases, model.Alias{From: line.Provider, To: inv.Provider})
					known[strings.ToLower(line.Provider)] = true
				}
			}
		}
		doc.Lines = append(doc.Lines, line)
	}

	if inboxName != "" {
		b, err := os.ReadFile(st.InboxPath(inboxName))
		if err != nil {
			return out, fmt.Errorf("Datei im Posteingang nicht gefunden: %w", err)
		}
		if doc.Attachment, err = st.AddDocument(bytes.NewReader(b), strings.ToLower(filepath.Ext(inboxName))); err != nil {
			return out, fmt.Errorf("Beleg konnte nicht abgelegt werden: %w", err)
		}
	}
	saved, err := st.SaveRefundDoc(doc)
	if err != nil {
		return out, fmt.Errorf("Speichern fehlgeschlagen: %w", err)
	}
	data.RefundDocs = append(data.RefundDocs, saved)
	out.DocID = saved.ID
	ids := make([]string, 0, len(changed))
	for id := range changed {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		inv, err := st.SaveInvoice(*changed[id])
		if err != nil {
			return out, fmt.Errorf("Speichern fehlgeschlagen: %w", err)
		}
		replaceInvoice(data, inv)
		out.Invoices++
	}
	if len(newAliases) > 0 || saveLayout {
		newSet := *set
		newSet.Aliases = append(slices.Clone(set.Aliases), newAliases...)
		if saveLayout {
			l := model.RefundLayout{PayerID: r.PayerID, Billed: r.Layout.Billed, Eligible: r.Layout.Eligible, Refund: r.Layout.Refund}
			newSet.RefundLayouts = slices.DeleteFunc(slices.Clone(set.RefundLayouts), func(x model.RefundLayout) bool { return x.PayerID == r.PayerID })
			newSet.RefundLayouts = append(newSet.RefundLayouts, l)
		}
		savedSet, err := st.SaveSettings(newSet)
		if err != nil {
			return out, fmt.Errorf("Einstellungen konnten nicht gespeichert werden: %w", err)
		}
		data.Settings = savedSet
		out.Aliases = len(newAliases)
	}
	if inboxName != "" {
		if err := st.RemoveFromInbox(inboxName); err != nil {
			return out, err
		}
	}
	return out, nil
}

// bookLine records the refund of one line on its invoice(s). Several invoices
// share the refund in proportion to their amounts. Booking the same line again
// replaces the earlier booking.
func (s *Service) bookLine(get func(string) (*model.Invoice, error), line model.RefundLine, doc model.RefundDoc, date civil.Date) error {
	invs := make([]*model.Invoice, 0, len(line.InvoiceIDs))
	var sum money.Cents
	for _, id := range line.InvoiceIDs {
		inv, err := get(id)
		if err != nil {
			return err
		}
		invs = append(invs, inv)
		sum += inv.Amount
	}
	if len(invs) == 0 {
		return errors.New("Zeile ohne Rechnung")
	}
	rest := line.Refund
	for i, inv := range invs {
		share := rest
		if i < len(invs)-1 && sum > 0 {
			share = money.Cents(int64(line.Refund) * int64(inv.Amount) / int64(sum))
			rest -= share
		}
		c := inv.Claims[doc.PayerID]
		c.Refunds = slices.DeleteFunc(c.Refunds, func(r model.Refund) bool { return r.DocID == doc.ID && r.LineID == line.ID })
		c.Refunds = append(c.Refunds, model.Refund{ID: s.newID(), Amount: share, Date: date, DocID: doc.ID, LineID: line.ID})
		c.Refunded = model.Mark{Date: date}
		if !c.Submitted.Done() && !doc.ApplicationDate.IsZero() {
			c.Submitted = model.Mark{Date: doc.ApplicationDate}
		}
		c.Held = false
		if inv.Claims == nil {
			inv.Claims = map[string]model.Claim{}
		}
		inv.Claims[doc.PayerID] = c
	}
	return nil
}

// RefundDocPath returns the file of a refund document (to open it).
func (s *Service) RefundDocPath(docID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, data, err := s.ready()
	if err != nil {
		return "", err
	}
	for _, d := range data.RefundDocs {
		if d.ID == docID && d.Attachment != "" {
			return st.DocumentPath(d.Attachment), nil
		}
	}
	return "", errors.New("zu diesem Bescheid ist keine Datei gespeichert")
}
