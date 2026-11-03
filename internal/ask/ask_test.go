package ask

import (
	"slices"
	"strings"
	"testing"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/match"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

var today = civil.MustParse("2026-11-03")

func day(s string) model.Mark { return model.Mark{Date: civil.MustParse(s)} }

func refunded(amount money.Cents, date string) model.Claim {
	return model.Claim{Submitted: day(date), Refunded: day(date), Refunds: []model.Refund{{ID: "r", Amount: amount, Date: civil.MustParse(date)}}}
}

// Made-up invoices: Beihilfe Bund pays 70 %, Muster PKV 30 %.
func testData() Data {
	s := &model.Settings{
		Payers: []model.Payer{{ID: "bh", Kind: model.Beihilfe, Name: "Beihilfe Bund", ClaimDeadlineYears: 3},
			{ID: "pkv", Kind: model.PKV, Name: "Muster PKV"}},
		Coverage: []model.CoveragePeriod{{From: civil.MustParse("2020-01-01"),
			Shares: []model.Share{{PayerID: "bh", Rate: money.Percent(70)}, {PayerID: "pkv", Rate: money.Percent(30)}}}},
		Years: []model.YearSetting{{Year: 2026, PayerID: "pkv", ExpectedPremiumRefund: 60000}},
	}
	inv := func(id, provider string, cat model.Category, invoiceDate, from string, amount money.Cents, paid bool, claims map[string]model.Claim) model.Invoice {
		i := model.Invoice{ID: id, Provider: provider, Category: cat, InvoiceDate: civil.MustParse(invoiceDate),
			ServiceFrom: civil.MustParse(from), Amount: amount, Claims: claims}
		if paid {
			i.Paid = day(invoiceDate)
		}
		return i
	}
	return Data{Settings: s, Invoices: []model.Invoice{
		inv("A", "Dr. med. Anna Muster", model.CatDoctor, "2026-03-14", "2026-03-03", 28640, true, map[string]model.Claim{
			"bh": refunded(20048, "2026-04-20"), "pkv": refunded(8592, "2026-04-22")}),
		inv("B", "Zahnarztpraxis Dr. Beispiel", model.CatDentist, "2026-05-20", "2026-05-10", 100000, true, map[string]model.Claim{
			"bh": {Submitted: day("2026-06-01")}}),
		inv("C", "Physiotherapie Lotte Probe", model.CatTherapy, "2025-09-30", "2025-09-15", 31200, false, map[string]model.Claim{
			"bh": refunded(20000, "2025-11-10"), "pkv": {Held: true}}),
		inv("D", "Linden-Apotheke", model.CatPharmacy, "2023-12-01", "2023-12-01", 3000, true, map[string]model.Claim{
			"bh": {Skip: true}, "pkv": refunded(1200, "2024-01-15")}),
		inv("E", "Gelöscht", model.CatDoctor, "2026-01-10", "2026-01-10", 99900, true, nil),
		inv("F", "Radiologie Mitte", model.CatDoctor, "2026-10-20", "2026-10-12", 15000, false, map[string]model.Claim{
			"bh": {Clarify: model.Mark{Date: civil.MustParse("2026-10-25"), Note: "Faktor zu hoch?"}}}),
	}}
}

// run answers q from testData with invoice E deleted.
func run(t *testing.T, q Query) Result {
	t.Helper()
	d := testData()
	d.Invoices[4].Deleted = true
	return Run(d, q, today)
}

func filters(kv ...string) []Filter {
	var out []Filter
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, Filter{kv[i], kv[i+1]})
	}
	return out
}

