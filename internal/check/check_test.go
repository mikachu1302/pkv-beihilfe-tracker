package check

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// group returns the only group of kind k.
func group(t *testing.T, r Result, k Kind) Group {
	t.Helper()
	var out []Group
	for _, g := range r.Groups {
		if g.Kind == k {
			out = append(out, g)
		}
	}
	if len(out) != 1 {
		t.Fatalf("%d groups of kind %s, want 1: %+v", len(out), k, r.Groups)
	}
	return out[0]
}

// with returns the findings of a group with the given code.
func with(g Group, code string) []Finding {
	var out []Finding
	for _, f := range g.Findings {
		if f.Code == code {
			out = append(out, f)
		}
	}
	return out
}

// one returns the only finding of a group with the given code.
func one(t *testing.T, g Group, code string) Finding {
	t.Helper()
	fs := with(g, code)
	if len(fs) != 1 {
		t.Fatalf("%d findings %q, want 1: %s", len(fs), code, dump(g))
	}
	return fs[0]
}

func dump(g Group) string {
	var b strings.Builder
	for _, f := range g.Findings {
		b.WriteString("\n  " + string(f.Status) + " " + f.Code + ": " + f.Text)
		for _, x := range f.Detail {
			b.WriteString("\n      " + x)
		}
	}
	return b.String()
}

func want(t *testing.T, f Finding, st Status, text string) {
	t.Helper()
	if f.Status != st || f.Text != text {
		t.Errorf("finding %s:\n got %s %q\nwant %s %q", f.Code, f.Status, f.Text, st, text)
	}
}

func noneWorseThan(t *testing.T, g Group, st Status) {
	t.Helper()
	for _, f := range g.Findings {
		if severity[f.Status] > severity[st] {
			t.Errorf("%s: unexpected %s %s: %s", g.Title, f.Status, f.Code, f.Text)
		}
	}
}

// --- the invoice in itself -----------------------------------------------------------------

func TestBillsOfTheMadeUpPatient(t *testing.T) {
	for _, tc := range []struct {
		inv  *model.Invoice
		bill *Bill
		sums []string
	}{
		{goaeInvoice(), goaeBill(), []string{"Summe der Positionen 243,87 € = Rechnungsbetrag 243,87 €"}},
		{gozInvoice(), gozBill(), []string{"Summe der Positionen 435,86 € = Honorar 435,86 €",
			"Honorar + Material- und Laborkosten 931,59 € = Rechnungsbetrag 931,59 €"}},
		{physioInvoice(), physioBill(), []string{"Summe der Positionen 311,00 € = Rechnungsbetrag 311,00 €"}},
	} {
		r := Run(Input{Settings: settings(), Invoice: tc.inv, Bill: tc.bill})
		g := group(t, r, KindBill)
		if r.Status != OK || g.Status != OK {
			t.Errorf("%s: status %s/%s, want ok:%s", tc.inv.ID, r.Status, g.Status, dump(g))
		}
		var got []string
		for _, f := range with(g, "sum") {
			got = append(got, f.Text)
		}
		if !slices.Equal(got, tc.sums) {
			t.Errorf("%s: sums %q, want %q", tc.inv.ID, got, tc.sums)
		}
		want(t, one(t, g, "header"), OK,
			"Wie in der App: Rechnungsbetrag, Rechnungsdatum, Rechnungsnummer, IBAN, Behandlungszeitraum und Behandler")
	}
	g := group(t, Run(Input{Settings: settings(), Invoice: goaeInvoice(), Bill: goaeBill()}), KindBill)
	if g.Title != "Rechnung Hans Probe vom 12.10.2026" || g.Source != "rechnung-goae.pdf" {
		t.Errorf("title %q, source %q", g.Title, g.Source)
	}
}

// The two reading errors of the test of step 1 (docs/PLAN-v0.6.md) are
// caught by the sums – as "prüfen", not as a deviation of the invoice.
func TestReadingErrorsAreCaughtBySums(t *testing.T) {
	merged := gozBill() // two rows without a tooth number merged: the Ä1 position missing
	merged.Positions = slices.DeleteFunc(merged.Positions, func(p Position) bool { return p.Code == "Ä1" })
	r := Run(Input{Settings: settings(), Invoice: gozInvoice(), Bill: merged})
	g := group(t, r, KindBill)
	fs := with(g, "sum")
	if len(fs) != 2 {
		t.Fatalf("sums: %s", dump(g))
	}
	want(t, fs[0], Check, "Summe der Positionen 425,14 €, Honorar 435,86 € (Differenz -10,72 €) – fehlt eine Position, oder ist ein Betrag falsch gelesen?")
	want(t, fs[1], OK, "Honorar + Material- und Laborkosten 931,59 € = Rechnungsbetrag 931,59 €")
	if r.Status != Check {
		t.Errorf("status %s, want check", r.Status)
	}

	dark := goaeBill() // 16,78 read as 18,78 on the dark photo
	dark.Positions[10].Amount = eur("18,78")
	g = group(t, Run(Input{Settings: settings(), Invoice: goaeInvoice(), Bill: dark}), KindBill)
	want(t, one(t, g, "sum"), Check, "Summe der Positionen 245,87 €, Rechnungsbetrag 243,87 € (Differenz +2,00 €) – fehlt eine Position, oder ist ein Betrag falsch gelesen?")
}

