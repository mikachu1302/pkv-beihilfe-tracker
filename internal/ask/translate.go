package ask

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/ai"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
)

// Turning a question into a query: the schema and the instructions for the
// model. The model sees the question, today's date and the vocabulary of
// the app – never an invoice. Listing only the filters a question needs
// (instead of every field) is what made the model reliable (docs/PLAN-v0.5.md).

// Turn is an earlier question of the conversation with the query the app ran.
type Turn struct {
	Question string `json:"frage"`
	Query    Query  `json:"abfrage"`
}

// MaxTurns is how many earlier questions go along (for follow-ups such as "und 2025?").
const MaxTurns = 2

// Schema is the JSON schema of a query for these settings (payer names as
// the allowed values), with the properties in the order the model writes them.
func Schema(s *model.Settings) json.RawMessage {
	vals := FilterValues(s)
	var filters []any
	for _, f := range FilterFields {
		value := map[string]any{"type": "string"}
		if v := vals[f]; v != nil {
			value = map[string]any{"enum": v}
		}
		filters = append(filters, map[string]any{"type": "object",
			"properties":           map[string]any{"feld": map[string]any{"const": f}, "wert": value},
			"required":             []string{"feld", "wert"},
			"additionalProperties": false})
	}
	props := []struct {
		key  string
		prop any
	}{
		{"typ", map[string]any{"type": "string", "enum": Kinds}},
		{"messgroesse", map[string]any{"type": "string", "enum": Measures}},
		{"jahre", map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}},
		{"filter", map[string]any{"type": "array", "items": map[string]any{"anyOf": filters}}},
		{"gruppierung", map[string]any{"type": "string", "enum": Groups}},
		{"grund", map[string]any{"type": "string"}},
	}
	var b strings.Builder
	b.WriteString(`{"type":"object","properties":{`)
	keys := make([]string, len(props))
	for i, p := range props {
		if i > 0 {
			b.WriteByte(',')
		}
		v, _ := json.Marshal(p.prop)
		fmt.Fprintf(&b, "%q:%s", p.key, v)
		keys[i] = p.key
	}
	req, _ := json.Marshal(keys)
	fmt.Fprintf(&b, `},"required":%s,"additionalProperties":false}`, req)
	return json.RawMessage(b.String())
}

// payerNames: the names of the payers of one kind, joined ("Beihilfe Bund").
func payerNames(s *model.Settings, kind model.PayerKind) []string {
	var out []string
	for _, p := range s.Payers {
		if p.Kind == kind {
			out = append(out, p.Name)
		}
	}
	return out
}

