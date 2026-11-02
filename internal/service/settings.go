package service

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// PayerDTO is a Beihilfestelle or a PKV.
type PayerDTO struct {
	ID                 string `json:"id"`
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	ClaimDeadlineYears int    `json:"claimDeadlineYears"`
	MinClaimAmount     string `json:"minClaimAmount"`
	Note               string `json:"note"`
}

// ShareDTO is a payer's share in a coverage period.
type ShareDTO struct {
	PayerID string `json:"payerId"`
	Rate    string `json:"rate"`
}

// CoverageDTO is a coverage period.
type CoverageDTO struct {
	From   string     `json:"from"`
	Shares []ShareDTO `json:"shares"`
}

// YearSettingDTO holds per-year values of one payer.
type YearSettingDTO struct {
	Year                  int    `json:"year"`
	PayerID               string `json:"payerId"`
	Deductible            string `json:"deductible"`
	AnnualDeduction       string `json:"annualDeduction"`
	ExpectedPremiumRefund string `json:"expectedPremiumRefund"`
}

// AliasDTO maps a name on refund documents to a provider name.
type AliasDTO struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// SettingsDTO are the shared settings.
type SettingsDTO struct {
	Payers           []PayerDTO       `json:"payers"`
	Coverage         []CoverageDTO    `json:"coverage"`
	Years            []YearSettingDTO `json:"years"`
	Aliases          []AliasDTO       `json:"aliases"`
	ClarifyTolerance string           `json:"clarifyTolerance"`
}

// SettingsResultDTO is returned after saving: the stored settings and remaining warnings.
type SettingsResultDTO struct {
	Settings SettingsDTO  `json:"settings"`
	Problems []ProblemDTO `json:"problems"`
}

func settingsDTO(s *model.Settings) SettingsDTO {
	d := SettingsDTO{Payers: []PayerDTO{}, Coverage: []CoverageDTO{}, Years: []YearSettingDTO{}, Aliases: []AliasDTO{},
		ClarifyTolerance: s.Tolerance().String()}
	for _, p := range s.Payers {
		d.Payers = append(d.Payers, PayerDTO{ID: p.ID, Kind: string(p.Kind), Name: p.Name,
			ClaimDeadlineYears: p.ClaimDeadlineYears, MinClaimAmount: amountOrEmpty(p.MinClaimAmount), Note: p.Note})
	}
	for _, c := range s.Coverage {
		cd := CoverageDTO{From: c.From.String(), Shares: []ShareDTO{}}
		for _, sh := range c.Shares {
			cd.Shares = append(cd.Shares, ShareDTO{PayerID: sh.PayerID, Rate: sh.Rate.String()})
		}
		d.Coverage = append(d.Coverage, cd)
	}
	for _, y := range s.Years {
		d.Years = append(d.Years, yearSettingDTO(y))
	}
	for _, a := range s.Aliases {
		d.Aliases = append(d.Aliases, AliasDTO(a))
	}
	return d
}

func yearSettingDTO(y model.YearSetting) YearSettingDTO {
	return YearSettingDTO{Year: y.Year, PayerID: y.PayerID, Deductible: amountOrEmpty(y.Deductible),
		AnnualDeduction: amountOrEmpty(y.AnnualDeduction), ExpectedPremiumRefund: amountOrEmpty(y.ExpectedPremiumRefund)}
}

func amountOrEmpty(c money.Cents) string {
	if c == 0 {
		return ""
	}
	return c.String()
}

var nonID = regexp.MustCompile(`[^a-z0-9]+`)

// payerID derives a readable ID from a name ("Beihilfe Bund" → "beihilfe-bund").
func payerID(name string, taken map[string]bool) string {
	base := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss").Replace(strings.ToLower(name))
	base = strings.Trim(nonID.ReplaceAllString(base, "-"), "-")
	if base == "" {
		base = "kostentraeger"
	}
	id := base
	for i := 2; taken[id]; i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	taken[id] = true
	return id
}