func TestSumRounding(t *testing.T) {
	third := func(n int, total string) *Bill {
		b := &Bill{Total: eur(total)}
		for range n {
			b.Positions = append(b.Positions, Position{Code: "1", Amount: eur("33,33")})
		}
		return b
	}
	inv := &model.Invoice{ID: "x", Amount: eur("100,00")}
	// 3 × 33,333… rounded: half a cent per position.
	g := group(t, Run(Input{Settings: settings(), Invoice: inv, Bill: third(3, "100,00")}), KindBill)
	want(t, one(t, g, "sum"), OK, "Summe der Positionen 99,99 € ≈ Rechnungsbetrag 100,00 € (Rundung)")
	g = group(t, Run(Input{Settings: settings(), Invoice: inv, Bill: third(3, "100,02")}), KindBill)
	if f := one(t, g, "sum"); f.Status != Check {
		t.Errorf("3 cents off for 3 positions: %s %s", f.Status, f.Text)
	}
}

func TestSumWithoutTotal(t *testing.T) {
	b := goaeBill()
	b.Total = 0 // not read: the amount in the app is used
	g := group(t, Run(Input{Settings: settings(), Invoice: goaeInvoice(), Bill: b}), KindBill)
	want(t, one(t, g, "sum"), OK, "Summe der Positionen 243,87 € = Rechnungsbetrag in der App 243,87 €")

	g = group(t, Run(Input{Settings: settings(), Invoice: goaeInvoice(), Bill: &Bill{Total: eur("10")}}), KindBill)
	want(t, one(t, g, "sum"), Info, "Keine Positionen gelesen – die Summe lässt sich nicht prüfen")
}

