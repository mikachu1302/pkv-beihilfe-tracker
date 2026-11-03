package ask

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
)

// Normalize checks the model's query and corrects it by the words of the
// question – a synonym table and a few rules for what an 8B model gets
// wrong now and then (tested with internal/ask/testdata):
//
//   - unknown values are dropped, payer names matched;
//   - the words of the question decide category, status, deviation,
//     Klärungsbedarf, paid, deadline, grouping, years and dates – a filter
//     the question does not mention is dropped (an extra filter changes
//     the answer), one it clearly names is added;
//   - the form of the question decides the kind ("Wie viele" counts,
//     "Welche" lists, "Wie viel" sums) and the measure.
//
// prev is the query of the question before (nil if none): a follow-up
// ("und 2025?", "nur die vom Zahnarzt") keeps what it does not change.
func Normalize(q Query, question string, prev *Query, s *model.Settings, today civil.Date) Query {
	w := newWords(question)
	follow := prev != nil && w.followUp()
	q = valid(q, s, today)
	inherited := func(f Filter) bool { return follow && slices.Contains(prev.Filters, f) }

	// Requests and general questions stay unsupported; questions about the data never are.
	switch {
	case w.request() || w.general() && !w.aboutData():
		return Query{Kind: KindUnsupported, Years: []int{}, Filters: []Filter{}, Reason: q.Reason}
	case q.Kind == KindUnsupported && w.aboutData():
		q.Kind, q.Reason = KindList, ""
	case q.Kind == KindUnsupported:
		return q
	}

	q.Kind = w.kind(q.Kind, follow, prev)
	out := []Filter{}
	keep := func(f Filter, cue bool) {
		if cue || inherited(f) {
			out = append(out, f)
		}
	}
	for _, f := range q.Filters {
		switch f.Field {
		case FieldPayer:
			keep(f, w.namesPayer(s, f.Value))
		case FieldCategory:
			keep(f, w.category() == f.Value)
		case FieldProvider:
			if c := categoryWord(f.Value); c != "" {
				continue // "Zahnarzt" is a category, not a name (added below)
			}
			keep(f, w.has(words(f.Value)...))
		case FieldStatus:
			keep(f, w.statusCue())
		case FieldDeviation:
			keep(f, w.deviation() == f.Value)
		case FieldClarify:
			keep(f, w.contains("klärung"))
		case FieldPaid:
			keep(f, w.paid() == f.Value)
		case FieldDeadline:
			keep(f, w.contains("frist"))
		case FieldFrom, FieldTo:
			keep(f, w.period())
		}
	}
	q.Filters = out

	// What the question names clearly, the query has.
	set := func(field, value string) {
		if value == "" {
			return
		}
		q.Filters = slices.DeleteFunc(q.Filters, func(f Filter) bool { return f.Field == field })
		q.Filters = append(q.Filters, Filter{field, value})
	}
	if q.Kind != KindPremium {
		set(FieldPayer, w.payer(s))
	}
	set(FieldCategory, w.category())
	set(FieldStatus, w.status(q.Kind))
	set(FieldDeviation, w.deviation())
	if w.contains("klärung") {
		set(FieldClarify, Yes)
	}
	set(FieldPaid, w.paid())
	if w.contains("frist") {
		set(FieldDeadline, Yes)
	}
	if from, to, ok := w.month(today); ok {
		set(FieldFrom, from)
		set(FieldTo, to)
	}
	// A follow-up keeps the conditions before that it does not change (the model forgets some).
	if follow {
		for _, f := range prev.Filters {
			if q.Get(f.Field) == "" {
				q.Filters = append(q.Filters, f)
			}
		}
	}

	q.Years = w.years(today, follow, prev)
	q.GroupBy = w.group(q.GroupBy, follow, prev)
	q.Measure = w.measure(q, follow, prev)
	if q.Kind == KindSum && q.Measure == MeasureOwed && q.Get(FieldStatus) == "" {
		q.Kind, q.Measure = KindOutstanding, "" // "Wie viel steht noch aus?": the Ausstand, with its parts
	}
	if q.Kind == KindOutstanding || q.Kind == KindPremium {
		q.Measure, q.GroupBy = "", ""
		// The Ausstand is what is still owed – a status says nothing more.
		q.Filters = slices.DeleteFunc(q.Filters, func(f Filter) bool { return f.Field == FieldStatus })
	}
	if q.Kind == KindPremium {
		// Only a PKV has a premium refund: with one PKV the payer says nothing.
		single := len(payerNames(s, model.PKV)) == 1
		q.Filters = slices.DeleteFunc(q.Filters, func(f Filter) bool { return f.Field != FieldPayer || single })
	}
	return q
}