// System is the instructions for the model: today, the vocabulary and examples.
func System(s *model.Settings, today civil.Date) string {
	bh, pkv := first(payerNames(s, model.Beihilfe), "Beihilfe"), first(payerNames(s, model.PKV), "PKV")
	var payers []string
	for _, p := range s.Payers {
		payers = append(payers, p.Name)
	}
	examples := []Turn{
		{"Wie viele Rechnungen aus 2025 sind noch nicht eingereicht?", Query{Kind: KindCount, Years: []int{2025},
			Filters: []Filter{{FieldStatus, StatusOpen}}}},
		{"Was hat die Beihilfe insgesamt erstattet?", Query{Kind: KindSum, Measure: MeasureActual, Filters: []Filter{{FieldPayer, bh}}}},
		{"Ausgaben beim Hautarzt pro Jahr", Query{Kind: KindSum, Measure: MeasureAmount, Filters: []Filter{{FieldProvider, "Hautarzt"}},
			GroupBy: GroupYear}},
		{"Welche Rechnungen hat die PKV 2026 gekürzt?", Query{Kind: KindList, Measure: MeasureDiff, Years: []int{2026},
			Filters: []Filter{{FieldPayer, pkv}, {FieldDeviation, TooLittle}}}},
		{"Welche Erstattungen der Beihilfe stehen noch aus?", Query{Kind: KindList, Filters: []Filter{{FieldPayer, bh},
			{FieldStatus, StatusSubmitted}}}},
		{"Wo gibt es Klärungsbedarf?", Query{Kind: KindList, Filters: []Filter{{FieldClarify, Yes}}}},
		{"Wie funktioniert die Beihilfe?", Query{Kind: KindUnsupported, Reason: "keine Frage zu den eigenen Rechnungen"}},
	}
	var b strings.Builder
	fmt.Fprintf(&b, `Du übersetzt Fragen zu Arztrechnungen und Erstattungen (private Krankenversicherung und Beihilfe) in eine Abfrage.
Du siehst keine Daten und rechnest nichts – die App führt die Abfrage aus. Antworte nur mit JSON nach dem Schema.

Heute ist der %s. Kostenträger: %s. Kategorien: %s.

- typ: anzahl (wie viele), summe (wie viel Geld), liste (welche, wo, zeige), beitragsrueckerstattung (lohnt sich Einreichen bei der PKV oder die Beitragsrückerstattung), ausstand (was schulden die Kostenträger noch, was steht insgesamt aus), nicht_unterstuetzt (keine Frage zu den eigenen Rechnungen, oder eine Bitte, etwas zu tun – Grund in „grund“).
- messgroesse: betrag (Rechnungsbetrag, „ausgegeben“), soll (erwartete Erstattung), ist (tatsächlich erstattet), differenz (IST minus SOLL), offen (noch ausstehend), eigenanteil (selbst getragen); leer bei anzahl.
- jahre: Jahre der Behandlung; leer = alle. „dieses Jahr“ = %d.
- filter: NUR die Bedingungen, die die Frage nennt – sonst eine leere Liste. kostentraeger („Beihilfe“ = %s, „PKV“/„Versicherung“ = %s); status je Kostenträger (nicht_erstattet = noch offen, also nicht eingereicht oder eingereicht; eingereicht = eingereicht, die Erstattung steht aus; zurueckgehalten = bewusst nicht eingereicht); abweichung (zu_wenig: IST kleiner als SOLL, zu_viel: mehr erstattet als erwartet); klaerungsbedarf; bezahlt (an den Arzt bezahlt); frist_bald (Antragsfrist läuft ab); kategorie (Physiotherapie, Ergotherapie, Logopädie = heilmittel; Brille, Hörgerät = hilfsmittel; Medikamente = apotheke); behandler (Name wie genannt, nie eine Kategorie); von/bis (JJJJ-MM-TT, nur bei Monaten oder Tagen).
- gruppierung: nur bei „pro“, „nach“, „je“ oder „am meisten/teuersten“.
- Nachfragen („und 2025?“, „nur die vom Zahnarzt“): die vorige Abfrage übernehmen und nur das Genannte ändern.

Beispiele:
`, today.DE(), strings.Join(payers, ", "), strings.Join(Categories, ", "), today.Year(), strings.Join(payerNames(s, model.Beihilfe), " oder "),
		strings.Join(payerNames(s, model.PKV), " oder "))
	for _, e := range examples {
		fmt.Fprintf(&b, "Frage: %s\nAbfrage: %s\n", e.Question, e.Query.JSON())
	}
	return b.String()
}

func first(list []string, fallback string) string {
	if len(list) > 0 {
		return list[0]
	}
	return fallback
}

// JSON is the query as the model writes it (empty lists, not null).
func (q Query) JSON() string {
	if q.Years == nil {
		q.Years = []int{}
	}
	if q.Filters == nil {
		q.Filters = []Filter{}
	}
	b, _ := json.Marshal(q)
	return string(b)
}

// Messages is the request for a question: the instructions, the last
// MaxTurns questions with the queries the app ran, the question.
func Messages(s *model.Settings, today civil.Date, history []Turn, question string) []ai.Message {
	msgs := []ai.Message{{Role: "system", Content: System(s, today)}}
	if len(history) > MaxTurns {
		history = history[len(history)-MaxTurns:]
	}
	for _, t := range history {
		msgs = append(msgs, ai.Message{Role: "user", Content: t.Question}, ai.Message{Role: "assistant", Content: t.Query.JSON()})
	}
	return append(msgs, ai.Message{Role: "user", Content: question})
}