func TestHeaderAgainstTheApp(t *testing.T) {
	inv := goaeInvoice()
	inv.Amount = eur("234,87")
	inv.Reference = "R-2026-1178"
	inv.IBAN = "DE75250100309182736450"
	inv.ServiceTo = d("2026-09-22")
	inv.Provider = "Dr. Anna Muster"
	g := group(t, Run(Input{Settings: settings(), Invoice: inv, Bill: goaeBill()}), KindBill)
	var got []string
	for _, f := range g.Findings {
		if strings.HasPrefix(f.Code, "header") {
			got = append(got, f.Code+" "+string(f.Status)+" "+f.Text)
		}
	}
	wantTexts := []string{
		"header_amount check Rechnungsbetrag auf der Rechnung: 243,87 €, in der App: 234,87 €",
		"header_reference check Rechnungsnummer auf der Rechnung: R-2026-1187, in der App: R-2026-1178",
		"header_iban check IBAN auf der Rechnung: DE48120300004455667788, in der App: DE75250100309182736450",
		"header_period check Behandlungszeitraum auf der Rechnung: 08.09.2026–29.09.2026, in der App: 08.09.2026–22.09.2026",
		"header_provider info Behandler auf der Rechnung: Hans Probe / Muster-PVS GmbH, in der App: Dr. Anna Muster",
		"header ok Wie in der App: Rechnungsdatum",
	}
	if !slices.Equal(got, wantTexts) {
		t.Errorf("header findings:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(wantTexts, "\n"))
	}

	empty := &model.Invoice{ID: "new", Provider: "Muster-PVS", InvoiceDate: d("2026-10-12")}
	g = group(t, Run(Input{Settings: settings(), Invoice: empty, Bill: goaeBill()}), KindBill)
	want(t, one(t, g, "header_amount"), Info, "Rechnungsbetrag in der App fehlt – auf der Rechnung: 243,87 €")
}

func TestPositionDates(t *testing.T) {
	b := goaeBill()
	b.From, b.To = d("2026-09-08"), d("2026-09-29")
	b.Positions[7].Date = d("2025-09-15") // year misread
	g := group(t, Run(Input{Settings: settings(), Invoice: goaeInvoice(), Bill: b}), KindBill)
	f := one(t, g, "position_date")
	want(t, f, Check, "1 Position liegt außerhalb des Behandlungszeitraums 08.09.2026–29.09.2026")
	if len(f.Detail) != 1 || !strings.HasPrefix(f.Detail[0], "Nr. 3 am 15.09.2025 – Eingehende") || f.Page != 1 {
		t.Errorf("detail %q, page %d", f.Detail, f.Page)
	}

	b = goaeBill() // no period printed: the positions make it
	b.Positions[15].Date = d("2026-10-29")
	g = group(t, Run(Input{Settings: settings(), Invoice: goaeInvoice(), Bill: b}), KindBill)
	f = one(t, g, "position_date")
	want(t, f, Check, "1 Position liegt nach dem Rechnungsdatum 12.10.2026")
	if f.Detail[0] != "Nr. 75 am 29.10.2026 – Ausführlicher schriftlicher Krankheits- und Befundbericht: 17,43 € (Seite 2)" {
		t.Errorf("detail %q", f.Detail)
	}
}

func TestDuplicates(t *testing.T) {
	b := gozBill()
	b.Positions[4].Date = d("2026-10-06") // 0090 for tooth 36 twice on one day
	b.Positions = append(b.Positions, pos("2026-10-06", "37", "0090", "Intraorale Infiltrationsanästhesie", 1, "7,76", 1))
	b.Fee += eur("7,76")
	b.Total += eur("7,76")
	inv := gozInvoice()
	inv.Amount = b.Total
	g := group(t, Run(Input{Settings: settings(), Invoice: inv, Bill: b}), KindBill)
	want(t, one(t, g, "duplicate"), Info, "Nr. 0090 (Zahn 36) am 06.10.2026 2-mal berechnet")
	noneWorseThan(t, g, Info)
}

// --- invoice and Bescheid ------------------------------------------------------------------

func TestBeihilfeBescheidWithCut(t *testing.T) {
	r := Run(Input{Settings: settings(), Invoice: goaeInvoice(), Notices: []Notice{*beihilfeNotice()}})
	g := group(t, r, KindNotice)
	if g.Title != "Beihilfebescheid vom 18.11.2026 – Beihilfestelle Musterland" {
		t.Errorf("title %q", g.Title)
	}
	want(t, one(t, g, "notice_line"), OK, "Zeile 1 (12.10.2026, Dr. med. Hans Probe): Rechnungsbetrag 243,87 € wie in der App")
	f := one(t, g, "refund")
	want(t, f, Deviation, "Erstattung 116,69 € statt erwartet 121,94 € (50 % von 243,87 €) – Differenz -5,25 €")
	if len(f.Detail) != 2 || f.Detail[0] != "Nicht beihilfefähig: 10,49 € (beihilfefähig 233,38 € von 243,87 €)" ||
		!strings.HasPrefix(f.Detail[1], "Hinweis 1: Der Gebührensatz über dem 2,3-fachen") {
		t.Errorf("detail %q", f.Detail)
	}
	want(t, one(t, g, "refund_recorded"), Info, "In der App ist noch keine Erstattung von Beihilfestelle Musterland erfasst")
	want(t, one(t, g, "notice_sum"), OK, "Erstattungen 616,70 € − Abzüge 100,00 € = Auszahlung 516,70 €")
	want(t, one(t, g, "notice_deduction"), Info,
		"Vom Bescheid abgezogen: Kostendämpfungspauschale 2026 100,00 € – betrifft die Auszahlung, nicht die einzelne Rechnung")
	if len(with(g, "notice_values"))+len(with(g, "notice_rate")) != 0 {
		t.Errorf("unexpected findings:%s", dump(g))
	}
	if r.Status != Deviation {
		t.Errorf("status %s", r.Status)
	}

	// Two notes on one line: the prescription cut the 7th session.
	g = group(t, Run(Input{Settings: settings(), Invoice: physioInvoice(), Notices: []Notice{*beihilfeNotice()}}), KindNotice)
	f = one(t, g, "refund")
	want(t, f, Deviation, "Erstattung 123,90 € statt erwartet 155,50 € (50 % von 311,00 €) – Differenz -31,60 €")
	if len(f.Detail) != 3 || !strings.HasPrefix(f.Detail[1], "Hinweis 3: ") || !strings.HasPrefix(f.Detail[2], "Hinweis 4: Verordnet waren 6") {
		t.Errorf("detail %q", f.Detail)
	}
}

func TestPKVStatementAsExpected(t *testing.T) {
	g := group(t, Run(Input{Settings: settings(), Invoice: goaeInvoice(), Notices: []Notice{*pkvNotice()}}), KindNotice)
	if g.Title != "Leistungsabrechnung vom 20.11.2026 – Muster Krankenversicherung" {
		t.Errorf("title %q", g.Title)
	}
	want(t, one(t, g, "refund"), OK, "Erstattung 121,94 € wie erwartet (50 % von 243,87 €)")
	want(t, one(t, g, "notice_sum"), OK, "Erstattungen 745,70 € − Abzüge 75,00 € = Auszahlung 670,70 €")
	noneWorseThan(t, g, Info)

	// The dental invoice: 931,59 × 50 % = 465,795 – the PKV rounds up as the app does.
	g = group(t, Run(Input{Settings: settings(), Invoice: gozInvoice(), Notices: []Notice{*pkvNotice()}}), KindNotice)
	want(t, one(t, g, "refund"), OK, "Erstattung 465,80 € wie erwartet (50 % von 931,59 €)")
}

func TestPKVCutAndMissingColumn(t *testing.T) {
	inv := &model.Invoice{ID: "apo", Provider: "Rosen-Apotheke", InvoiceDate: d("2026-10-21"), Amount: eur("23,92"), Claims: submitted()}
	g := group(t, Run(Input{Settings: settings(), Invoice: inv, Notices: []Notice{*pkvNotice()}}), KindNotice)
	f := one(t, g, "refund")
	want(t, f, Deviation, "Erstattung 9,46 € statt erwartet 11,96 € (50 % von 23,92 €) – Differenz -2,50 €")
	if !slices.Equal(f.Detail, []string{"Nicht erstattungsfähig: 5,00 € (erstattungsfähig 18,92 € von 23,92 €)",
		"Hinweis A3: Nicht verschreibungspflichtige Arzneimittel sind im Tarif BT50 nicht versichert."}) {
		t.Errorf("detail %q", f.Detail)
	}

	n := pkvNotice()
	n.Lines[1].Eligible = 0 // column not printed: no cut to show
	g = group(t, Run(Input{Settings: settings(), Invoice: inv, Notices: []Notice{*n}}), KindNotice)
	if f := one(t, g, "refund"); len(f.Detail) != 1 {
		t.Errorf("detail %q", f.Detail)
	}
}

func TestRefundAlreadyRecorded(t *testing.T) {
	inv := goaeInvoice()
	c := inv.Claims["bh"]
	c.Refunded = model.Mark{Date: d("2026-11-20")}
	c.Refunds = []model.Refund{{ID: "r1", Amount: eur("116,69"), DocID: "doc1"}}
	inv.Claims["bh"] = c
	g := group(t, Run(Input{Settings: settings(), Invoice: inv, Notices: []Notice{*beihilfeNotice()}}), KindNotice)
	// Booked already: the line is still found, and nothing to record.
	want(t, one(t, g, "notice_line"), OK, "Zeile 1 (12.10.2026, Dr. med. Hans Probe): Rechnungsbetrag 243,87 € wie in der App")
	if fs := with(g, "refund_recorded"); len(fs) != 0 {
		t.Errorf("refund_recorded: %+v", fs)
	}

	c.Refunds[0].Amount = eur("120,00")
	inv.Claims["bh"] = c
	g = group(t, Run(Input{Settings: settings(), Invoice: inv, Notices: []Notice{*beihilfeNotice()}}), KindNotice)
	want(t, one(t, g, "refund_recorded"), Check,
		"In der App sind 120,00 € als Erstattung von Beihilfestelle Musterland erfasst, im Bescheid stehen 116,69 €")
}

func TestNoticeReadingErrors(t *testing.T) {
	n := beihilfeNotice()
	n.PaidOut = eur("515,70")           // a digit misread
	n.Lines[0].Eligible = eur("253,38") // more than billed
	n.Lines[0].Refund = eur("161,69")   // not 50 % of eligible
	g := group(t, Run(Input{Settings: settings(), Invoice: goaeInvoice(), Notices: []Notice{*n}}), KindNotice)
	vals := with(g, "notice_values")
	if len(vals) != 2 {
		t.Fatalf("notice_values:%s", dump(g))
	}
	want(t, vals[0], Check, "Zeile 1 (12.10.2026, Dr. med. Hans Probe): beihilfefähig 253,38 € ist mehr als der Rechnungsbetrag 243,87 € – falsch gelesen?")
	want(t, vals[1], Check, "Zeile 1 (12.10.2026, Dr. med. Hans Probe): Erstattung 161,69 € ist nicht 50 % von 253,38 € (126,69 €) – falsch gelesen?")
	want(t, one(t, g, "notice_sum"), Check,
		"Erstattungen 661,70 € − Abzüge 100,00 €, Auszahlung 515,70 € (Differenz +46,00 €) – fehlt eine Zeile, oder ist ein Betrag falsch gelesen?")
}

func TestNoticePayer(t *testing.T) {
	n := beihilfeNotice()
	n.PayerID = ""
	g := group(t, Run(Input{Settings: settings(), Invoice: goaeInvoice(), Notices: []Notice{*n}}), KindNotice)
	want(t, one(t, g, "notice_payer"), Check, "Kostenträger des Bescheids nicht erkannt („Beihilfestelle Musterland“) – bitte zuordnen")
	if g.Title != "Bescheid vom 18.11.2026 – Beihilfestelle Musterland" {
		t.Errorf("title %q", g.Title)
	}
	one(t, g, "notice_sum") // the Bescheid in itself is still checked

	s := settings()
	s.Payers = append(s.Payers, model.Payer{ID: "alt", Kind: model.PKV, Name: "Alte Versicherung"})
	n.PayerID = "alt"
	g = group(t, Run(Input{Settings: s, Invoice: goaeInvoice(), Notices: []Notice{*n}}), KindNotice)
	want(t, one(t, g, "notice_payer"), Check, "Laut App hat Alte Versicherung an dieser Rechnung keinen Anteil")
}

func TestInvoiceNotInBescheid(t *testing.T) {
	inv := &model.Invoice{ID: "other", Provider: "Dr. Otto Anders", InvoiceDate: d("2026-08-03"), Amount: eur("88,00")}
	g := group(t, Run(Input{Settings: settings(), Invoice: inv, Notices: []Notice{*beihilfeNotice()}}), KindNotice)
	f := one(t, g, "notice_line")
	want(t, f, Check, "Keine Zeile des Bescheids passt zu dieser Rechnung (03.08.2026, 88,00 €)")
	if len(f.Detail) != 4 || f.Detail[1] != "Zeile 2 (21.10.2026, Rosen-Apotheke): 23,92 €" {
		t.Errorf("detail %q", f.Detail)
	}
	if len(with(g, "refund")) != 0 {
		t.Errorf("refund without a line:%s", dump(g))
	}
}

func TestRateDiffersFromSettings(t *testing.T) {
	n := pkvNotice()
	n.Lines[0] = line("2026-10-12", "Dr. med. Hans Probe", "243,87", "243,87", 70, "170,71")
	n.PaidOut += eur("48,77")
	g := group(t, Run(Input{Settings: settings(), Invoice: goaeInvoice(), Notices: []Notice{*n}}), KindNotice)
	f := one(t, g, "refund")
	want(t, f, Deviation, "Erstattung 170,71 € statt erwartet 121,94 € (50 % von 243,87 €) – Differenz +48,77 €")
	if !slices.Contains(f.Detail, "Satz im Bescheid 70 %, in der App 50 %") {
		t.Errorf("detail %q", f.Detail)
	}
	want(t, one(t, g, "notice_rate"), Check,
		"Bemessungssatz im Bescheid 70 %, in der App 50 % – stimmt der Versicherungsschutz in den Einstellungen?")
	one(t, g, "notice_sum")
}

// A Bescheid may list a dental invoice as fee and lab costs: the matcher
// finds both lines.
func TestInvoiceSplitOverTwoLines(t *testing.T) {
	n := pkvNotice()
	n.Lines = []NoticeLine{
		line("2026-11-03", "Zahnarztpraxis Dr. Beispiel", "435,86", "435,86", 50, "217,93"),
		line("2026-11-03", "Zahnarztpraxis Dr. Beispiel", "495,73", "495,73", 50, "247,87"),
		line("2026-10-12", "Dr. med. Hans Probe", "243,87", "243,87", 50, "121,94"),
	}
	n.PaidOut = eur("512,74")
	g := group(t, Run(Input{Settings: settings(), Invoice: gozInvoice(), Notices: []Notice{*n}}), KindNotice)
	want(t, one(t, g, "notice_line"), OK, "Zeilen 1 und 2 zusammen: Rechnungsbetrag 931,59 € wie in der App")
	want(t, one(t, g, "refund"), OK, "Erstattung 465,80 € wie erwartet (50 % von 931,59 €)")
}

func TestRefundWithinTolerance(t *testing.T) {
	n := pkvNotice()
	n.Lines[0].Refund = eur("121,44")
	n.Lines[0].Rate = 0 // not printed
	g := group(t, Run(Input{Settings: settings(), Invoice: goaeInvoice(), Notices: []Notice{*n}}), KindNotice)
	want(t, one(t, g, "refund"), Info,
		"Erstattung 121,44 € statt erwartet 121,94 € (50 % von 243,87 €) – Differenz -0,50 €, unter der Toleranz von 1,00 €")
}

// --- invoice and cost plan -----------------------------------------------------------------

func TestCostPlan(t *testing.T) {
	r := Run(Input{Settings: settings(), Invoice: gozInvoice(), Bill: gozBill(), Plans: []Plan{*costPlan()}})
	g := group(t, r, KindPlan)
	if g.Title != "Heil- und Kostenplan vom 15.09.2026 – Ruth Beispiel" || g.Status != Info {
		t.Errorf("title %q, status %s", g.Title, g.Status)
	}
	sums := with(g, "plan_sum")
	if len(sums) != 2 || sums[0].Text != "Summe der Positionen 336,35 € = Honorar 336,35 €" ||
		sums[1].Text != "Honorar + Material- und Laborkosten 786,35 € = Gesamtbetrag 786,35 €" {
		t.Errorf("plan sums:%s", dump(g))
	}
	want(t, one(t, g, "plan_date"), OK, "Kostenplan vom 15.09.2026, vor dem Behandlungsbeginn am 06.10.2026")
	want(t, one(t, g, "plan_positions"), OK, "5 Positionen wie im Kostenplan")
	f := one(t, g, "plan_extra")
	want(t, f, Info, "Berechnet, aber nicht im Kostenplan: 3 Positionen, zusammen 120,49 €")
	wantDetail := []string{
		"Nr. 2030 (Zahn 36) – Besondere Maßnahmen beim Präparieren oder Füllen von…: 8,41 €",
		"Nr. Ä1 – Beratung – auch mittels Fernsprecher: 10,72 €",
		"Nr. 1040 – Professionelle Zahnreinigung, je Zahn, 28×: 101,36 €",
	}
	if !slices.Equal(f.Detail, wantDetail) {
		t.Errorf("extra detail\n%q\nwant\n%q", f.Detail, wantDetail)
	}
	f = one(t, g, "plan_missing")
	want(t, f, Info, "Im Kostenplan, aber nicht berechnet: 1 Position, zusammen 20,98 €")
	if !slices.Equal(f.Detail, []string{"Nr. Ä925a (Zahn 36) – Röntgendiagnostik der Zähne, bis zwei Einzelaufnahmen: 20,98 €"}) {
		t.Errorf("missing detail %q", f.Detail)
	}
	want(t, one(t, g, "plan_fee"), Info, "Honorar 435,86 € – geplant 336,35 € (+99,51 €, +29,6 %)")
	want(t, one(t, g, "plan_expenses"), Info, "Material- und Laborkosten 495,73 € – geplant 450,00 € (+45,73 €, +10,2 %)")
	want(t, one(t, g, "plan_total"), Info, "Gesamtbetrag 931,59 € – geplant 786,35 € (+145,24 €, +18,5 %)")
	// 2 × 465,80 = 931,60: each payer rounds up – no own share, and no "-0,01 €".
	want(t, one(t, g, "plan_own_share"), Info, "Eigenanteil voraussichtlich 0,00 € (Rechnung 931,59 € − erwartete Erstattungen 931,60 €)")
	if r.Status != Info {
		t.Errorf("status %s: differences to a plan are information", r.Status)
	}
}

func TestCostPlanVariants(t *testing.T) {
	p := costPlan()
	p.Date = d("2026-10-10") // made after the treatment began
	p.Positions[2].Amount = eur("200,00")
	p.Fee, p.Total = eur("319,29"), eur("769,29")
	inv := gozInvoice()
	inv.Claims["bh"] = model.Claim{Refunds: []model.Refund{{ID: "r", Amount: eur("366,65")}}}
	inv.Claims["pkv"] = model.Claim{Refunds: []model.Refund{{ID: "s", Amount: eur("465,80")}}}
	g := group(t, Run(Input{Settings: settings(), Invoice: inv, Bill: gozBill(), Plans: []Plan{*p}}), KindPlan)
	want(t, one(t, g, "plan_date"), Check, "Kostenplan vom 10.10.2026 – die Behandlung begann schon am 06.10.2026")
	changed := with(g, "plan_positions")
	if len(changed) != 2 || changed[1].Text != "Anders als im Kostenplan: 1 Position" ||
		!slices.Equal(changed[1].Detail, []string{"Nr. 2210 (Zahn 36): berechnet 1× 217,06 €, geplant 1× 200,00 € (+17,06 €)"}) {
		t.Errorf("plan_positions:%s", dump(g))
	}
	want(t, one(t, g, "plan_own_share"), Info, "Eigenanteil bisher 99,14 € (Rechnung 931,59 € − Erstattungen 832,45 €)")

	g = group(t, Run(Input{Settings: settings(), Invoice: gozInvoice(), Plans: []Plan{*costPlan()}}), KindPlan)
	want(t, one(t, g, "needs_bill"), Info, "Für den Vergleich mit dem Kostenplan wird die Rechnung selbst gebraucht – bitte die Rechnung mit auswählen")
	if len(with(g, "plan_sum")) != 2 || len(with(g, "plan_fee")) != 0 {
		t.Errorf("without the invoice only the plan in itself:%s", dump(g))
	}
}

// A cost estimate without a fee line (a therapist's), and a plan without
// the lab costs.
func TestCostEstimates(t *testing.T) {
	b := physioBill()
	est := &Plan{Date: d("2026-09-15"), Total: eur("311,00"), Positions: slices.Clone(b.Positions)}
	g := group(t, Run(Input{Settings: settings(), Invoice: physioInvoice(), Bill: b, Plans: []Plan{*est}}), KindPlan)
	want(t, one(t, g, "plan_sum"), OK, "Summe der Positionen 311,00 € = Gesamtbetrag 311,00 €")
	want(t, one(t, g, "plan_positions"), OK, "2 Positionen wie im Kostenplan") // KG and WP, summed over the days
	want(t, one(t, g, "plan_fee"), OK, "Honorar 311,00 € wie geplant")
	want(t, one(t, g, "plan_total"), OK, "Gesamtbetrag 311,00 € wie geplant")
	if len(with(g, "plan_expenses")) != 0 || g.Title != "Heil- und Kostenplan vom 15.09.2026" {
		t.Errorf("title %q:%s", g.Title, dump(g))
	}

	p := costPlan()
	p.Expenses, p.Total = 0, eur("336,35")
	g = group(t, Run(Input{Settings: settings(), Invoice: gozInvoice(), Bill: gozBill(), Plans: []Plan{*p}}), KindPlan)
	want(t, one(t, g, "plan_expenses"), Info, "Material- und Laborkosten 495,73 € – im Kostenplan nicht vorgesehen")
	if fs := with(g, "plan_sum"); len(fs) != 2 || fs[1].Text != "Honorar 336,35 € = Gesamtbetrag 336,35 €" {
		t.Errorf("plan_sum:%s", dump(g))
	}
}

func TestNoticeWithGaps(t *testing.T) {
	n := beihilfeNotice()
	n.PaidOut = 0
	n.Notes = n.Notes[1:] // the text of note 1 not read
	n.Deductions = nil
	g := group(t, Run(Input{Settings: settings(), Invoice: goaeInvoice(), Notices: []Notice{*n}}), KindNotice)
	want(t, one(t, g, "notice_sum"), Info, "Auszahlungsbetrag nicht gelesen – die Summe lässt sich nicht prüfen")
	if f := one(t, g, "refund"); !slices.Contains(f.Detail, "Hinweis 1 (Text nicht gelesen)") {
		t.Errorf("detail %q", f.Detail)
	}
	if len(with(g, "notice_deduction")) != 0 {
		t.Errorf("deduction without one:%s", dump(g))
	}

	n = pkvNotice()
	n.Lines[0].Billed = 0 // column not printed
	n.Deductions, n.PaidOut = nil, eur("745,70")
	g = group(t, Run(Input{Settings: settings(), Invoice: goaeInvoice(), Notices: []Notice{*n}}), KindNotice)
	want(t, one(t, g, "notice_line"), OK, "Zeile 1 (12.10.2026, Dr. med. Hans Probe) gehört zu dieser Rechnung")
	want(t, one(t, g, "notice_sum"), OK, "Erstattungen 745,70 € = Auszahlung 745,70 €")
}

// --- invoice and prescription --------------------------------------------------------------

func TestPrescription(t *testing.T) {
	r := Run(Input{Settings: settings(), Invoice: physioInvoice(), Bill: physioBill(), Prescriptions: []Prescription{*prescription()}})
	g := group(t, r, KindPrescription)
	if g.Title != "Verordnung vom 10.09.2026 – Klara Beispiel" {
		t.Errorf("title %q", g.Title)
	}
	counts := with(g, "rx_count")
	if len(counts) != 2 {
		t.Fatalf("rx_count:%s", dump(g))
	}
	want(t, counts[0], Check, "Krankengymnastik: 7 Behandlungen berechnet, 6 verordnet")
	if !slices.Equal(counts[0].Detail, []string{"7. Behandlung am 13.10.2026"}) {
		t.Errorf("detail %q", counts[0].Detail)
	}
	want(t, counts[1], OK, "Fango: 6 von 6 verordneten Behandlungen berechnet")
	want(t, one(t, g, "rx_start"), OK,
		"Erste Behandlung am 22.09.2026, nach der Verordnung vom 10.09.2026 und innerhalb ihrer Gültigkeit (bis 08.10.2026)")
	if len(with(g, "rx_extra")) != 0 || r.Status != Check {
		t.Errorf("status %s:%s", r.Status, dump(g))
	}
}

func TestPrescriptionVariants(t *testing.T) {
	run := func(rx *Prescription, b *Bill) Group {
		return group(t, Run(Input{Settings: settings(), Invoice: physioInvoice(), Bill: b, Prescriptions: []Prescription{*rx}}), KindPrescription)
	}
	rx := prescription()
	rx.Date = d("2026-09-25")
	want(t, one(t, run(rx, physioBill()), "rx_start"), Check, "Erste Behandlung am 22.09.2026, vor der Verordnung vom 25.09.2026")

	rx = prescription()
	rx.ValidUntil = d("2026-09-20")
	want(t, one(t, run(rx, physioBill()), "rx_start"), Check, "Erste Behandlung am 22.09.2026 – die Verordnung galt bis 20.09.2026")

	rx = prescription()
	rx.Remedies = []Remedy{{Name: "KG", Count: 10}, {Name: "Wärmepackung"}, {Name: "Manuelle Lymphdrainage", Count: 6}}
	b := physioBill()
	b.Positions = append(b.Positions, pos("2026-10-13", "", "HB", "Hausbesuch", 1, "20,00", 1))
	g := run(rx, b)
	var got []string
	for _, f := range with(g, "rx_count") {
		got = append(got, string(f.Status)+" "+f.Text)
	}
	wantTexts := []string{
		"ok KG: 7 von 10 verordneten Behandlungen berechnet",
		"info Wärmepackung: Anzahl auf der Verordnung nicht gelesen – 6 Behandlungen berechnet",
		"info Manuelle Lymphdrainage: 6× verordnet, aber nicht berechnet",
	}
	if !slices.Equal(got, wantTexts) {
		t.Errorf("rx_count:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(wantTexts, "\n"))
	}
	f := one(t, g, "rx_extra")
	want(t, f, Info, "Nicht auf der Verordnung: 1 Position, zusammen 20,00 €")
	if !slices.Equal(f.Detail, []string{"Nr. HB am 13.10.2026 – Hausbesuch: 20,00 €"}) {
		t.Errorf("detail %q", f.Detail)
	}

	g = group(t, Run(Input{Settings: settings(), Invoice: physioInvoice(), Prescriptions: []Prescription{*prescription()}}), KindPrescription)
	one(t, g, "needs_bill")

	g = run(&Prescription{}, physioBill())
	want(t, one(t, g, "rx_count"), Info, "Keine Heilmittel auf der Verordnung gelesen – die Anzahl lässt sich nicht prüfen")
	if g.Title != "Verordnung" {
		t.Errorf("title %q", g.Title)
	}
}

// Remedies are found by whole words, with the usual abbreviations.
func TestRemedyWords(t *testing.T) {
	for _, tc := range []struct {
		remedy, code, text string
		want               bool
	}{
		{"Krankengymnastik", "KG", "Krankengymnastik, Einzelbehandlung", true},
		{"KG", "", "Krankengymnastik", true},
		{"Fango", "WP", "Wärmepackung (Fango)", true},
		{"Krankengymnastik am Gerät", "KGG", "", true},
		{"Manuelle Therapie", "MLD", "Manuelle Lymphdrainage", false}, // one word in common is not enough
		{"Fango", "", "Fangopackung", false},                          // whole words only
		{"KG", "KT", "Kälteanwendung", false},
		{"", "KG", "Krankengymnastik", false},
	} {
		got := bestRemedy([]Remedy{{Name: tc.remedy}}, Position{Code: tc.code, Text: tc.text}) == 0
		if got != tc.want {
			t.Errorf("%q in %q %q: %v, want %v", tc.remedy, tc.code, tc.text, got, tc.want)
		}
	}
	// Of two that fit, the more specific one.
	rs := []Remedy{{Name: "Krankengymnastik"}, {Name: "Krankengymnastik am Gerät"}}
	if i := bestRemedy(rs, Position{Code: "KGG", Text: "Krankengymnastik am Gerät"}); i != 1 {
		t.Errorf("KGG: remedy %d, want 1", i)
	}
	if i := bestRemedy(rs, Position{Code: "KG", Text: "Krankengymnastik"}); i != 0 {
		t.Errorf("KG: remedy %d, want 0", i)
	}
}

// --- helpers and the result ----------------------------------------------------------------

func TestFormatting(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{pct(eur("99,51"), eur("336,35")), "+29,6 %"},
		{pct(eur("-5,25"), eur("121,94")), "-4,3 %"},
		{pct(eur("0,01"), eur("1000")), "+0,0 %"},
		{signed(eur("99,51")), "+99,51 €"},
		{signed(eur("-5,25")), "-5,25 €"},
		{rateDE(money.Percent(30.5)), "30,5 %"},
		{span(d("2026-09-08"), d("2026-09-29")), "08.09.2026–29.09.2026"},
		{span(d("2026-09-08"), d("2026-09-08")), "08.09.2026"},
		{list([]string{"a", "b", "c"}), "a, b und c"},
		{list([]string{"a"}), "a"},
		{codeKey("0090"), "90"},
		{codeKey(" ä925a"), "Ä925A"},
		{codeKey("3585.H1"), "3585.H1"},
		{codeKey("000"), "000"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

func TestSameProvider(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"Hans Probe", "Dr. med. Hans Probe", true},
		{"Dr. med. Hans Probe, Facharzt für Innere Medizin", "Hans Probe", true},
		{"Praxis Dr. Müller", "Mueller", true},
		{"Rosen-Apotheke", "Linden-Apotheke", false},
		{"Hans Probe", "Lotte Probe", false},
		{"Dr. med.", "Dr. med. Hans Probe", false}, // nothing left to compare
	} {
		if got := sameProvider(tc.a, tc.b); got != tc.want {
			t.Errorf("sameProvider(%q, %q) = %v", tc.a, tc.b, got)
		}
	}
}

func TestResult(t *testing.T) {
	r := Run(Input{Settings: settings(), Invoice: goaeInvoice()})
	if b, _ := json.Marshal(r); string(b) != `{"status":"ok","groups":[]}` {
		t.Errorf("nothing to check: %s", b)
	}
	r = Run(Input{Settings: settings(), Invoice: physioInvoice(), Bill: physioBill(),
		Notices: []Notice{*beihilfeNotice(), *pkvNotice()}, Prescriptions: []Prescription{*prescription()}})
	var kinds []Kind
	for _, g := range r.Groups {
		kinds = append(kinds, g.Kind)
	}
	if !slices.Equal(kinds, []Kind{KindBill, KindNotice, KindNotice, KindPrescription}) || r.Status != Deviation {
		t.Errorf("groups %v, status %s", kinds, r.Status)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var back Result
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Status != r.Status || len(back.Groups) != 4 || back.Groups[1].Findings[0].Text != r.Groups[1].Findings[0].Text {
		t.Errorf("JSON round trip: %s", b)
	}
	if !strings.Contains(string(b), `"status":"deviation"`) {
		t.Errorf("JSON: %s", b)
	}
}
