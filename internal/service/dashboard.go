package service

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/calc"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/store"
)

// BucketDTO sums claims in one status.
type BucketDTO struct {
	Count    int    `json:"count"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

// PremiumRefundDTO compares claiming from the PKV with keeping the BRE.
type PremiumRefundDTO struct {
	Expected         string `json:"expected"`
	ClaimAll         string `json:"claimAll"`
	ClaimNeutralOnly string `json:"claimNeutralOnly"`
	HoldTotal        string `json:"holdTotal"`
	Advantage        string `json:"advantage"`
	Forfeited        bool   `json:"forfeited"`
}

// PayerYearDTO holds one payer's figures of a year.
type PayerYearDTO struct {
	PayerID         string            `json:"payerId"`
	PayerName       string            `json:"payerName"`
	Kind            string            `json:"kind"`
	Claims          int               `json:"claims"`
	Expected        string            `json:"expected"`
	Actual          string            `json:"actual"`
	Refunded        BucketDTO         `json:"refunded"`
	Submitted       BucketDTO         `json:"submitted"`
	Open            BucketDTO         `json:"open"`
	Held            BucketDTO         `json:"held"`
	Skipped         BucketDTO         `json:"skipped"`
	Deviation       string            `json:"deviation"`
	Clarify         int               `json:"clarify"`
	SuggestClarify  int               `json:"suggestClarify"`
	DeadlineSoon    int               `json:"deadlineSoon"`
	DeadlineMissed  int               `json:"deadlineMissed"`
	OpenBilled      string            `json:"openBilled"`
	MinClaimAmount  string            `json:"minClaimAmount"`
	AnnualDeduction string            `json:"annualDeduction"`
	Deductible      string            `json:"deductible"`
	PremiumRefund   *PremiumRefundDTO `json:"premiumRefund"`
	Setting         YearSettingDTO    `json:"setting"` // editable per-year values
}

// YearDTO holds the figures of one Leistungsjahr (Year 0 = total).
type YearDTO struct {
	Year            int            `json:"year"`
	Invoices        int            `json:"invoices"`
	Billed          string         `json:"billed"`
	Expected        string         `json:"expected"`
	Actual          string         `json:"actual"`
	OwnShare        string         `json:"ownShare"`
	OwnShareSettled string         `json:"ownShareSettled"`
	Allowances      string         `json:"allowances"`
	Payers          []PayerYearDTO `json:"payers"`
}

// OverviewDTO is the overview of all years.
type OverviewDTO struct {
	Years []YearDTO `json:"years"` // newest first
	Total YearDTO   `json:"total"`
}

func bucketDTO(b calc.Bucket) BucketDTO {
	return BucketDTO{Count: b.Count, Expected: b.Expected.String(), Actual: b.Actual.String()}
}

func yearDTO(set *model.Settings, y calc.Year) YearDTO {
	d := YearDTO{Year: y.Year, Invoices: y.Invoices, Billed: y.Billed.String(), Expected: y.Expected.String(),
		Actual: y.Actual.String(), OwnShare: y.OwnShare.String(), OwnShareSettled: y.OwnShareSettled.String(),
		Allowances: y.Allowances.String(), Payers: []PayerYearDTO{}}
	for _, p := range y.Payers {
		pd := PayerYearDTO{
			PayerID: p.PayerID, PayerName: p.PayerName, Kind: string(p.Kind), Claims: p.Claims,
			Expected: p.Expected.String(), Actual: p.Actual.String(), Refunded: bucketDTO(p.Refunded),
			Submitted: bucketDTO(p.Submitted), Open: bucketDTO(p.Open), Held: bucketDTO(p.Held),
			Skipped: bucketDTO(p.Skipped), Deviation: p.Deviation.String(), Clarify: p.Clarify,
			SuggestClarify: p.SuggestClarify, DeadlineSoon: p.DeadlineSoon, DeadlineMissed: p.DeadlineMissed,
			OpenBilled: p.OpenBilled.String(), MinClaimAmount: p.MinClaimAmount.String(),
			AnnualDeduction: p.AnnualDeduction.String(), Deductible: p.Deductible.String(),
		}
		if y.Year != 0 {
			pd.Setting = yearSettingDTO(set.Year(y.Year, p.PayerID))
		}
		if pr := p.PremiumRefund; pr != nil {
			pd.PremiumRefund = &PremiumRefundDTO{Expected: pr.Expected.String(), ClaimAll: pr.ClaimAll.String(),
				ClaimNeutralOnly: pr.ClaimNeutralOnly.String(), HoldTotal: pr.HoldTotal.String(),
				Advantage: pr.Advantage.String(), Forfeited: pr.Forfeited}
		}
		d.Payers = append(d.Payers, pd)
	}
	return d
}

// payersOfYear returns the payers whose coverage touches the year.
func payersOfYear(set *model.Settings, year int) []string {
	var ids []string
	addShares := func(shares []model.Share) {
		for _, sh := range shares {
			if !slices.Contains(ids, sh.PayerID) {
				ids = append(ids, sh.PayerID)
			}
		}
	}
	addShares(set.SharesAt(civil.New(year, 1, 1)))
	for _, c := range set.Coverage {
		if c.From.Year() == year {
			addShares(c.Shares)
		}
	}
	return ids
}

// withCoveragePayers adds empty entries for payers that cover the year but have no claims yet.
func withCoveragePayers(set *model.Settings, y calc.Year) calc.Year {
	for _, id := range payersOfYear(set, y.Year) {
		if slices.ContainsFunc(y.Payers, func(p calc.PayerYear) bool { return p.PayerID == id }) {
			continue
		}
		p, _ := set.Payer(id)
		y.Payers = append(y.Payers, calc.PayerYear{PayerID: id, PayerName: p.Name, Kind: p.Kind,
			MinClaimAmount: p.MinClaimAmount, AnnualDeduction: set.Year(y.Year, id).AnnualDeduction,
			Deductible: set.Year(y.Year, id).Deductible})
	}
	pos := func(id string) int {
		for i, p := range set.Payers {
			if p.ID == id {
				return i
			}
		}
		return len(set.Payers)
	}
	slices.SortStableFunc(y.Payers, func(a, b calc.PayerYear) int { return pos(a.PayerID) - pos(b.PayerID) })
	return y
}

func (s *Service) years(data *store.Data) []calc.Year {
	return calc.Years(&data.Settings, data.Invoices, data.Allowances, s.today())
}

// Dashboard returns the figures of one Leistungsjahr.
func (s *Service) Dashboard(year int) (YearDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return YearDTO{}, err
	}
	y := calc.Year{Year: year}
	for _, yr := range s.years(data) {
		if yr.Year == year {
			y = yr
		}
	}
	return yearDTO(&data.Settings, withCoveragePayers(&data.Settings, y)), nil
}

// Overview returns all years (newest first) and their total.
func (s *Service) Overview() (OverviewDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return OverviewDTO{}, err
	}
	years := s.years(data)
	out := OverviewDTO{Years: []YearDTO{}, Total: yearDTO(&data.Settings, calc.Total(&data.Settings, years))}
	for i := len(years) - 1; i >= 0; i-- {
		out.Years = append(out.Years, yearDTO(&data.Settings, years[i]))
	}
	return out, nil
}

// AllowanceDTO is a benefit without an invoice (e.g. Krankenhaustagegeld).
type AllowanceDTO struct {
	ID       string  `json:"id"`
	PayerID  string  `json:"payerId"`
	Kind     string  `json:"kind"`
	Date     string  `json:"date"`
	Amount   string  `json:"amount"`
	Received MarkDTO `json:"received"`
	Note     string  `json:"note"`
}

func allowanceDTO(a model.Allowance) AllowanceDTO {
	return AllowanceDTO{ID: a.ID, PayerID: a.PayerID, Kind: a.Kind, Date: a.Date.String(), Amount: a.Amount.String(),
		Received: markDTO(a.Received), Note: a.Note}
}

// ListAllowances returns the allowances of a year (0 = all).
func (s *Service) ListAllowances(year int) ([]AllowanceDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return nil, err
	}
	out := []AllowanceDTO{}
	for _, a := range data.Allowances {
		if !a.Deleted && (year == 0 || a.Date.Year() == year) {
			out = append(out, allowanceDTO(a))
		}
	}
	slices.SortFunc(out, func(a, b AllowanceDTO) int { return strings.Compare(a.Date, b.Date) })
	return out, nil
}

// SaveAllowance creates or updates an allowance.
func (s *Service) SaveAllowance(d AllowanceDTO) (AllowanceDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, data, err := s.ready()
	if err != nil {
		return AllowanceDTO{}, err
	}
	date, err := parseDate("Datum", d.Date)
	if err != nil {
		return AllowanceDTO{}, err
	}
	amount, err := parseAmount("Betrag", d.Amount)
	if err != nil {
		return AllowanceDTO{}, err
	}
	received, err := parseMark("Erhalten", d.Received)
	if err != nil {
		return AllowanceDTO{}, err
	}
	if date.IsZero() || amount <= 0 {
		return AllowanceDTO{}, fmt.Errorf("Datum und Betrag sind nötig")
	}
	if _, ok := data.Settings.Payer(d.PayerID); !ok {
		return AllowanceDTO{}, fmt.Errorf("unbekannter Kostenträger %q", d.PayerID)
	}
	kind := strings.TrimSpace(d.Kind)
	if kind == "" {
		kind = model.AllowanceHospitalDay
	}
	a := model.Allowance{ID: d.ID, PayerID: d.PayerID, Kind: kind, Date: date, Amount: amount, Received: received,
		Note: strings.TrimSpace(d.Note)}
	if a.ID == "" {
		a.ID = s.newID()
	}
	saved, err := st.SaveAllowance(a)
	if err != nil {
		return AllowanceDTO{}, fmt.Errorf("Speichern fehlgeschlagen: %w", err)
	}
	replaced := false
	for i := range data.Allowances {
		if data.Allowances[i].ID == saved.ID {
			data.Allowances[i], replaced = saved, true
		}
	}
	if !replaced {
		data.Allowances = append(data.Allowances, saved)
	}
	return allowanceDTO(saved), nil
}

// DeleteAllowance marks an allowance as deleted.
func (s *Service) DeleteAllowance(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, data, err := s.ready()
	if err != nil {
		return err
	}
	for i := range data.Allowances {
		if a := data.Allowances[i]; a.ID == id && !a.Deleted {
			a.Deleted = true
			saved, err := st.SaveAllowance(a)
			if err != nil {
				return fmt.Errorf("Löschen fehlgeschlagen: %w", err)
			}
			data.Allowances[i] = saved
			return nil
		}
	}
	return fmt.Errorf("Eintrag %s nicht gefunden", id)
}