func TestQueries(t *testing.T) {
	cases := []struct {
		question string
		q        Query
		answer   string
		invoices []string
	}{
		{"Wie viele Rechnungen habe ich?", Query{Kind: KindCount}, "5 Rechnungen", []string{"F", "B", "A", "C", "D"}},
		{"Wie viele Rechnungen aus 2026 sind noch offen?", Query{Kind: KindCount, Years: []int{2026},
			Filters: filters(FieldStatus, StatusNotRefunded)}, "2 Rechnungen", []string{"F", "B"}},
		{"Wo hat mir die Beihilfe zu wenig erstattet?", Query{Kind: KindList, Measure: MeasureDiff,
			Filters: filters(FieldPayer, "Beihilfe Bund", FieldDeviation, TooLittle)},
			"1 Rechnung, Differenz (IST − SOLL) zusammen -18,40 €", []string{"C"}},
		{"Wo hat die PKV mehr erstattet als erwartet?", Query{Kind: KindList,
			Filters: filters(FieldPayer, "Muster PKV", FieldDeviation, TooMuch)}, "1 Rechnung, Rechnungsbetrag zusammen 30,00 €", []string{"D"}},
		{"Was hat die PKV 2026 insgesamt erstattet?", Query{Kind: KindSum, Measure: MeasureActual, Years: []int{2026},
			Filters: filters(FieldPayer, "Muster PKV")}, "Erstattet (IST): 85,92 € (1 Rechnung)", []string{"A"}},
		{"Welche Rechnungen sind noch nicht bezahlt?", Query{Kind: KindList, Filters: filters(FieldPaid, No)},
			"2 Rechnungen, Rechnungsbetrag zusammen 462,00 €", []string{"F", "C"}},
		{"Wie viel habe ich 2026 beim Zahnarzt ausgegeben?", Query{Kind: KindSum, Measure: MeasureAmount, Years: []int{2026},
			Filters: filters(FieldCategory, "zahnarzt")}, "Rechnungsbetrag: 1.000,00 € (1 Rechnung)", []string{"B"}},
		{"Welche Rechnungen habe ich noch nicht eingereicht?", Query{Kind: KindList, Filters: filters(FieldStatus, StatusOpen)},
			"3 Rechnungen, Rechnungsbetrag zusammen 1.462,00 €", []string{"F", "B", "C"}},
		{"Welche Rechnungen halte ich bei der PKV zurück?", Query{Kind: KindList,
			Filters: filters(FieldPayer, "Muster PKV", FieldStatus, StatusHeld)}, "1 Rechnung, Rechnungsbetrag zusammen 312,00 €", []string{"C"}},
		{"Welche Erstattungen stehen noch aus?", Query{Kind: KindList, Filters: filters(FieldStatus, StatusSubmitted)},
			"1 Rechnung, Rechnungsbetrag zusammen 1.000,00 €", []string{"B"}},
		{"Bei welchen Rechnungen läuft die Antragsfrist bald ab?", Query{Kind: KindList, Filters: filters(FieldDeadline, Yes)},
			"Keine Rechnung passt dazu.", []string{}},
		// D: the PKV refunded 3 € more than the SOLL – above the tolerance, so Klärungsbedarf is suggested (as in the list).
		{"Wo ist noch Klärungsbedarf?", Query{Kind: KindList, Filters: filters(FieldClarify, Yes)},
			"3 Rechnungen, Rechnungsbetrag zusammen 492,00 €", []string{"F", "C", "D"}},
		{"Zeig mir alle Rechnungen von Dr. Muster.", Query{Kind: KindList, Filters: filters(FieldProvider, "Dr. Muster")},
			"1 Rechnung, Rechnungsbetrag zusammen 286,40 €", []string{"A"}},
		{"Wie hoch war mein Eigenanteil 2025?", Query{Kind: KindSum, Measure: MeasureOwnShare, Years: []int{2025}},
			"Eigenanteil: 112,00 € (1 Rechnung)", []string{"C"}},
		{"Welche Rechnungen aus dem letzten Monat?", Query{Kind: KindList, Filters: filters(FieldFrom, "2026-10-01", FieldTo, "2026-10-31")},
			"1 Rechnung, Rechnungsbetrag zusammen 150,00 €", []string{"F"}},
		{"SOLL und IST der Beihilfe 2026", Query{Kind: KindSum, Years: []int{2026}, Filters: filters(FieldPayer, "Beihilfe Bund")},
			"SOLL 1.005,48 €, IST 200,48 € (3 Rechnungen)", []string{"F", "B", "A"}},
		{"Wie viel Geld ist bei der Beihilfe eingereicht, aber noch nicht erstattet?", Query{Kind: KindSum, Measure: MeasureOwed,
			Filters: filters(FieldPayer, "Beihilfe Bund", FieldStatus, StatusSubmitted)}, "Noch offen: 700,00 € (1 Rechnung)", []string{"B"}},
		{"Summe der Differenzen bei der Beihilfe 2025", Query{Kind: KindSum, Measure: MeasureDiff, Years: []int{2025},
			Filters: filters(FieldPayer, "Beihilfe Bund")}, "Differenz (IST − SOLL): -18,40 € (1 Rechnung)", []string{"C"}},
		{"Rechnungen der AOK", Query{Kind: KindList, Filters: filters(FieldPayer, "AOK")}, "Keine Rechnung passt dazu.", []string{}},
		{"Wie viele Rechnungen hatte ich 2019?", Query{Kind: KindCount, Years: []int{2019}}, "Keine Rechnung passt dazu.", []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.question, func(t *testing.T) {
			r := run(t, tc.q)
			if r.Answer != tc.answer || !slices.Equal(r.Invoices, tc.invoices) {
				t.Errorf("%s\n got %q %v\nwant %q %v", tc.q, r.Answer, r.Invoices, tc.answer, tc.invoices)
			}
		})
	}
}

