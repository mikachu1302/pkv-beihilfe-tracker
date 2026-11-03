package ask

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
)

// canon writes a query so that the order of the filters does not matter.
func canon(q Query) string {
	var f []string
	for _, x := range q.Filters {
		f = append(f, x.Field+"="+x.Value)
	}
	slices.Sort(f)
	return fmt.Sprintf("%s|%s|%v|%s|%s", q.Kind, q.Measure, q.Years, strings.Join(f, ","), q.GroupBy)
}

func payers(names ...string) *model.Settings {
	s := &model.Settings{}
	for i, n := range names {
		kind := model.PKV
		if strings.Contains(n, "Beihilfe") {
			kind = model.Beihilfe
		}
		s.Payers = append(s.Payers, model.Payer{ID: fmt.Sprint(i), Kind: kind, Name: n})
	}
	return s
}

// Phrasings where a rule must not fire (or must fire differently) – most of
// them found in the review of 2026-11-03. The model's answer is a plausible
// one, often with a mistake the rules should correct.
func TestNormalizeMisreadings(t *testing.T) {
	std := payers("Beihilfe Bund", "Muster PKV")
	today := civil.MustParse("2026-11-03")
	f := filters
	sumIST := Query{Kind: KindSum, Measure: MeasureActual, Years: []int{2026}, Filters: f(FieldPayer, "Beihilfe Bund")}
	cases := []struct {
		question string
		model    Query
		want     Query
		prev     *Query
		settings *model.Settings
	}{
		// "zurück" is not always "zurückgehalten".
		{"Wie viel habe ich 2025 von der Beihilfe zurückbekommen?",
			Query{Kind: KindSum, Measure: MeasureActual, Years: []int{2025}, Filters: f(FieldPayer, "Beihilfe Bund", FieldStatus, StatusHeld)},
			Query{Kind: KindSum, Measure: MeasureActual, Years: []int{2025}, Filters: f(FieldPayer, "Beihilfe Bund")}, nil, nil},
		{"Wie viel Geld bekomme ich noch zurück?", Query{Kind: KindOutstanding}, Query{Kind: KindOutstanding}, nil, nil},
		// "ohne Klärungsbedarf" is not "mit".
		{"Welche Rechnungen sind ohne Klärungsbedarf?", Query{Kind: KindList, Filters: f(FieldClarify, No)},
			Query{Kind: KindList, Filters: f(FieldClarify, No)}, nil, nil},
		// A provider with a category word in its name stays a provider.
		{"Wie viel habe ich bei der Linden-Apotheke ausgegeben?",
			Query{Kind: KindSum, Measure: MeasureAmount, Filters: f(FieldProvider, "Linden-Apotheke")},
			Query{Kind: KindSum, Measure: MeasureAmount, Filters: f(FieldProvider, "Linden-Apotheke")}, nil, nil},
		{"Wie viel habe ich bei der Zahnarztpraxis Dr. Beispiel ausgegeben?",
			Query{Kind: KindSum, Measure: MeasureAmount, Filters: f(FieldProvider, "Zahnarztpraxis Dr. Beispiel", FieldCategory, "zahnarzt")},
			Query{Kind: KindSum, Measure: MeasureAmount, Filters: f(FieldProvider, "Zahnarztpraxis Dr. Beispiel")}, nil, nil},
		// …but a "provider" that is only a category word is the category.
		{"Nur die vom Zahnarzt.", Query{Kind: KindCount, Filters: f(FieldProvider, "Zahnarzt")},
			Query{Kind: KindList, Years: []int{2026}, Filters: f(FieldCategory, "zahnarzt", FieldStatus, StatusOpen)},
			&Query{Kind: KindList, Years: []int{2026}, Filters: f(FieldStatus, StatusOpen)}, nil},
		// A payer that still has to pay is the Ausstand, not an unpaid invoice.
		{"Was muss die Beihilfe noch zahlen?", Query{Kind: KindOutstanding, Filters: f(FieldPayer, "Beihilfe Bund")},
			Query{Kind: KindOutstanding, Filters: f(FieldPayer, "Beihilfe Bund")}, nil, nil},
		{"Wie viel hat die PKV noch zu zahlen?", Query{Kind: KindSum, Measure: MeasureAmount, Filters: f(FieldPayer, "Muster PKV")},
			Query{Kind: KindOutstanding, Filters: f(FieldPayer, "Muster PKV")}, nil, nil},
		{"Welche Rechnungen muss ich noch bezahlen?", Query{Kind: KindList},
			Query{Kind: KindList, Filters: f(FieldPaid, No)}, nil, nil},
		{"Welche Rechnungen habe ich bezahlt?", Query{Kind: KindList},
			Query{Kind: KindList, Filters: f(FieldPaid, Yes)}, nil, nil},
		{"Wie viel Erstattung bekomme ich noch von der Beihilfe?", Query{Kind: KindOutstanding},
			Query{Kind: KindOutstanding, Filters: f(FieldPayer, "Beihilfe Bund")}, nil, nil},
		{"Welcher Arzt hat mir die meisten Rechnungen geschickt?", Query{Kind: KindCount, GroupBy: GroupProvider},
			Query{Kind: KindCount, GroupBy: GroupProvider}, nil, nil},
		// "kürzlich" is no cut, "mehr als 500 Euro" no deviation and no year.
		{"Welche Rechnungen habe ich kürzlich eingereicht?", Query{Kind: KindList, Filters: f(FieldStatus, StatusSubmitted)},
			Query{Kind: KindList, Filters: f(FieldStatus, StatusSubmitted)}, nil, nil},
		{"Welche Rechnungen kosten mehr als 500 Euro?", Query{Kind: KindList}, Query{Kind: KindList, Measure: MeasureAmount}, nil, nil},
		{"Welche Rechnungen liegen über 2000 Euro?", Query{Kind: KindList, Years: []int{2000}}, Query{Kind: KindList}, nil, nil},
		{"Wo hat die PKV mehr erstattet als erwartet?",
			Query{Kind: KindList, Filters: f(FieldPayer, "Muster PKV", FieldDeviation, TooMuch, FieldStatus, StatusHeld)},
			Query{Kind: KindList, Measure: MeasureDiff, Filters: f(FieldPayer, "Muster PKV", FieldDeviation, TooMuch)}, nil, nil},
		// Years.
		{"Was habe ich vorletztes Jahr ausgegeben?", Query{Kind: KindSum, Measure: MeasureAmount, Years: []int{2025}},
			Query{Kind: KindSum, Measure: MeasureAmount, Years: []int{2024}}, nil, nil},
		{"Wie viel habe ich in den letzten drei Jahren ausgegeben?", Query{Kind: KindSum, Measure: MeasureAmount},
			Query{Kind: KindSum, Measure: MeasureAmount, Years: []int{2024, 2025, 2026}}, nil, nil},
		{"Wie viele Rechnungen hatte ich zwischen 2023 und 2025?", Query{Kind: KindCount, Years: []int{2023, 2025}},
			Query{Kind: KindCount, Years: []int{2023, 2024, 2025}}, nil, nil},
		{"Wie viele Rechnungen seit 2024?", Query{Kind: KindCount, Years: []int{2024}},
			Query{Kind: KindCount, Years: []int{2024, 2025, 2026}}, nil, nil},
		{"Zeig die 2025er Rechnungen", Query{Kind: KindList}, Query{Kind: KindList, Years: []int{2025}}, nil, nil},
		{"Ausgaben dieses und letztes Jahr", Query{Kind: KindSum, Measure: MeasureAmount, Years: []int{2025}},
			Query{Kind: KindSum, Measure: MeasureAmount, Years: []int{2025, 2026}}, nil, nil},
		{"Was habe ich vor zwei Jahren ausgegeben?", Query{Kind: KindSum, Measure: MeasureAmount, Years: []int{2024}},
			Query{Kind: KindSum, Measure: MeasureAmount, Years: []int{2024}}, nil, nil},
		// "nicht vollständig erstattet" is the opposite of "erstattet".
		{"Welche Rechnungen hat die Beihilfe noch nicht vollständig erstattet?",
			Query{Kind: KindList, Filters: f(FieldPayer, "Beihilfe Bund", FieldStatus, StatusRefunded)},
			Query{Kind: KindList, Filters: f(FieldPayer, "Beihilfe Bund", FieldStatus, StatusNotRefunded)}, nil, nil},
		// Questions, not requests; follow-ups, not general questions.
		{"Macht es Sinn, die Rechnungen von 2026 bei der PKV einzureichen?", Query{Kind: KindPremium, Years: []int{2026}},
			Query{Kind: KindPremium, Years: []int{2026}}, nil, nil},
		{"Was ist mit 2025?", Query{Kind: KindSum, Measure: MeasureActual, Years: []int{2025}, Filters: f(FieldPayer, "Beihilfe Bund")},
			Query{Kind: KindSum, Measure: MeasureActual, Years: []int{2025}, Filters: f(FieldPayer, "Beihilfe Bund")}, &sumIST, nil},
		// General questions stay unsupported.
		{"Wie funktioniert die Erstattung bei der Beihilfe?", Query{Kind: KindList, Filters: f(FieldPayer, "Beihilfe Bund")},
			Query{Kind: KindUnsupported}, nil, nil},
		{"Was bedeutet Klärungsbedarf?", Query{Kind: KindList, Filters: f(FieldClarify, Yes)}, Query{Kind: KindUnsupported}, nil, nil},
		{"Was kostet eine Zahnreinigung normalerweise?", Query{Kind: KindUnsupported}, Query{Kind: KindUnsupported}, nil, nil},
		// An invoice amount has no share per payer.
		{"Ausgaben pro Kostenträger", Query{Kind: KindSum, Measure: MeasureAmount, GroupBy: GroupPayer},
			Query{Kind: KindSum, GroupBy: GroupPayer}, nil, nil},
		{"Wie viel ist noch offen, nach Jahren?", Query{Kind: KindOutstanding},
			Query{Kind: KindSum, Measure: MeasureOwed, GroupBy: GroupYear}, nil, nil},
		// Spelling: no umlauts, "wieviele" in one word.
		{"Wieviele Rechnungen sind noch offen?", Query{Kind: KindList, Filters: f(FieldStatus, StatusNotRefunded)},
			Query{Kind: KindCount, Filters: f(FieldStatus, StatusNotRefunded)}, nil, nil},
		{"Welche Rechnungen halte ich bei der PKV zurueck?", Query{Kind: KindList},
			Query{Kind: KindList, Filters: f(FieldPayer, "Muster PKV", FieldStatus, StatusHeld)}, nil, nil},
		// A new question starting with "und" is not a follow-up; "und alle Jahre?" widens.
		{"Und wie viel habe ich 2025 beim Zahnarzt ausgegeben?", Query{Kind: KindSum, Measure: MeasureAmount, Years: []int{2025}},
			Query{Kind: KindSum, Measure: MeasureAmount, Years: []int{2025}, Filters: f(FieldCategory, "zahnarzt")},
			&Query{Kind: KindList, Filters: f(FieldPayer, "Beihilfe Bund", FieldDeviation, TooLittle)}, nil},
		{"Und alle Jahre?", Query{Kind: KindCount, Years: []int{2026}, Filters: f(FieldStatus, StatusOpen)},
			Query{Kind: KindCount, Filters: f(FieldStatus, StatusOpen)},
			&Query{Kind: KindCount, Years: []int{2026}, Filters: f(FieldStatus, StatusOpen)}, nil},
		// A provider named like a payer is a provider.
		{"Zeig mir alle Rechnungen von Dr. Muster.", Query{Kind: KindList, Filters: f(FieldProvider, "Dr. Muster")},
			Query{Kind: KindList, Filters: f(FieldProvider, "Dr. Muster")}, nil, nil},
		// Payers by their own names: whole distinctive words, both named = no payer condition.
		{"Was hat die Debeka 2025 erstattet?", Query{Kind: KindSum, Measure: MeasureActual, Years: []int{2025},
			Filters: f(FieldPayer, "Debeka Krankenversicherung")},
			Query{Kind: KindSum, Measure: MeasureActual, Years: []int{2025}, Filters: f(FieldPayer, "Debeka Krankenversicherung")},
			nil, payers("Beihilfe NRW", "Debeka Krankenversicherung")},
		{"Wie viele Rechnungen habe ich von Dr. Vogel?", Query{Kind: KindCount, Filters: f(FieldProvider, "Dr. Vogel")},
			Query{Kind: KindCount, Filters: f(FieldProvider, "Dr. Vogel")}, nil, payers("Beihilfe NRW", "R+V")},
		{"Wie viel habe ich für Ergotherapie ausgegeben?", Query{Kind: KindSum, Measure: MeasureAmount},
			Query{Kind: KindSum, Measure: MeasureAmount, Filters: f(FieldCategory, "heilmittel")}, nil, payers("Beihilfe NRW", "ERGO")},
		{"Was hat die DKV Zahnzusatz 2025 erstattet?", Query{Kind: KindSum, Measure: MeasureActual, Years: []int{2025}},
			Query{Kind: KindSum, Measure: MeasureActual, Years: []int{2025}, Filters: f(FieldPayer, "DKV Zahnzusatz")},
			nil, payers("Beihilfe NRW", "Debeka", "DKV Zahnzusatz")},
		{"Wie viel haben Beihilfe und PKV 2025 erstattet?",
			Query{Kind: KindSum, Measure: MeasureActual, Years: []int{2025}, Filters: f(FieldPayer, "Beihilfe Bund")},
			Query{Kind: KindSum, Measure: MeasureActual, Years: []int{2025}}, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.question, func(t *testing.T) {
			s := tc.settings
			if s == nil {
				s = std
			}
			got := Normalize(tc.model, tc.question, tc.prev, s, today)
			if tc.want.Years == nil {
				tc.want.Years = []int{}
			}
			if canon(got) != canon(tc.want) {
				t.Errorf("\n got %s (%s)\nwant %s", canon(got), got, canon(tc.want))
			}
		})
	}
}