// valid drops what the app does not know: kinds, measures, groupings,
// filter fields and values, implausible years and dates. Payer names are
// matched without regard to case.
func valid(q Query, s *model.Settings, today civil.Date) Query {
	if !slices.Contains(Kinds, q.Kind) {
		q.Kind = KindUnsupported
	}
	if !slices.Contains(Measures, q.Measure) {
		q.Measure = ""
	}
	if !slices.Contains(Groups, q.GroupBy) {
		q.GroupBy = ""
	}
	years := []int{}
	for _, y := range q.Years {
		if y >= 1990 && y <= today.Year()+1 && !slices.Contains(years, y) {
			years = append(years, y)
		}
	}
	slices.Sort(years)
	q.Years = years
	vals := FilterValues(s)
	filters := []Filter{}
	for _, f := range q.Filters {
		allowed, known := vals[f.Field]
		f.Value = strings.TrimSpace(f.Value)
		switch {
		case !known || f.Value == "":
			continue
		case f.Field == FieldPayer:
			i := slices.IndexFunc(allowed, func(p string) bool { return strings.EqualFold(p, f.Value) })
			if i < 0 {
				continue
			}
			f.Value = allowed[i]
		case allowed != nil && !slices.Contains(allowed, f.Value):
			continue
		case f.Field == FieldFrom || f.Field == FieldTo:
			d, err := civil.Parse(f.Value)
			if err != nil {
				continue
			}
			f.Value = d.String()
		}
		// One value per field: the last one counts.
		filters = slices.DeleteFunc(filters, func(g Filter) bool { return g.Field == f.Field })
		filters = append(filters, f)
	}
	q.Filters = filters
	return q
}

// question holds the words of a question, lower case.
type question struct {
	text  string   // lower case, single spaces
	words []string // as words() makes them: umlauts folded, no punctuation
}

func newWords(s string) question {
	return question{text: " " + strings.Join(strings.Fields(strings.ToLower(s)), " ") + " ", words: words(s)}
}

// contains: the text has one of the parts (lower case).
func (w question) contains(parts ...string) bool {
	return slices.ContainsFunc(parts, func(p string) bool { return strings.Contains(w.text, p) })
}

// has: all these words (as words() makes them) appear in the question.
func (w question) has(ws ...string) bool {
	if len(ws) == 0 {
		return false
	}
	for _, x := range ws {
		if !slices.ContainsFunc(w.words, func(y string) bool { return strings.HasPrefix(y, x) }) {
			return false
		}
	}
	return true
}

// followUp: the question builds on the one before ("Und 2025?", "Nur die vom Zahnarzt.", "Wie viele davon …?").
func (w question) followUp() bool {
	return strings.HasPrefix(w.text, " und ") || strings.HasPrefix(w.text, " nur ") || strings.HasPrefix(w.text, " auch ") ||
		w.contains("davon", "darunter", "diese rechnungen", "dieser rechnungen")
}