func TestDeadlineSoon(t *testing.T) {
	d := testData()
	// Not yet submitted to the Beihilfe: three years after the invoice date (2023-12-01) – in four weeks.
	d.Invoices[3].Claims["bh"] = model.Claim{}
	r := Run(d, Query{Kind: KindList, Filters: filters(FieldDeadline, Yes)}, today)
	if !slices.Equal(r.Invoices, []string{"D"}) {
		t.Errorf("%v", r.Invoices)
	}
}

func TestGroups(t *testing.T) {
	r := run(t, Query{Kind: KindSum, Measure: MeasureAmount, GroupBy: GroupYear})
	want := []Group{{"2023", 1, "30,00 €"}, {"2025", 1, "312,00 €"}, {"2026", 3, "1.436,40 €"}}
	if !slices.Equal(r.Groups, want) {
		t.Errorf("by year %+v", r.Groups)
	}
	// The most expensive first.
	r = run(t, Query{Kind: KindSum, Measure: MeasureAmount, Years: []int{2026}, GroupBy: GroupProvider})
	if r.Groups[0].Label != "Zahnarztpraxis Dr. Beispiel" || r.Groups[0].Value != "1.000,00 €" || len(r.Groups) != 3 {
		t.Errorf("by provider %+v", r.Groups)
	}
	r = run(t, Query{Kind: KindSum, Measure: MeasureActual, Years: []int{2026}, Filters: filters(FieldPayer, "Beihilfe Bund"), GroupBy: GroupMonth})
	// Only refunded invoices say something about IST.
	if !slices.Equal(r.Groups, []Group{{"März 2026", 1, "200,48 €"}}) {
		t.Errorf("by month %+v", r.Groups)
	}
	// By payer: an invoice counts for each payer it has a claim with (by the coverage; D is not
	// claimed from the Beihilfe).
	r = run(t, Query{Kind: KindCount, GroupBy: GroupPayer})
	if !slices.Equal(r.Groups, []Group{{"Muster PKV", 5, ""}, {"Beihilfe Bund", 4, ""}}) {
		t.Errorf("by payer %+v", r.Groups)
	}
	r = run(t, Query{Kind: KindSum, GroupBy: GroupCategory, Years: []int{2026}})
	if r.Groups[0].Label != "Arzt" || r.Groups[0].Value != "SOLL 436,40 € · IST 286,40 €" {
		t.Errorf("overview by category %+v", r.Groups)
	}
}

func TestOutstanding(t *testing.T) {
	r := run(t, Query{Kind: KindOutstanding})
	if r.Answer != "Noch ausstehend: 1.165,40 € – Beihilfe Bund 823,40 €, Muster PKV 342,00 €" {
		t.Errorf("answer %q", r.Answer)
	}
	// The largest debt first.
	if !slices.Equal(r.Invoices, []string{"B", "F", "C", "D"}) {
		t.Errorf("invoices %v", r.Invoices)
	}
	figures := map[string]string{}
	for _, f := range r.Figures {
		figures[f.Label] = f.Value
	}
	if figures["Beihilfe Bund: eingereicht, Erstattung offen"] != "700,00 €" || figures["Beihilfe Bund: noch nicht eingereicht"] != "105,00 €" ||
		figures["Beihilfe Bund: Abweichungen erstatteter Rechnungen"] != "18,40 €" || figures["Muster PKV: Abweichungen erstatteter Rechnungen"] != "-3,00 €" {
		t.Errorf("figures %v", figures)
	}
	r = run(t, Query{Kind: KindOutstanding, Filters: filters(FieldPayer, "Muster PKV")})
	if r.Answer != "Noch ausstehend: 342,00 €" || len(r.Groups) != 1 {
		t.Errorf("one payer %q %+v", r.Answer, r.Groups)
	}
}

func TestPremium(t *testing.T) {
	r := run(t, Query{Kind: KindPremium})
	if !strings.HasPrefix(r.Answer, "Muster PKV (Leistungsjahr 2026): Nicht einreichen lohnt sich") ||
		!strings.Contains(r.Answer, "schon eine Rechnung eingereicht oder erstattet") || len(r.Figures) != 2 {
		t.Errorf("%q %+v", r.Answer, r.Figures)
	}
	r = run(t, Query{Kind: KindPremium, Years: []int{2025}})
	if !strings.HasPrefix(r.Answer, "Für 2025 ist keine erwartete Beitragsrückerstattung eingetragen") {
		t.Errorf("%q", r.Answer)
	}
}

func TestUnsupported(t *testing.T) {
	r := run(t, Query{Kind: KindUnsupported, Reason: "allgemeine Frage zur Beihilfe."})
	if r.Answer != "Darauf kann die App nicht antworten – sie rechnet nur mit den eigenen Rechnungen und Erstattungen. (allgemeine Frage zur Beihilfe)" {
		t.Errorf("%q", r.Answer)
	}
}

