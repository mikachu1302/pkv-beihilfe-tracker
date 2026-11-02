package calc

import (
	"strings"
	"testing"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

var d = civil.MustParse

func eur(s string) money.Cents {
	c, err := money.Parse(s)
	if err != nil {
		panic(err)
	}
	return c
}

// settings70 is Beihilfe 50/50 until 2025, 70/30 from 2026 (e.g. retirement).
func settings70() *model.Settings {
	return &model.Settings{
		Payers: []model.Payer{
			{ID: "bh", Kind: model.Beihilfe, Name: "Beihilfe", ClaimDeadlineYears: 3, MinClaimAmount: eur("200")},
			{ID: "pkv", Kind: model.PKV, Name: "PKV"},
		},
		Coverage: []model.CoveragePeriod{
			{From: d("2023-01-01"), Shares: []model.Share{{PayerID: "bh", Rate: money.Percent(50)}, {PayerID: "pkv", Rate: money.Percent(50)}}},
			{From: d("2026-01-01"), Shares: []model.Share{{PayerID: "bh", Rate: money.Percent(70)}, {PayerID: "pkv", Rate: money.Percent(30)}}},
		},
	}
}

func claimOf(v InvoiceView, payer string) ClaimView {
	for _, c := range v.Claims {
		if c.PayerID == payer {
			return c
		}
	}
	return ClaimView{}
}

func refund(amount string) []model.Refund { return []model.Refund{{ID: "r", Amount: eur(amount)}} }

func TestInvoiceExample(t *testing.T) {
	// The example from the proposal: 286,40 € at 70/30, Beihilfe refunded 180,20 €, PKV held back.
	inv := model.Invoice{
		ID: "a", Provider: "Dr. Muster", InvoiceDate: d("2026-03-14"), ServiceFrom: d("2026-03-03"), Amount: eur("286,40"),
		Paid: model.Mark{Date: d("2026-03-20")},
		Claims: map[string]model.Claim{
			"bh":  {Submitted: model.Mark{Date: d("2026-04-02")}, Refunded: model.Mark{Date: d("2026-04-28")}, Refunds: refund("180,20")},
			"pkv": {Held: true},
		},
	}
	v := Invoice(settings70(), &inv, d("2026-11-02"))
	bh, pkv := claimOf(v, "bh"), claimOf(v, "pkv")
	if bh.Expected != eur("200,48") || bh.Actual != eur("180,20") || bh.Diff != eur("-20,28") || bh.Status != StatusRefunded || !bh.SuggestClarify {
		t.Errorf("Beihilfe: %+v", bh)
	}
	if pkv.Expected != eur("85,92") || pkv.Status != StatusHeld || !pkv.Deadline.IsZero() {
		t.Errorf("PKV: %+v", pkv)
	}
	if v.Year != 2026 || v.Expected != eur("286,40") || v.OwnShare != eur("106,20") || v.Settled || len(v.Warnings) != 0 {
		t.Errorf("view: %+v", v)
	}
	// Marking Klärungsbedarf removes the suggestion.
	c := inv.Claims["bh"]
	c.Clarify = model.Mark{Date: d("2026-04-29"), Note: "Steigerungssatz"}
	inv.Claims["bh"] = c
	if Invoice(settings70(), &inv, d("2026-11-02")).Claims[0].SuggestClarify {
		t.Error("suggestion should disappear once Klärungsbedarf is marked")
	}
}

func TestRateFollowsTreatmentDate(t *testing.T) {
	s := settings70()
	inv := model.Invoice{InvoiceDate: d("2026-01-15"), ServiceFrom: d("2025-12-18"), Amount: eur("100")}
	v := Invoice(s, &inv, d("2026-02-01"))
	if v.Year != 2025 || claimOf(v, "bh").Expected != eur("50") || claimOf(v, "pkv").Expected != eur("50") {
		t.Errorf("December 2025 treatment must use 50/50 and count for 2025: %+v", v)
	}
}

func TestOverridesAndStatus(t *testing.T) {
	s := settings70()
	eligible, expected, rate := eur("200"), eur("10"), money.Percent(80)
	inv := model.Invoice{InvoiceDate: d("2026-02-01"), Amount: eur("250"), Claims: map[string]model.Claim{
		"bh":  {Eligible: &eligible, Rate: &rate},
		"pkv": {Expected: &expected, Skip: true, Refunds: refund("5")},
	}}
	v := Invoice(s, &inv, d("2026-02-02"))
	if got := claimOf(v, "bh"); got.Expected != eur("160") || got.Status != StatusOpen {
		t.Errorf("bh: %+v", got)
	}
	if got := claimOf(v, "pkv"); got.Expected != eur("10") || got.Status != StatusSkipped {
		t.Errorf("pkv: skip must win over refunds: %+v", got)
	}
	if ClaimStatus(model.Claim{Submitted: model.Mark{Date: d("2026-01-01")}, Held: true}) != StatusSubmitted {
		t.Error("submitted wins over held")
	}
}

func TestDeadline(t *testing.T) {
	s := settings70()
	inv := model.Invoice{InvoiceDate: d("2023-06-01"), Amount: eur("80")}
	bh := claimOf(Invoice(s, &inv, d("2026-04-01")), "bh")
	if bh.Deadline.String() != "2026-06-01" || bh.DeadlineDays != 61 {
		t.Errorf("deadline %s, %d days", bh.Deadline, bh.DeadlineDays)
	}
	inv.Claims = map[string]model.Claim{"bh": {Submitted: model.Mark{Date: d("2026-04-02")}}}
	if bh := claimOf(Invoice(s, &inv, d("2026-04-03")), "bh"); !bh.Deadline.IsZero() {
		t.Error("submitted claims have no deadline")
	}
}

func TestFullPKV(t *testing.T) {
	s := &model.Settings{
		Payers:   []model.Payer{{ID: "pkv", Kind: model.PKV, Name: "PKV"}},
		Coverage: []model.CoveragePeriod{{From: d("2020-01-01"), Shares: []model.Share{{PayerID: "pkv", Rate: money.Full}}}},
	}
	inv := model.Invoice{InvoiceDate: d("2026-02-01"), Amount: eur("99,99")}
	v := Invoice(s, &inv, d("2026-02-02"))
	if len(v.Claims) != 1 || v.Claims[0].Expected != eur("99,99") {
		t.Errorf("100 %% PKV: %+v", v.Claims)
	}
}

func TestWarnings(t *testing.T) {
	inv := model.Invoice{
		InvoiceDate: d("2024-02-14"), ServiceFrom: d("2024-02-20"), Amount: eur("100"),
		Claims: map[string]model.Claim{
			"bh":  {Submitted: model.Mark{Date: d("2013-09-17")}, Refunds: refund("80")},
			"pkv": {Submitted: model.Mark{Date: d("2024-02-02")}, Refunds: refund("50")},
		},
	}
	v := Invoice(settings70(), &inv, d("2026-01-01"))
	codes := map[string]int{}
	for _, w := range v.Warnings {
		codes[w.Code]++
	}
	for code, n := range map[string]int{"service_after_invoice": 1, "refund_exceeds_amount": 1, "date_implausible": 1, "date_before_invoice": 1} {
		if codes[code] != n {
			t.Errorf("%s: %d, want %d (%v)", code, codes[code], n, v.Warnings)
		}
	}
}

func TestRoundingIsNoWarning(t *testing.T) {
	inv := model.Invoice{InvoiceDate: d("2025-02-01"), Amount: eur("301,37"), Claims: map[string]model.Claim{
		"bh": {Refunds: refund("150,69")}, "pkv": {Refunds: refund("150,69")}}}
	if w := Invoice(settings70(), &inv, d("2026-01-01")).Warnings; len(w) != 0 {
		t.Errorf("one cent per payer is rounding: %v", w)
	}
	inv.Claims["pkv"] = model.Claim{Refunds: refund("150,71")}
	if w := Invoice(settings70(), &inv, d("2026-01-01")).Warnings; len(w) != 1 {
		t.Errorf("three cents too much should warn: %v", w)
	}
}

// dashboard: five invoices in 2026 (3.800 €) at 70/30.
func dashboardInvoices() []model.Invoice {
	amounts := []string{"1000", "1200", "800", "500", "300"}
	var out []model.Invoice
	for i, a := range amounts {
		inv := model.Invoice{ID: string(rune('a' + i)), InvoiceDate: d("2026-03-01").AddDays(i), Amount: eur(a),
			Claims: map[string]model.Claim{"pkv": {Held: true}}}
		bh := model.Claim{}
		switch {
		case i < 3: // refunded exactly
			bh.Submitted = model.Mark{Date: d("2026-04-01")}
			bh.Refunds = refund(eur(a).MulRate(money.Percent(70)).String())
		case i == 3:
			bh.Submitted = model.Mark{Date: d("2026-04-01")}
		}
		inv.Claims["bh"] = bh
		out = append(out, inv)
	}
	return out
}

func TestYears(t *testing.T) {
	s := settings70()
	s.Years = []model.YearSetting{{Year: 2026, PayerID: "pkv", ExpectedPremiumRefund: eur("1260")}}
	invs := dashboardInvoices()
	invs = append(invs, model.Invoice{ID: "z", Deleted: true, InvoiceDate: d("2026-05-01"), Amount: eur("999")})
	allow := []model.Allowance{{ID: "k", PayerID: "pkv", Date: d("2026-07-01"), Amount: eur("450")}}
	years := Years(s, invs, allow, d("2026-11-02"))
	if len(years) != 1 {
		t.Fatalf("years: %d", len(years))
	}
	y := years[0]
	if y.Year != 2026 || y.Invoices != 5 || y.Billed != eur("3800") || y.Allowances != eur("450") {
		t.Fatalf("year: %+v", y)
	}
	if len(y.Payers) != 2 || y.Payers[0].PayerID != "bh" {
		t.Fatalf("payers: %+v", y.Payers)
	}
	bh, pkv := y.Payers[0], y.Payers[1]
	if bh.Expected != eur("2660") || bh.Actual != eur("2100") || bh.Refunded.Count != 3 ||
		bh.Submitted.Expected != eur("350") || bh.Open.Expected != eur("210") || bh.OpenBilled != eur("300") ||
		bh.MinClaimAmount != eur("200") || bh.Deviation != 0 || bh.PremiumRefund != nil {
		t.Errorf("Beihilfe: %+v", bh)
	}
	if pkv.Expected != eur("1140") || pkv.Held.Count != 5 || pkv.Held.Expected != eur("1140") {
		t.Errorf("PKV: %+v", pkv)
	}
	pr := pkv.PremiumRefund
	if pr == nil || pr.ClaimAll != eur("1140") || pr.HoldTotal != eur("1260") || pr.Advantage != eur("-120") || pr.Forfeited {
		t.Fatalf("premium refund: %+v", pr)
	}
	if y.OwnShare != eur("1700") || y.OwnShareSettled != 0 {
		t.Errorf("own share %s / settled %s", y.OwnShare, y.OwnShareSettled)
	}

	// A preventive check-up that keeps the BRE, plus a deductible of 300 €.
	c := invs[4].Claims["pkv"]
	c.KeepsPremiumRefund = true
	invs[4].Claims["pkv"] = c
	s.Years[0].Deductible = eur("300")
	pr = Years(s, invs, nil, d("2026-11-02"))[0].Payers[1].PremiumRefund
	if pr.ClaimAll != eur("840") || pr.ClaimNeutralOnly != 0 || pr.HoldTotal != eur("1260") || pr.Advantage != eur("-420") {
		t.Errorf("with deductible: %+v", pr)
	}
	s.Years[0].Deductible = 0
	pr = Years(s, invs, nil, d("2026-11-02"))[0].Payers[1].PremiumRefund
	if pr.ClaimNeutralOnly != eur("90") || pr.HoldTotal != eur("1350") || pr.Advantage != eur("-210") {
		t.Errorf("with neutral claim: %+v", pr)
	}

	// Submitting a relevant PKV claim forfeits the BRE.
	c = invs[0].Claims["pkv"]
	c.Held, c.Submitted = false, model.Mark{Date: d("2026-10-01")}
	invs[0].Claims["pkv"] = c
	if pr := Years(s, invs, nil, d("2026-11-02"))[0].Payers[1].PremiumRefund; !pr.Forfeited {
		t.Error("BRE should be forfeited")
	}
}

func TestYearsSplitAndTotal(t *testing.T) {
	s := settings70()
	invs := []model.Invoice{
		{ID: "1", InvoiceDate: d("2025-05-01"), Amount: eur("100"), Claims: map[string]model.Claim{
			"bh": {Refunds: refund("50")}, "pkv": {Refunds: refund("50")}}},
		{ID: "2", InvoiceDate: d("2026-01-10"), ServiceFrom: d("2025-12-20"), Amount: eur("40")},
		{ID: "3", InvoiceDate: d("2026-02-01"), Amount: eur("10")},
		{ID: "4", Amount: eur("5")}, // no date at all
	}
	years := Years(s, invs, nil, d("2026-03-01"))
	if len(years) != 3 || years[0].Year != 0 || years[1].Year != 2025 || years[2].Year != 2026 {
		t.Fatalf("years: %+v", years)
	}
	if years[1].Invoices != 2 || years[1].Billed != eur("140") || years[1].OwnShareSettled != 0 || years[1].Actual != eur("100") {
		t.Errorf("2025: %+v", years[1])
	}
	tot := Total(s, years)
	if tot.Invoices != 4 || tot.Billed != eur("155") || len(tot.Payers) != 2 || tot.Payers[0].Claims != 4 {
		t.Errorf("total: %+v", tot)
	}
	if !strings.Contains(tot.Payers[1].PayerName, "PKV") {
		t.Errorf("payer order: %+v", tot.Payers)
	}
}