var (
	requestRe = regexp.MustCompile(`^ (bitte )?(schreib|formulier|erstell|verfass|send|schick|lösch|änder|trag|reich|markier|buch|mach)`)
	generalRe = regexp.MustCompile(`^ (was (ist|sind|bedeutet|heißt)|wie (funktioniert|wird|geht)|warum|erklär|wer )`)
	listRe    = regexp.MustCompile(`^ (bitte )?(welche|zeig|liste|wo |bei welche[nmr]? rechnung|gab es|gibt es)|welche[nmrs]? (rechnung|erstattung|beleg)`)
	sumRe     = regexp.MustCompile(`^ was hat`)
	openRe    = regexp.MustCompile(`^ (was|wie viel) (ist|steht)`) // "Was ist noch offen?": the Ausstand
	notSentRe = regexp.MustCompile(`nicht (\S+ ){0,4}(eingereicht|einreich)`) // "noch nicht bei der PKV eingereicht"
	refundRe  = regexp.MustCompile(`(vollständig|komplett|ganz|schon|bereits)? erstattet`)
	dateRe    = regexp.MustCompile(`\d{1,2}\.\d{1,2}\.`)
)

// request: a task, not a question ("Schreib mir eine E-Mail").
func (w question) request() bool { return requestRe.MatchString(w.text) }

// general: a question about rules or the world, not about the own data.
func (w question) general() bool { return generalRe.MatchString(w.text) }

// aboutData: the question asks about the own invoices or refunds.
func (w question) aboutData() bool {
	return w.contains("rechnung", "erstatt", "eingereicht", "einreich", "klärung", "zurück", "frist", "bezahlt", "ausgegeben",
		"ausgaben", "schuld", "steht", "stehen", "eigenanteil", "beleg", "offen")
}

// kind decides by the form of the question; where it says nothing, a
// follow-up keeps the kind before and otherwise the model's stays.
func (w question) kind(k string, follow bool, prev *Query) string {
	switch {
	case k == KindPremium || w.contains("lohnt", "beitragsrückerstattung"):
		return KindPremium
	case w.contains("wie viele"):
		return KindCount
	case w.contains("schuldet", "schulden") || w.owing() && !w.contains("welche", "erstattung") ||
		w.contains("noch offen") && openRe.MatchString(w.text):
		return KindOutstanding
	case w.most():
		return KindSum
	case listRe.MatchString(w.text):
		return KindList
	case w.contains("wie viel", "wie hoch", "summe", "gesamtbetrag", "ausgaben", "insgesamt") || sumRe.MatchString(w.text) ||
		w.contains(" soll ") && w.contains(" ist "):
		return KindSum
	case follow:
		return prev.Kind
	}
	return k
}

// owing: "steht noch aus", "ausstehend", "fehlt noch".
func (w question) owing() bool {
	return w.contains("noch aus", "steht aus", "stehen aus", "ausstehend", "fehlt", "fehlen")
}

// payer: the payer the question names – by name, or by kind ("Beihilfe",
// "PKV") when exactly one payer has that kind.
func (w question) payer(s *model.Settings) string {
	for _, p := range s.Payers {
		if w.has(words(p.Name)...) {
			return p.Name
		}
	}
	var kind model.PayerKind
	switch {
	case w.contains("beihilfe"):
		kind = model.Beihilfe
	case w.contains("pkv", "versicherung", "krankenkasse", "kasse "):
		kind = model.PKV
	default:
		return ""
	}
	if names := payerNames(s, kind); len(names) == 1 {
		return names[0]
	}
	return ""
}

// namesPayer: the question names this payer (or its kind).
func (w question) namesPayer(s *model.Settings, name string) bool {
	if w.has(words(name)...) {
		return true
	}
	for _, p := range s.Payers {
		if p.Name == name {
			return p.Kind == model.Beihilfe && w.contains("beihilfe") ||
				p.Kind == model.PKV && w.contains("pkv", "versicherung", "krankenkasse", "kasse ")
		}
	}
	return false
}

// The words that name a category (synonym table); checked in this order
// ("Zahnarzt" before "Arzt").
var categoryWords = []struct {
	category string
	parts    []string
}{
	{string(model.CatDentist), []string{"zahn", "kieferorthop", "kfo"}},
	{string(model.CatTherapy), []string{"physio", "krankengymnast", "ergothera", "logopäd", "massage", "lymphdrainage", "heilmittel"}},
	{string(model.CatPharmacy), []string{"apothek", "medikament", "arznei"}},
	{string(model.CatAids), []string{"hilfsmittel", "brille", "hörgerät", "kontaktlinse", "sanitätshaus", "einlagen"}},
	{string(model.CatHospital), []string{"krankenhaus", "klinik", "stationär"}},
	{string(model.CatNaturopath), []string{"heilpraktiker"}},
	{string(model.CatDoctor), []string{" arzt", " ärzt", "hausarzt", "facharzt", "beim arzt"}},
}