func TestNoData(t *testing.T) {
	r := Run(Data{Settings: &model.Settings{}}, Query{Kind: KindSum, Measure: MeasureAmount}, today)
	if r.Answer != "Keine Rechnung passt dazu." || r.Invoices == nil {
		t.Errorf("%+v", r)
	}
	if r := Run(Data{Settings: &model.Settings{}}, Query{Kind: KindOutstanding}, today); !strings.HasPrefix(r.Answer, "Nichts") {
		t.Errorf("%+v", r)
	}
}

func TestDescribe(t *testing.T) {
	q := Query{Kind: KindList, Measure: MeasureDiff, Years: []int{2025, 2026}, GroupBy: GroupMonth,
		Filters: filters(FieldPayer, "Beihilfe Bund", FieldDeviation, TooLittle, FieldProvider, "Dr. Muster", FieldFrom, "2026-10-01",
			FieldCategory, "heilmittel", FieldStatus, StatusSubmitted)}
	want := "Liste mit Differenz (IST − SOLL) · 2025 und 2026 · Beihilfe Bund · zu wenig erstattet · Behandler „Dr. Muster“ · " +
		"ab 01.10.2026 · Heilmittel · Erstattung offen · nach Monat"
	if q.String() != want {
		t.Errorf("\n got %s\nwant %s", q, want)
	}
	if c := Describe(Query{Kind: KindSum}); len(c) != 1 || c[0].Text != "SOLL und IST" || c[0].Key != "typ" {
		t.Errorf("%+v", c)
	}
}

func TestNameMatches(t *testing.T) {
	for _, c := range []struct {
		want, name string
		ok         bool
	}{
		{"Dr. Muster", "Praxis Dr. med. Anna Muster", true},
		{"muster", "Dr. Anna Müller", false},
		{"Müller", "Dr. Anna Mueller", true},
		{"Radiologie", "Radiologie Mitte MVZ", true},
		{"Zahnarzt", "Zahnarztpraxis Dr. Beispiel", true},
		{"Dr.", "Dr. Muster", false},
	} {
		if nameMatches(match.NameWords(c.want), c.name) != c.ok {
			t.Errorf("%q in %q: want %v", c.want, c.name, c.ok)
		}
	}
}

func TestRunFromReview(t *testing.T) {
	// "ohne Klärungsbedarf": no claim needs clarifying (C and D have deviations, F is marked).
	if r := run(t, Query{Kind: KindList, Filters: filters(FieldClarify, No)}); !slices.Equal(r.Invoices, []string{"B", "A"}) {
		t.Errorf("without Klärungsbedarf %v", r.Invoices)
	}
	// An invoice amount has no share per payer: SOLL and IST instead (not the amount twice).
	r := run(t, Query{Kind: KindSum, Measure: MeasureAmount, GroupBy: GroupPayer})
	if len(r.Groups) != 2 || !strings.HasPrefix(r.Groups[0].Value, "SOLL ") {
		t.Errorf("amount by payer %+v", r.Groups)
	}
	// IST by payer counts the refunded claims only (the PKV held C back).
	r = run(t, Query{Kind: KindSum, Measure: MeasureActual, Years: []int{2025}, GroupBy: GroupPayer})
	if !slices.Equal(r.Groups, []Group{{"Beihilfe Bund", 1, "200,00 €"}}) {
		t.Errorf("IST by payer %+v", r.Groups)
	}
	// The largest cut first, as large as a surplus.
	r = run(t, Query{Kind: KindSum, Measure: MeasureDiff, GroupBy: GroupProvider})
	if r.Groups[0].Label != "Physiotherapie Lotte Probe" || r.Groups[0].Value != "-18,40 €" {
		t.Errorf("difference by provider %+v", r.Groups)
	}
	// A list with an amount is sorted by it.
	if r := run(t, Query{Kind: KindList, Measure: MeasureAmount}); !slices.Equal(r.Invoices, []string{"B", "C", "A", "F", "D"}) {
		t.Errorf("by amount %v", r.Invoices)
	}
}

func TestPremiumSeveralPKVs(t *testing.T) {
	d := testData()
	d.Settings.Payers = append(d.Settings.Payers, model.Payer{ID: "pkv2", Kind: model.PKV, Name: "Zweite PKV"})
	d.Settings.Coverage[0].Shares = append(d.Settings.Coverage[0].Shares, model.Share{PayerID: "pkv2", Rate: money.Percent(10)})
	d.Settings.Years = append(d.Settings.Years, model.YearSetting{Year: 2026, PayerID: "pkv2", ExpectedPremiumRefund: 10000})
	r := Run(d, Query{Kind: KindPremium}, today)
	if len(r.Figures) != 4 || !strings.HasPrefix(r.Figures[0].Label, "Muster PKV: ") || !strings.HasPrefix(r.Figures[2].Label, "Zweite PKV: ") {
		t.Errorf("figures %+v", r.Figures)
	}
}
