package service

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// newTestService returns a service with a fresh config and data folder,
// set up for Beihilfe 70 % (Bund rules) + PKV 30 %, "today" = 2026-11-02.
func newTestService(t *testing.T) *Service {
	t.Helper()
	s := NewWithConfigDir(t.TempDir())
	s.now = func() time.Time { return time.Date(2026, 11, 2, 9, 0, 0, 0, time.Local) }
	st, err := s.SetDataDir(filepath.Join(t.TempDir(), "daten"))
	if err != nil || !st.Configured || !st.NeedsSetup {
		t.Fatalf("SetDataDir: %+v, %v", st, err)
	}
	st, err = s.Setup(SetupDTO{WithBeihilfe: true, BeihilfeName: "Beihilfe Bund", BeihilfeRate: "70", BundRules: true,
		PKVName: "Muster PKV", From: "2023-01-01"})
	if err != nil || st.NeedsSetup || len(st.Problems) != 0 {
		t.Fatalf("Setup: %+v, %v", st, err)
	}
	return s
}

func sampleInvoice() InvoiceDTO {
	return InvoiceDTO{Provider: "Dr. Muster", InvoiceDate: "2026-03-14", ServiceFrom: "2026-03-03", Amount: "286,40",
		Category: "arzt"}
}

func claim(row InvoiceRowDTO, payer string) ClaimViewDTO {
	for _, c := range row.Claims {
		if c.PayerID == payer {
			return c
		}
	}
	return ClaimViewDTO{}
}

func TestSetupCreatesPayers(t *testing.T) {
	s := newTestService(t)
	set, err := s.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Payers) != 2 || set.Payers[0].ID != "beihilfe-bund" || set.Payers[0].ClaimDeadlineYears != 3 ||
		set.Payers[0].MinClaimAmount != "200.00" || set.Payers[1].ID != "muster-pkv" {
		t.Errorf("payers: %+v", set.Payers)
	}
	if len(set.Coverage) != 1 || set.Coverage[0].Shares[1].Rate != "30" {
		t.Errorf("coverage: %+v", set.Coverage)
	}
	if _, err := s.Setup(SetupDTO{PKVName: "X", From: "2024-01-01"}); err == nil {
		t.Error("second setup should be refused")
	}
}

func TestInvoiceLifecycle(t *testing.T) {
	s := newTestService(t)
	if _, err := s.SaveInvoice(InvoiceDTO{Amount: "x"}); err == nil || !strings.Contains(err.Error(), "kein gültiger Betrag") {
		t.Fatalf("invalid input: %v", err)
	}
	if _, err := s.SaveInvoice(InvoiceDTO{}); err == nil || !strings.Contains(err.Error(), "Behandler fehlt") {
		t.Fatalf("missing fields: %v", err)
	}
	det, err := s.SaveInvoice(sampleInvoice())
	if err != nil {
		t.Fatal(err)
	}
	id := det.Invoice.ID
	bh := claim(det.View, "beihilfe-bund")
	if id == "" || bh.Expected != "200.48" || claim(det.View, "muster-pkv").Expected != "85.92" || bh.Status != "open" ||
		bh.Deadline != "2029-03-14" || len(det.Invoice.Claims) != 2 {
		t.Fatalf("saved: %+v", det)
	}

	// Tick "eingereicht" (today) and "erstattet" with an amount below SOLL.
	row, err := s.SetMark(MarkChangeDTO{InvoiceID: id, PayerID: "beihilfe-bund", Mark: MarkSubmitted, On: true})
	if err != nil || claim(row, "beihilfe-bund").Submitted.Date != "2026-11-02" {
		t.Fatalf("submitted: %+v, %v", row, err)
	}
	row, err = s.SetMark(MarkChangeDTO{InvoiceID: id, PayerID: "beihilfe-bund", Mark: MarkRefunded, On: true, Amount: "180,20", Date: "2026-11-20"})
	bh = claim(row, "beihilfe-bund")
	if err != nil || bh.Actual != "180.20" || bh.Diff != "-20.28" || !bh.SuggestClarify || bh.Refunded.Date != "2026-11-20" {
		t.Fatalf("refunded: %+v, %v", bh, err)
	}
	// PKV: ticking "erstattet" without amount takes the full SOLL.
	row, _ = s.SetMark(MarkChangeDTO{InvoiceID: id, PayerID: "muster-pkv", Mark: MarkRefunded, On: true})
	if pkv := claim(row, "muster-pkv"); pkv.Actual != "85.92" || !row.Settled {
		t.Fatalf("pkv: %+v", pkv)
	}
	row, _ = s.SetMark(MarkChangeDTO{InvoiceID: id, PayerID: "beihilfe-bund", Mark: MarkClarify, On: true, Note: "Steigerungssatz"})
	if c := claim(row, "beihilfe-bund"); !c.Clarify.Done || c.Clarify.Note != "Steigerungssatz" || c.SuggestClarify {
		t.Fatalf("clarify: %+v", c)
	}
	if _, err := s.SetMark(MarkChangeDTO{InvoiceID: id, PayerID: "andere", Mark: MarkHeld, On: true}); err == nil {
		t.Error("unknown payer accepted")
	}
	// Unticking "erstattet" removes the refunds entered by hand.
	row, _ = s.SetMark(MarkChangeDTO{InvoiceID: id, PayerID: "muster-pkv", Mark: MarkRefunded, On: false})
	if c := claim(row, "muster-pkv"); c.Actual != "0.00" || c.Status != "open" {
		t.Fatalf("unticked: %+v", c)
	}
	row, _ = s.SetMark(MarkChangeDTO{InvoiceID: id, Mark: MarkPaid, On: true, Date: "2026-03-20"})
	if !row.Paid.Done || row.Paid.Date != "2026-03-20" {
		t.Fatalf("paid: %+v", row.Paid)
	}

	// The form round-trips: load, change a field, save.
	got, err := s.GetInvoice(id)
	if err != nil {
		t.Fatal(err)
	}
	in := got.Invoice
	in.Note = "Kontrolle"
	in.Claims[1].Held = true
	in.Claims[1].Refunds = append(in.Claims[1].Refunds, RefundDTO{}) // empty new row is ignored
	saved, err := s.SaveInvoice(in)
	if err != nil || saved.Invoice.Note != "Kontrolle" || claim(saved.View, "muster-pkv").Status != "held" ||
		claim(saved.View, "beihilfe-bund").Actual != "180.20" {
		t.Fatalf("round trip: %+v, %v", saved, err)
	}

	// Data survives a restart (same config folder).
	s2 := NewWithConfigDir(s.cfgDir)
	rows, err := s2.ListInvoices(2026)
	if err != nil || len(rows) != 1 || rows[0].Note != "Kontrolle" {
		t.Fatalf("after restart: %+v, %v", rows, err)
	}
	if err := s.DeleteInvoice(id); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.ListInvoices(0); len(rows) != 0 {
		t.Errorf("deleted invoice listed: %+v", rows)
	}
}