// category: the category the question names, "" if none (or a question
// about the most expensive doctor, which groups by provider instead).
func (w question) category() string {
	if w.contains("welchem arzt", "welcher arzt", "welche ärzt", "welchen arzt") {
		return ""
	}
	for _, c := range categoryWords {
		if w.contains(c.parts...) {
			return c.category
		}
	}
	return ""
}

// categoryWord: a provider "name" that is a category ("Zahnarzt", "Physiotherapie").
func categoryWord(name string) string {
	return newWords(" " + name).category()
}

// statusCue: the question speaks about the state of claims at all.
func (w question) statusCue() bool {
	return w.contains("eingereicht", "einreich", "erstatt", "offen", "zurück") || w.owing()
}

// status: the claim status the question names, "" if none clearly.
func (w question) status(kind string) string {
	switch {
	case notSentRe.MatchString(w.text):
		return StatusOpen
	case w.contains("zurückgehalten", "zurückhalten", "zurück"):
		return StatusHeld
	case w.contains("eingereicht") && w.contains("nicht erstattet", "aber"):
		return StatusSubmitted
	case w.contains("erstattung") && (w.owing() || w.contains("wart")):
		return StatusSubmitted // "Welche Erstattungen stehen noch aus?", "warten auf die Erstattung"
	case w.contains("noch offen", "sind offen", " offen?", " offen ", " offene"):
		return StatusNotRefunded
	case (kind == KindCount || kind == KindList) && refundRe.MatchString(w.text) &&
		!w.contains("nicht erstattet", "zu wenig", "weniger", "mehr erstattet", "zu viel"):
		return StatusRefunded
	}
	return ""
}

// deviation: "zu wenig" or "zu viel" erstattet.
func (w question) deviation() string {
	switch {
	case w.contains("zu wenig", "weniger", "gekürzt", "kürz"):
		return TooLittle
	case w.contains("zu viel", "mehr erstattet", "mehr als"):
		return TooMuch
	}
	return ""
}

// paid: paid to the provider or not, "" if the question does not say.
func (w question) paid() string {
	switch {
	case w.contains("nicht bezahlt", "unbezahlt", "nicht überwiesen", "zu bezahlen", "zu zahlen", "noch bezahlen", "noch zahlen", "noch überweisen"):
		return No
	case w.contains("sind bezahlt", "schon bezahlt", "bereits bezahlt", "bezahlte rechnung", "ich überwiesen", "überwiesen habe"):
		return Yes
	}
	return ""
}

// period: the question names a part of a year (dates are only for months or days).
func (w question) period() bool {
	if w.contains("monat", "woche", "seit", "quartal", " tag") || dateRe.MatchString(w.text) {
		return true
	}
	return slices.ContainsFunc(months, func(m string) bool { return w.contains(strings.ToLower(m)) })
}

// month: "letzten Monat" and "diesen Monat" as dates.
func (w question) month(today civil.Date) (from, to string, ok bool) {
	start := civil.New(today.Year(), today.Time().Month(), 1)
	switch {
	case w.contains("letzten monat", "letzter monat", "vormonat", "vorigen monat"):
		start = civil.Of(start.Time().AddDate(0, -1, 0))
	case w.contains("diesen monat", "diesem monat", "dieser monat"):
	default:
		return "", "", false
	}
	return start.String(), civil.Of(start.Time().AddDate(0, 1, -1)).String(), true
}

var yearRe = regexp.MustCompile(`\b(19[9]\d|20\d\d)\b`)
var yearRangeRe = regexp.MustCompile(`\b(20\d\d) ?(?:bis|-|–) ?(20\d\d)\b`)

