package ask

import (
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
)

// Chip is one part of how the app understood a question – shown with the
// answer, so a misunderstanding is visible and can be fixed.
type Chip struct {
	Key  string // what it stands for: "jahre", "gruppierung", a filter field; "typ" for the kind (fixed)
	Text string
}

// Labels for the chips and for choosing values in the UI.
var (
	StatusLabels = map[string]string{StatusOpen: "nicht eingereicht", StatusSubmitted: "Erstattung offen", StatusRefunded: "erstattet",
		StatusHeld: "zurückgehalten", StatusNotRefunded: "noch nicht erstattet"}
	GroupLabels = map[string]string{GroupYear: "nach Jahr", GroupMonth: "nach Monat", GroupPayer: "nach Kostenträger",
		GroupProvider: "nach Behandler", GroupCategory: "nach Kategorie"}
	valueLabels = map[string]map[string]string{
		FieldDeviation: {TooLittle: "zu wenig erstattet", TooMuch: "mehr erstattet als erwartet"},
		FieldClarify:   {Yes: "Klärungsbedarf", No: "ohne Klärungsbedarf"},
		FieldPaid:      {Yes: "bezahlt", No: "nicht bezahlt"},
		FieldDeadline:  {Yes: "Frist bald"},
		FieldStatus:    StatusLabels,
		FieldCategory:  CategoryLabels,
	}
)

// Describe lists how the query reads: what is asked, the years, each
// filter, the grouping.
func Describe(q Query) []Chip {
	out := []Chip{{Key: "typ", Text: kindText(q)}}
	if len(q.Years) > 0 {
		out = append(out, Chip{Key: "jahre", Text: yearList(q.Years)})
	}
	for _, f := range q.Filters {
		out = append(out, Chip{Key: f.Field, Text: FilterText(f)})
	}
	if q.GroupBy != "" {
		out = append(out, Chip{Key: "gruppierung", Text: GroupLabels[q.GroupBy]})
	}
	return out
}

func kindText(q Query) string {
	switch q.Kind {
	case KindCount:
		return "Anzahl"
	case KindSum:
		if q.Measure == "" {
			return "SOLL und IST"
		}
		return "Summe: " + measureLabels[q.Measure]
	case KindList:
		if q.Measure != "" && q.Measure != MeasureAmount {
			return "Liste mit " + measureLabels[q.Measure]
		}
		return "Liste"
	case KindPremium:
		return "Einreichen oder Beitragsrückerstattung"
	case KindOutstanding:
		return "Ausstand"
	}
	return "keine Frage zu den eigenen Daten"
}

// FilterText is a filter as the chip shows it.
func FilterText(f Filter) string {
	if l := valueLabels[f.Field][f.Value]; l != "" {
		return l
	}
	switch f.Field {
	case FieldProvider:
		return "Behandler „" + f.Value + "“"
	case FieldFrom, FieldTo:
		word := "ab "
		if f.Field == FieldTo {
			word = "bis "
		}
		if d, err := civil.Parse(f.Value); err == nil {
			return word + d.DE()
		}
		return word + f.Value
	}
	return f.Value // payer names, values the labels do not know
}

// MeasureLabel names a measure ("Erstattet (IST)").
func MeasureLabel(m string) string { return measureLabels[m] }

// YearsText: "2026", "2025 und 2026" (exported for the UI's year choice).
func YearsText(years []int) string {
	if len(years) == 0 {
		return "alle Jahre"
	}
	return yearList(years)
}

// String is the query in one line (for logs and tests): "Anzahl · 2026 · nicht eingereicht".
func (q Query) String() string {
	var parts []string
	for _, c := range Describe(q) {
		parts = append(parts, c.Text)
	}
	return strings.Join(parts, " · ")
}
