package calc

import (
	"slices"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// Bucket sums claims in one status.
type Bucket struct {
	Count    int         `json:"count"`
	Expected money.Cents `json:"expected"`
	Actual   money.Cents `json:"actual"`
}

func (b *Bucket) add(cv *ClaimView) {
	b.Count++
	b.Expected += cv.Expected
	b.Actual += cv.Actual
}

func (b *Bucket) plus(o Bucket) {
	b.Count += o.Count
	b.Expected += o.Expected
	b.Actual += o.Actual
}

// PremiumRefund compares claiming everything from the PKV with keeping the
// Beitragsrückerstattung (BRE) for one year.
type PremiumRefund struct {
	Expected         money.Cents `json:"expected"`         // BRE if nothing relevant is claimed
	ClaimAll         money.Cents `json:"claimAll"`         // SOLL of all claims minus deductible
	ClaimNeutralOnly money.Cents `json:"claimNeutralOnly"` // SOLL of claims that keep the BRE minus deductible
	HoldTotal        money.Cents `json:"holdTotal"`        // ClaimNeutralOnly + Expected
	Advantage        money.Cents `json:"advantage"`        // ClaimAll − HoldTotal; > 0: claiming pays off
	Forfeited        bool        `json:"forfeited"`        // a relevant claim was already submitted or refunded
}

// Outstanding is SOLL − IST of a payer's claims that are submitted or still
// to be submitted, plus the deviations of refunded claims (until they are
// accepted). Claims held back or marked "wird nicht eingereicht" are not
// owed and left out.
type Outstanding struct {
	Expected  money.Cents // SOLL of those claims
	Actual    money.Cents // IST of those claims
	Total     money.Cents // Expected − Actual
	Submitted money.Cents // submitted, refund still open
	Open      money.Cents // not submitted yet
	Refunded  money.Cents // refunded claims: SOLL − IST (deviations)
}

// Outstanding returns what the payer still owes according to SOLL.
func (p PayerYear) Outstanding() Outstanding {
	o := Outstanding{
		Expected:  p.Refunded.Expected + p.Submitted.Expected + p.Open.Expected,
		Actual:    p.Refunded.Actual + p.Submitted.Actual + p.Open.Actual,
		Submitted: p.Submitted.Expected - p.Submitted.Actual,
		Open:      p.Open.Expected - p.Open.Actual,
		Refunded:  p.Refunded.Expected - p.Refunded.Actual,
	}
	o.Total = o.Expected - o.Actual
	return o
}

// PayerYear holds one payer's figures for one Leistungsjahr.
type PayerYear struct {
	PayerID   string          `json:"payerId"`
	PayerName string          `json:"payerName"`
	Kind      model.PayerKind `json:"kind"`

	Claims   int         `json:"claims"`
	Expected money.Cents `json:"expected"` // SOLL of all claims: what the payer would have to pay
	Actual   money.Cents `json:"actual"`   // IST

	Refunded  Bucket `json:"refunded"`
	Submitted Bucket `json:"submitted"`
	Open      Bucket `json:"open"`
	Held      Bucket `json:"held"`
	Skipped   Bucket `json:"skipped"`

	Deviation      money.Cents `json:"deviation"` // Σ (IST − SOLL) of refunded claims
	Clarify        int         `json:"clarify"`
	SuggestClarify int         `json:"suggestClarify"`
	DeadlineSoon   int         `json:"deadlineSoon"`
	DeadlineMissed int         `json:"deadlineMissed"`

	OpenBilled      money.Cents `json:"openBilled"` // invoice amounts not yet claimed (compare MinClaimAmount)
	MinClaimAmount  money.Cents `json:"minClaimAmount"`
	AnnualDeduction money.Cents `json:"annualDeduction"`
	Deductible      money.Cents `json:"deductible"`

	PremiumRefund *PremiumRefund `json:"premiumRefund,omitempty"` // PKV with an expected BRE only
}

// Year holds the figures of one Leistungsjahr.
type Year struct {
	Year            int         `json:"year"` // 0 in totals and for invoices without any date
	Invoices        int         `json:"invoices"`
	Billed          money.Cents `json:"billed"`
	Expected        money.Cents `json:"expected"`
	Actual          money.Cents `json:"actual"`
	OwnShare        money.Cents `json:"ownShare"`        // billed − IST
	OwnShareSettled money.Cents `json:"ownShareSettled"` // amount − IST of settled invoices only
	Allowances      money.Cents `json:"allowances"`      // e.g. Krankenhaustagegeld
	Payers          []PayerYear `json:"payers"`
}

type pkvAcc struct {
	neutral   money.Cents
	forfeited bool
}

// Years computes the figures for every Leistungsjahr that has invoices or allowances, oldest first.
func Years(s *model.Settings, invoices []model.Invoice, allowances []model.Allowance, today civil.Date) []Year {
	years := map[int]*Year{}
	payers := map[int]map[string]*PayerYear{}
	acc := map[int]map[string]*pkvAcc{}
	year := func(y int) *Year {
		if years[y] == nil {
			years[y] = &Year{Year: y}
			payers[y] = map[string]*PayerYear{}
			acc[y] = map[string]*pkvAcc{}
		}
		return years[y]
	}
	for i := range invoices {
		inv := &invoices[i]
		if inv.Deleted {
			continue
		}
		v := Invoice(s, inv, today)
		y := year(v.Year)
		y.Invoices++
		y.Billed += inv.Amount
		y.Expected += v.Expected
		y.Actual += v.Actual
		if v.Settled {
			y.OwnShareSettled += v.OwnShare
		}
		for ci := range v.Claims {
			cv := &v.Claims[ci]
			py := payers[v.Year][cv.PayerID]
			if py == nil {
				py = &PayerYear{PayerID: cv.PayerID, PayerName: cv.PayerName, Kind: cv.Kind}
				payers[v.Year][cv.PayerID] = py
				acc[v.Year][cv.PayerID] = &pkvAcc{}
			}
			a := acc[v.Year][cv.PayerID]
			addClaim(py, a, cv, inv.Amount)
		}
	}
	for _, al := range allowances {
		if !al.Deleted {
			year(al.Date.Year()).Allowances += al.Amount
		}
	}
	out := make([]Year, 0, len(years))
	for y, yr := range years {
		yr.OwnShare = yr.Billed - yr.Actual
		for id, py := range payers[y] {
			p, _ := s.Payer(id)
			ys := s.Year(y, id)
			py.MinClaimAmount = p.MinClaimAmount
			py.AnnualDeduction = ys.AnnualDeduction
			py.Deductible = ys.Deductible
			if py.Kind == model.PKV && ys.ExpectedPremiumRefund > 0 {
				py.PremiumRefund = premiumRefund(py, acc[y][id], ys.ExpectedPremiumRefund)
			}
			yr.Payers = append(yr.Payers, *py)
		}
		sortPayers(s, yr.Payers)
		out = append(out, *yr)
	}
	slices.SortFunc(out, func(a, b Year) int { return a.Year - b.Year })
	return out
}

func addClaim(py *PayerYear, a *pkvAcc, cv *ClaimView, billed money.Cents) {
	py.Claims++
	py.Expected += cv.Expected
	py.Actual += cv.Actual
	switch cv.Status {
	case StatusRefunded:
		py.Refunded.add(cv)
		py.Deviation += cv.Diff
	case StatusSubmitted:
		py.Submitted.add(cv)
	case StatusOpen:
		py.Open.add(cv)
		py.OpenBilled += billed
	case StatusHeld:
		py.Held.add(cv)
	case StatusSkipped:
		py.Skipped.add(cv)
	}
	if cv.Clarify.Done() {
		py.Clarify++
	}
	if cv.SuggestClarify {
		py.SuggestClarify++
	}
	if !cv.Deadline.IsZero() {
		switch {
		case cv.DeadlineDays < 0:
			py.DeadlineMissed++
		case cv.DeadlineDays <= DeadlineWarnDays:
			py.DeadlineSoon++
		}
	}
	if cv.KeepsPremiumRefund {
		a.neutral += cv.Expected
	} else if cv.Status == StatusSubmitted || cv.Status == StatusRefunded {
		a.forfeited = true
	}
}

func premiumRefund(py *PayerYear, a *pkvAcc, expected money.Cents) *PremiumRefund {
	all := max(0, py.Expected-py.Deductible)
	neutral := max(0, a.neutral-py.Deductible)
	hold := neutral + expected
	return &PremiumRefund{
		Expected: expected, ClaimAll: all, ClaimNeutralOnly: neutral,
		HoldTotal: hold, Advantage: all - hold, Forfeited: a.forfeited,
	}
}

// sortPayers orders payers as in the settings; unknown payers go last, by ID.
func sortPayers(s *model.Settings, ps []PayerYear) {
	pos := func(id string) int {
		for i, p := range s.Payers {
			if p.ID == id {
				return i
			}
		}
		return len(s.Payers)
	}
	slices.SortFunc(ps, func(a, b PayerYear) int {
		if d := pos(a.PayerID) - pos(b.PayerID); d != 0 {
			return d
		}
		if a.PayerID < b.PayerID {
			return -1
		}
		if a.PayerID > b.PayerID {
			return 1
		}
		return 0
	})
}

// Total sums several years (e.g. for the overview of all years). Year is 0.
// Amounts that result from per-year settings – the annual deduction and the
// deductible – are summed as well; the BRE comparison is per year and stays
// empty.
func Total(s *model.Settings, years []Year) Year {
	t := Year{}
	byID := map[string]*PayerYear{}
	var order []string
	for _, y := range years {
		t.Invoices += y.Invoices
		t.Billed += y.Billed
		t.Expected += y.Expected
		t.Actual += y.Actual
		t.OwnShare += y.OwnShare
		t.OwnShareSettled += y.OwnShareSettled
		t.Allowances += y.Allowances
		for _, p := range y.Payers {
			tp := byID[p.PayerID]
			if tp == nil {
				tp = &PayerYear{PayerID: p.PayerID, PayerName: p.PayerName, Kind: p.Kind}
				byID[p.PayerID] = tp
				order = append(order, p.PayerID)
			}
			tp.Claims += p.Claims
			tp.Expected += p.Expected
			tp.Actual += p.Actual
			tp.Refunded.plus(p.Refunded)
			tp.Submitted.plus(p.Submitted)
			tp.Open.plus(p.Open)
			tp.Held.plus(p.Held)
			tp.Skipped.plus(p.Skipped)
			tp.Deviation += p.Deviation
			tp.Clarify += p.Clarify
			tp.SuggestClarify += p.SuggestClarify
			tp.DeadlineSoon += p.DeadlineSoon
			tp.DeadlineMissed += p.DeadlineMissed
			tp.OpenBilled += p.OpenBilled
			tp.AnnualDeduction += p.AnnualDeduction
			tp.Deductible += p.Deductible
		}
	}
	for _, id := range order {
		t.Payers = append(t.Payers, *byID[id])
	}
	sortPayers(s, t.Payers)
	return t
}