// years: the years the question names; a follow-up without a year keeps
// the years before; a question without a year asks about all (unless the
// model saw a month or a date range).
func (w question) years(today civil.Date, follow bool, prev *Query) []int {
	var out []int
	add := func(y int) {
		if y >= 1990 && y <= today.Year()+1 && !slices.Contains(out, y) {
			out = append(out, y)
		}
	}
	if m := yearRangeRe.FindStringSubmatch(w.text); m != nil {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		for y := a; y <= b && y-a < 50; y++ {
			add(y)
		}
	}
	for _, m := range yearRe.FindAllString(w.text, -1) {
		y, _ := strconv.Atoi(m)
		add(y)
	}
	switch {
	case w.contains("dieses jahr", "diesem jahr", "dieses jahres", "laufenden jahr", "aktuellen jahr", "heuer"):
		add(today.Year())
	case w.contains("letztes jahr", "letzten jahr", "vorjahr", "vorigen jahr", "vergangenen jahr"):
		add(today.Year() - 1)
	}
	switch {
	case len(out) > 0:
		slices.Sort(out)
		return out
	case follow:
		return slices.Clone(prev.Years)
	}
	return []int{} // the model's years count only when the question names them (it guesses otherwise)
}

// group: the grouping the question asks for ("pro Jahr", "nach Monaten", "am teuersten").
func (w question) group(given string, follow bool, prev *Query) string {
	by := func(parts ...string) bool {
		for _, p := range parts {
			if w.contains("pro "+p, "nach "+p, "je "+p) {
				return true
			}
		}
		return false
	}
	switch {
	case by("monat") || w.contains("monatlich", "welchen monat", "welchem monat"):
		return GroupMonth
	case by("jahr") || w.contains("jährlich", "welchem jahr", "welchen jahren"):
		return GroupYear
	case by("kategorie", "art") || w.contains("welcher kategorie", "welchen kategorien"):
		return GroupCategory
	case by("kostenträger"):
		return GroupPayer
	case by("arzt", "ärzt", "behandler", "praxis") || w.most() && w.contains("arzt", "behandler", "praxis", "wem"):
		return GroupProvider
	case w.contains("pro ", "nach ", " je ") || w.most():
		return given
	case follow:
		return prev.GroupBy
	}
	return ""
}

// most: "am meisten", "am teuersten", "die meisten".
func (w question) most() bool {
	return w.contains("am meisten", "am teuersten", "die meisten", "die höchsten")
}

// measure: what is summed, by the words of the question; a follow-up
// without such words keeps the measure before.
func (w question) measure(q Query, follow bool, prev *Query) string {
	switch q.Kind {
	case KindCount, KindPremium, KindOutstanding, KindUnsupported:
		return ""
	}
	cue := ""
	switch {
	case w.contains(" soll ") && w.contains(" ist "):
		return "" // the overview: SOLL, IST, difference, open
	case w.contains("eigenanteil", "selbst getragen", "selbst bezahlt"):
		cue = MeasureOwnShare
	case w.contains("differenz", "abweichung") || q.Kind == KindSum && w.deviation() != "":
		cue = MeasureDiff // "Wie viel hat die Beihilfe weniger erstattet als erwartet?"
	case w.contains("nicht erstattet", "offen") || w.owing():
		cue = MeasureOwed
	case w.contains("erstattet", "erstattung") && q.Kind == KindSum ||
		q.Get(FieldPayer) != "" && w.contains("überwiesen", "bekommen", "gezahlt", "ausgezahlt"):
		cue = MeasureActual // "Wie viel hat die PKV überwiesen?": the payer paid – IST
	case w.contains("ausgegeben", "ausgaben", "bezahlt", "gekostet", "kosten", "gesamtbetrag", "rechnungsbetrag", "teuer"):
		cue = MeasureAmount
	case w.contains(" soll"):
		cue = MeasureExpected
	}
	switch {
	case cue != "":
		return cue
	case follow && prev.Kind == q.Kind:
		return prev.Measure
	case q.Kind == KindList && q.Get(FieldDeviation) != "":
		return MeasureDiff // "Wo hat die Beihilfe zu wenig erstattet?": by how much
	case q.Kind == KindSum && q.Measure == "":
		return MeasureAmount
	}
	return q.Measure
}
