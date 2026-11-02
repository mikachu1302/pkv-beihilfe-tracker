package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

var d = civil.MustParse

func settings() Settings {
	return Settings{
		Payers: []Payer{
			{ID: "bh", Kind: Beihilfe, Name: "Beihilfe", ClaimDeadlineYears: 3},
			{ID: "pkv", Kind: PKV, Name: "PKV"},
		},
		Coverage: []CoveragePeriod{
			{From: d("2027-03-01"), Shares: []Share{{"bh", money.Percent(70)}, {"pkv", money.Percent(30)}}},
			{From: d("2023-01-01"), Shares: []Share{{"bh", money.Percent(50)}, {"pkv", money.Percent(50)}}},
		},
	}
}

func TestSharesAt(t *testing.T) {
	s := settings()
	if got := s.ShareOf("bh", d("2027-02-28")); got != money.Percent(50) {
		t.Errorf("before change: %s", got)
	}
	if got := s.ShareOf("bh", d("2027-03-01")); got != money.Percent(70) {
		t.Errorf("on change: %s", got)
	}
	if got := s.SharesAt(d("2022-12-31")); got != nil {
		t.Errorf("before first period: %v", got)
	}
	if got := s.ShareOf("pkv", civil.Date{}); got != money.Percent(30) {
		t.Errorf("zero date should use the latest period: %s", got)
	}
}

func TestServiceYear(t *testing.T) {
	inv := Invoice{InvoiceDate: d("2026-01-12"), ServiceFrom: d("2025-12-18")}
	if inv.ServiceYear() != 2025 {
		t.Errorf("December treatment billed in January belongs to 2025, got %d", inv.ServiceYear())
	}
	inv.ServiceFrom = civil.Date{}
	if inv.ServiceYear() != 2026 {
		t.Errorf("without treatment date the invoice date counts, got %d", inv.ServiceYear())
	}
	inv.ServiceYearOverride = 2024
	if inv.ServiceYear() != 2024 {
		t.Error("override ignored")
	}
}

func TestMatches(t *testing.T) {
	inv := Invoice{InvoiceDate: d("2026-03-14"), ServiceFrom: d("2026-03-01"), ServiceTo: d("2026-03-05")}
	for _, s := range []string{"2026-03-14", "2026-03-01", "2026-03-03", "2026-03-05"} {
		if !inv.Matches(d(s)) {
			t.Errorf("%s should match", s)
		}
	}
	if inv.Matches(d("2026-03-06")) || inv.Matches(civil.Date{}) {
		t.Error("unexpected match")
	}
}

func TestPayerIDs(t *testing.T) {
	s := settings()
	inv := Invoice{InvoiceDate: d("2026-05-01"), Claims: map[string]Claim{"old-pkv": {Held: true}, "bh": {Held: true}}}
	if got := strings.Join(inv.PayerIDs(&s), ","); got != "bh,pkv,old-pkv" {
		t.Errorf("PayerIDs = %s", got)
	}
}

func TestValidate(t *testing.T) {
	s := settings()
	if p := s.Validate(); len(p) != 0 {
		t.Fatalf("valid settings reported %v", p)
	}
	s.Coverage = append(s.Coverage, CoveragePeriod{From: d("2030-01-01"), Shares: []Share{{"pkv", money.Percent(80)}, {"nope", money.Percent(10)}}})
	s.Payers = append(s.Payers, Payer{ID: "bh", Kind: "x"})
	var errs, warns int
	for _, p := range s.Validate() {
		if p.Error {
			errs++
		} else {
			warns++
		}
	}
	if errs != 3 || warns != 1 { // duplicate ID, bad kind, unknown payer; sum 90 %
		t.Errorf("errors %d, warnings %d: %v", errs, warns, s.Validate())
	}
}

func TestJSONOmitsEmpty(t *testing.T) {
	inv := Invoice{ID: "x", Amount: 1000, Claims: map[string]Claim{"bh": {Submitted: Mark{Date: d("2026-04-02")}}}}
	b, err := json.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"x","provider":"","amount":"10.00","claims":{"bh":{"submitted":{"date":"2026-04-02"}}}}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
	if !(Claim{}).IsZero() || (Claim{Held: true}).IsZero() {
		t.Error("Claim.IsZero")
	}
}

func TestRefundDocChecksum(t *testing.T) {
	doc := RefundDoc{PaidOut: 41452, Lines: []RefundLine{{Refund: 18020}, {Refund: 3157}, {Refund: 875}, {Refund: 29400}},
		Deductions: []Deduction{{Label: "Kostendämpfungspauschale", Amount: 10000}}}
	if !doc.ChecksumOK() {
		t.Errorf("lines %d − deductions should equal payout", doc.LinesTotal())
	}
	doc.PaidOut++
	if doc.ChecksumOK() {
		t.Error("checksum should fail")
	}
}

func TestNewID(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	a, b := NewID(now), NewID(now)
	if !strings.HasPrefix(a, "20261102-") || len(a) != 17 || a == b {
		t.Errorf("ids %q %q", a, b)
	}
}