func settingsFromDTO(d SettingsDTO) (model.Settings, error) {
	var errs []string
	add := func(err error) {
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	s := model.Settings{}
	taken := map[string]bool{}
	for _, p := range d.Payers {
		if p.ID != "" {
			taken[p.ID] = true
		}
	}
	for _, p := range d.Payers {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			errs = append(errs, "Kostenträger ohne Namen")
			continue
		}
		id := p.ID
		if id == "" {
			id = payerID(name, taken)
		}
		minAmount, err := parseAmount(name+" – Mindestbetrag", p.MinClaimAmount)
		add(err)
		if p.ClaimDeadlineYears < 0 {
			errs = append(errs, name+": Antragsfrist kann nicht negativ sein")
		}
		s.Payers = append(s.Payers, model.Payer{ID: id, Kind: model.PayerKind(p.Kind), Name: name,
			ClaimDeadlineYears: p.ClaimDeadlineYears, MinClaimAmount: minAmount, Note: strings.TrimSpace(p.Note)})
	}
	for _, c := range d.Coverage {
		from, err := parseDate("Versicherungszeitraum ab", c.From)
		add(err)
		cp := model.CoveragePeriod{From: from}
		for _, sh := range c.Shares {
			if sh.PayerID == "" || strings.TrimSpace(sh.Rate) == "" {
				continue
			}
			r, err := parseRate("Anteil", sh.Rate)
			add(err)
			cp.Shares = append(cp.Shares, model.Share{PayerID: sh.PayerID, Rate: r})
		}
		s.Coverage = append(s.Coverage, cp)
	}
	for _, y := range d.Years {
		ys := model.YearSetting{Year: y.Year, PayerID: y.PayerID}
		var err error
		ys.Deductible, err = parseAmount(fmt.Sprintf("%d – Selbstbehalt", y.Year), y.Deductible)
		add(err)
		ys.AnnualDeduction, err = parseAmount(fmt.Sprintf("%d – Kostendämpfungspauschale", y.Year), y.AnnualDeduction)
		add(err)
		ys.ExpectedPremiumRefund, err = parseAmount(fmt.Sprintf("%d – Beitragsrückerstattung", y.Year), y.ExpectedPremiumRefund)
		add(err)
		if ys.Deductible != 0 || ys.AnnualDeduction != 0 || ys.ExpectedPremiumRefund != 0 {
			s.Years = append(s.Years, ys)
		}
	}
	for _, a := range d.Aliases {
		if strings.TrimSpace(a.From) != "" && strings.TrimSpace(a.To) != "" {
			s.Aliases = append(s.Aliases, model.Alias{From: strings.TrimSpace(a.From), To: strings.TrimSpace(a.To)})
		}
	}
	tol, err := parseAmount("Toleranz", d.ClarifyTolerance)
	add(err)
	if tol != model.DefaultClarifyTolerance {
		s.ClarifyTolerance = tol
	}
	if len(errs) > 0 {
		return s, errors.New(strings.Join(errs, "\n"))
	}
	return s, nil
}

// GetSettings returns the shared settings.
func (s *Service) GetSettings() (SettingsDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return SettingsDTO{}, err
	}
	return settingsDTO(&data.Settings), nil
}

// SaveSettings validates and stores the settings. Errors prevent saving;
// warnings are returned with the result.
func (s *Service) SaveSettings(d SettingsDTO) (SettingsResultDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, data, err := s.ready()
	if err != nil {
		return SettingsResultDTO{}, err
	}
	set, err := settingsFromDTO(d)
	if err != nil {
		return SettingsResultDTO{}, err
	}
	var errs []string
	res := SettingsResultDTO{Problems: []ProblemDTO{}}
	for _, p := range set.Validate() {
		if p.Error {
			errs = append(errs, p.Text)
		} else {
			res.Problems = append(res.Problems, ProblemDTO(p))
		}
	}
	if len(errs) > 0 {
		return SettingsResultDTO{}, errors.New(strings.Join(errs, "\n"))
	}
	saved, err := st.SaveSettings(set)
	if err != nil {
		return SettingsResultDTO{}, fmt.Errorf("Speichern fehlgeschlagen: %w", err)
	}
	data.Settings = saved
	res.Settings = settingsDTO(&saved)
	return res, nil
}