func TestPreviewInvoice(t *testing.T) {
	s := newTestService(t)
	in := sampleInvoice()
	in.Claims = []ClaimDTO{{PayerID: "beihilfe-bund", Rate: "50"}}
	det, err := s.PreviewInvoice(in)
	if err != nil || claim(det.View, "beihilfe-bund").Expected != "143.20" || len(det.Invoice.Claims) != 2 {
		t.Fatalf("preview: %+v, %v", det, err)
	}
	if rows, _ := s.ListInvoices(0); len(rows) != 0 {
		t.Error("preview must not save")
	}
	nw, err := s.NewInvoice()
	if err != nil || nw.Invoice.InvoiceDate != "2026-11-02" || len(nw.Invoice.Claims) != 2 {
		t.Fatalf("new: %+v, %v", nw, err)
	}
}

func TestBulkSubmitAndDashboard(t *testing.T) {
	s := newTestService(t)
	var ids []string
	for _, a := range []string{"1000", "500", "300"} {
		in := sampleInvoice()
		in.Amount = a
		det, err := s.SaveInvoice(in)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, det.Invoice.ID)
	}
	s.SetMark(MarkChangeDTO{InvoiceID: ids[2], PayerID: "beihilfe-bund", Mark: MarkSubmitted, On: true, Date: "2026-04-01"})
	res, err := s.BulkSubmit(BulkSubmitDTO{InvoiceIDs: ids, PayerID: "beihilfe-bund", Date: "2026-05-01", Channel: "App"})
	if err != nil || res.Marked != 2 || len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0], "schon eingereicht am 01.04.2026") {
		t.Fatalf("bulk: %+v, %v", res, err)
	}
	if len(s.data.Submissions) != 1 || len(s.data.Submissions[0].InvoiceIDs) != 2 {
		t.Fatalf("submission: %+v", s.data.Submissions)
	}
	if err := s.SaveYearSetting(YearSettingDTO{Year: 2026, PayerID: "muster-pkv", ExpectedPremiumRefund: "600"}); err != nil {
		t.Fatal(err)
	}
	y, err := s.Dashboard(2026)
	if err != nil {
		t.Fatal(err)
	}
	if y.Invoices != 3 || y.Billed != "1800.00" || len(y.Payers) != 2 {
		t.Fatalf("year: %+v", y)
	}
	bh, pkv := y.Payers[0], y.Payers[1]
	if bh.Expected != "1260.00" || bh.Submitted.Count != 3 || bh.MinClaimAmount != "200.00" {
		t.Errorf("beihilfe: %+v", bh)
	}
	if pkv.PremiumRefund == nil || pkv.PremiumRefund.ClaimAll != "540.00" || pkv.PremiumRefund.Advantage != "-60.00" ||
		pkv.Setting.ExpectedPremiumRefund != "600.00" {
		t.Errorf("pkv: %+v %+v", pkv, pkv.PremiumRefund)
	}
	// A year without invoices still lists the payers of the coverage.
	empty, _ := s.Dashboard(2024)
	if empty.Invoices != 0 || len(empty.Payers) != 2 {
		t.Errorf("empty year: %+v", empty)
	}
	ov, err := s.Overview()
	if err != nil || len(ov.Years) != 1 || ov.Total.Billed != "1800.00" {
		t.Errorf("overview: %+v, %v", ov, err)
	}
	years, _ := s.Years()
	if len(years) != 1 || years[0] != 2026 {
		t.Errorf("years: %v", years)
	}
}

func TestAllowances(t *testing.T) {
	s := newTestService(t)
	a, err := s.SaveAllowance(AllowanceDTO{PayerID: "muster-pkv", Date: "2025-07-01", Amount: "450"})
	if err != nil || a.Kind != "krankenhaustagegeld" {
		t.Fatalf("save: %+v, %v", a, err)
	}
	if list, _ := s.ListAllowances(2025); len(list) != 1 {
		t.Fatalf("list: %+v", list)
	}
	if y, _ := s.Dashboard(2025); y.Allowances != "450.00" {
		t.Errorf("dashboard: %+v", y)
	}
	if err := s.DeleteAllowance(a.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListAllowances(0); len(list) != 0 {
		t.Errorf("deleted allowance listed")
	}
	if _, err := s.SaveAllowance(AllowanceDTO{PayerID: "muster-pkv", Date: "2025-07-01"}); err == nil {
		t.Error("allowance without amount accepted")
	}
}

func TestSaveSettings(t *testing.T) {
	s := newTestService(t)
	set, _ := s.GetSettings()
	set.Payers = append(set.Payers, PayerDTO{Kind: "pkv", Name: "Neue PKV"})
	set.Coverage = append(set.Coverage, CoverageDTO{From: "2027-01-01", Shares: []ShareDTO{
		{PayerID: "beihilfe-bund", Rate: "70"}, {PayerID: "neue-pkv", Rate: "20"}}})
	res, err := s.SaveSettings(set)
	if err != nil || len(res.Settings.Payers) != 3 || res.Settings.Payers[2].ID != "neue-pkv" || len(res.Problems) != 1 {
		t.Fatalf("save: %+v, %v", res, err)
	}
	set = res.Settings
	set.Coverage[1].Shares[1].PayerID = "gibt-es-nicht"
	if _, err := s.SaveSettings(set); err == nil || !strings.Contains(err.Error(), "unbekannter Kostenträger") {
		t.Errorf("invalid settings saved: %v", err)
	}
}

func TestSpreadsheetImport(t *testing.T) {
	s := newTestService(t)
	path := filepath.Join(t.TempDir(), "alt.xlsx")
	f := excelize.NewFile()
	rows := [][]any{
		{"Rechnungsdatum", "Behandler", "Gesamtbetrag", "Eingereicht Beihilfe", "Betrag Erstattet Beihilfe", "Krankenhaus Tagegeld"},
		{"14.03.2026", "Dr. Muster", 286.40, "02.04.2026", 180.20, nil},
		{"15.03.2026", "Klinik Beispiel", 1200, nil, nil, 300},
	}
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		f.SetSheetRow("Sheet1", cell, &r)
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	// The same invoice already exists (other spelling) → flagged as duplicate.
	if _, err := s.SaveInvoice(InvoiceDTO{Provider: "dr.  MUSTER", InvoiceDate: "2026-03-14", Amount: "286.40"}); err != nil {
		t.Fatal(err)
	}
	pv, err := s.PreviewSpreadsheet(path, "beihilfe-bund", "muster-pkv")
	if err != nil {
		t.Fatal(err)
	}
	if len(pv.Rows) != 2 || pv.Duplicates != 1 || !pv.Rows[0].Duplicate || pv.Allowances != 1 || pv.Rows[0].Status[0] != "Beihilfe Bund: eingereicht 02.04.2026, erstattet 180,20 €" {
		t.Fatalf("preview: %+v", pv)
	}
	res, err := s.CommitSpreadsheet(pv.Token, []string{"1"})
	if err != nil || res.Invoices != 1 || res.Allowances != 1 || res.Skipped != 1 {
		t.Fatalf("commit: %+v, %v", res, err)
	}
	if _, err := s.CommitSpreadsheet(pv.Token, []string{"0"}); err == nil {
		t.Error("token reused")
	}
	if _, err := s.PreviewSpreadsheet(path, "", ""); err == nil {
		t.Error("missing PKV accepted")
	}
}

func TestNoDataDir(t *testing.T) {
	s := NewWithConfigDir(t.TempDir())
	if st := s.Status(); st.Configured || st.DeviceID == "" {
		t.Fatalf("status: %+v", st)
	}
	if _, err := s.ListInvoices(0); !errors.Is(err, ErrNoDataDir) {
		t.Errorf("err = %v", err)
	}
}