// SaveYearSetting stores the per-year values of one payer (from the dashboard).
func (s *Service) SaveYearSetting(y YearSettingDTO) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, data, err := s.ready()
	if err != nil {
		return err
	}
	if _, ok := data.Settings.Payer(y.PayerID); !ok {
		return fmt.Errorf("unbekannter Kostenträger %q", y.PayerID)
	}
	d := settingsDTO(&data.Settings)
	found := false
	for i := range d.Years {
		if d.Years[i].Year == y.Year && d.Years[i].PayerID == y.PayerID {
			d.Years[i], found = y, true
		}
	}
	if !found {
		d.Years = append(d.Years, y)
	}
	set, err := settingsFromDTO(d)
	if err != nil {
		return err
	}
	saved, err := st.SaveSettings(set)
	if err != nil {
		return fmt.Errorf("Speichern fehlgeschlagen: %w", err)
	}
	data.Settings = saved
	return nil
}

// SetupDTO is the quick setup on first start.
type SetupDTO struct {
	WithBeihilfe bool   `json:"withBeihilfe"`
	BeihilfeName string `json:"beihilfeName"`
	BeihilfeRate string `json:"beihilfeRate"` // "50" or "70"; the PKV gets the rest
	// BundRules sets the rules of the Bund (BBhV): 3 years deadline, more than 200 € per claim.
	BundRules bool   `json:"bundRules"`
	PKVName   string `json:"pkvName"`
	From      string `json:"from"` // coverage valid from
}

// Setup creates payers and the first coverage period.
func (s *Service) Setup(d SetupDTO) (StatusDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, data, err := s.ready()
	if err != nil {
		return s.status(), err
	}
	if len(data.Settings.Payers) > 0 {
		return s.status(), errors.New("es sind bereits Kostenträger eingerichtet – bitte in den Einstellungen ändern")
	}
	from, err := parseDate("Gültig ab", d.From)
	if err != nil {
		return s.status(), err
	}
	if from.IsZero() {
		return s.status(), errors.New("Gültig ab fehlt")
	}
	taken := map[string]bool{}
	pkvName := strings.TrimSpace(d.PKVName)
	if pkvName == "" {
		pkvName = "PKV"
	}
	set := data.Settings
	pkv := model.Payer{ID: payerID(pkvName, taken), Kind: model.PKV, Name: pkvName}
	period := model.CoveragePeriod{From: from}
	if d.WithBeihilfe {
		name := strings.TrimSpace(d.BeihilfeName)
		if name == "" {
			name = "Beihilfe"
		}
		rate, err := parseRate("Beihilfe-Satz", d.BeihilfeRate)
		if err != nil {
			return s.status(), err
		}
		bh := model.Payer{ID: payerID(name, taken), Kind: model.Beihilfe, Name: name}
		if d.BundRules {
			bh.ClaimDeadlineYears, bh.MinClaimAmount = 3, 20000
		}
		set.Payers = []model.Payer{bh, pkv}
		period.Shares = []model.Share{{PayerID: bh.ID, Rate: rate}, {PayerID: pkv.ID, Rate: money.Full - rate}}
	} else {
		set.Payers = []model.Payer{pkv}
		period.Shares = []model.Share{{PayerID: pkv.ID, Rate: money.Full}}
	}
	set.Coverage = []model.CoveragePeriod{period}
	saved, err := st.SaveSettings(set)
	if err != nil {
		return s.status(), fmt.Errorf("Speichern fehlgeschlagen: %w", err)
	}
	data.Settings = saved
	return s.status(), nil
}
