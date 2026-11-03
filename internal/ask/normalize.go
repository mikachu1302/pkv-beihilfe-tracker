package ask

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/match"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
)

// Normalize checks the model's query and corrects it by the words of the
// question – a synonym table and rules for what an 8B model gets wrong now
// and then (tested with internal/ask/testdata, including phrasings where a
// rule must not fire):
//
//   - unknown values are dropped, payer names matched;
//   - the words of the question decide payer, category, status, deviation,
//     Klärungsbedarf, paid, deadline, grouping, years and dates – a filter
//     the question does not mention is dropped (an extra filter changes the
//     answer), one it clearly names is added;
//   - the form of the question decides the kind ("Wie viele" counts,
//     "Welche" lists, "Wie viel" sums) and the measure.
//
// prev is the query of the question before (nil if none): a follow-up
// ("und 2025?", "nur die vom Zahnarzt") keeps what it does not change.
func Normalize(q Query, text string, prev *Query, s *model.Settings, today civil.Date) Query {
	w := readQuestion(text)
	follow := prev != nil && w.followUp()
	q = valid(q, s, today)

	// Requests and general questions are not about the own data; questions about it are.
	switch {
	case w.request() || w.general() && !w.ownData():
		return Query{Kind: KindUnsupported, Years: []int{}, Filters: []Filter{}, Reason: q.Reason}
	case q.Kind == KindUnsupported && w.aboutData():
		q.Kind, q.Reason = KindList, ""
	case q.Kind == KindUnsupported:
		return q
	}
	q.Kind = w.kind(q.Kind, follow, prev)

	provider := q.Get(FieldProvider)
	if provider != "" && (onlyCategory(provider) || !w.hasWords(match.NameWords(provider)...)) {
		provider = "" // "Zahnarzt" is a category, not a name; a name the question does not contain is made up
	}
	payers := w.without(provider).payers(s) // "Dr. Muster" names a provider, not "Muster PKV"
	// Payer and provider names do not count as category words ("DKV Zahnzusatz", "Linden-Apotheke").
	category := w.without(append(slices.Clone(payers), provider)...).category()
	inherited := func(f Filter) bool { return follow && slices.Contains(prev.Filters, f) }

	out := []Filter{}
	keep := func(f Filter, cue bool) {
		if cue || inherited(f) {
			out = append(out, f)
		}
	}
	for _, f := range q.Filters {
		switch f.Field {
		case FieldPayer:
			keep(f, slices.Contains(payers, f.Value) && len(payers) == 1)
		case FieldCategory:
			keep(f, category == f.Value)
		case FieldProvider:
			keep(f, provider != "")
		case FieldStatus:
			keep(f, w.statusCue(f.Value, q.Kind))
		case FieldDeviation:
			keep(f, w.deviation() == f.Value)
		case FieldClarify:
			keep(f, w.clarify() == f.Value)
		case FieldPaid:
			keep(f, w.paid(q.Kind) == f.Value)
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
	switch {
	case len(payers) == 1 && q.Kind != KindPremium:
		set(FieldPayer, payers[0])
	case len(payers) > 1: // "Beihilfe und PKV": both, so no payer condition
		q.Filters = slices.DeleteFunc(q.Filters, func(f Filter) bool { return f.Field == FieldPayer })
	}
	set(FieldCategory, category)
	set(FieldStatus, w.status(q.Kind))
	set(FieldDeviation, w.deviation())
	set(FieldClarify, w.clarify())
	set(FieldPaid, w.paid(q.Kind))
	if w.contains("frist") { // "Antragsfrist", "Frist"
		set(FieldDeadline, Yes)
	}
	if from, to, ok := w.month(today); ok {
		set(FieldFrom, from)
		set(FieldTo, to)
	}
	// A follow-up keeps the conditions before that it does not change (the model forgets some).
	if follow {
		for _, f := range prev.Filters {
			if q.Get(f.Field) == "" && (f.Field != FieldPayer || len(payers) <= 1) {
				q.Filters = append(q.Filters, f)
			}
		}
	}

	q.Years = w.years(q.Years, today, follow, prev)
	q.GroupBy = w.group(q.GroupBy, follow, prev)
	q.Measure = w.measure(q, follow, prev)
	switch {
	case q.Kind == KindOutstanding && q.GroupBy != "":
		// "Was ist noch offen, nach Jahren?": the open amounts (as the Ausstand) by year.
		q.Kind, q.Measure = KindSum, MeasureOwed
		q.Filters = slices.DeleteFunc(q.Filters, func(f Filter) bool { return f.Field == FieldStatus })
	case q.Kind == KindSum && q.Measure == MeasureOwed && q.Get(FieldStatus) == "" && q.GroupBy == "":
		q.Kind = KindOutstanding // "Wie viel steht noch aus?": the Ausstand, with its parts
	}
	if q.GroupBy == GroupPayer && (q.Measure == MeasureAmount || q.Measure == MeasureOwnShare) {
		q.Measure = "" // an invoice amount has no share per payer: SOLL and IST instead
	}
	switch q.Kind {
	case KindOutstanding:
		// The Ausstand is what is still owed – a status or a measure says nothing more.
		q.Measure, q.GroupBy = "", ""
		q.Filters = slices.DeleteFunc(q.Filters, func(f Filter) bool { return f.Field == FieldStatus })
	case KindPremium:
		q.Measure, q.GroupBy = "", ""
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
	if !slices.Contains(kinds, q.Kind) {
		q.Kind = KindUnsupported
	}
	if !slices.Contains(measures, q.Measure) {
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
	vals := filterValues(s)
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

// question is a question as the rules read it: folded (lower case, umlauts
// written out: "zurück" → "zurueck"), "wieviel" as "wie viel", punctuation
// as spaces (dots in dates stay), single spaces and a space at both ends –
// so " offen " finds the word and not "offenen". The rules below are
// written in this form.
type question struct {
	text  string
	words []string // the words as match.NameWords makes them
}

var (
	punctRe   = regexp.MustCompile(`[?!,;:"„“”‚‘'()\[\]]|\.(\s|$)`)
	spaceRe   = regexp.MustCompile(`\s+`)
	wievielRe = regexp.MustCompile(`\bwieviel`)
)

func readQuestion(s string) question {
	t := match.Fold(s)
	t = wievielRe.ReplaceAllString(t, "wie viel")
	t = punctRe.ReplaceAllString(t, " ")
	t = " " + strings.TrimSpace(spaceRe.ReplaceAllString(t, " ")) + " "
	return question{text: t, words: match.NameWords(t)}
}

// contains: the text has one of the parts (folded on the way, so umlauts may be written).
func (w question) contains(parts ...string) bool {
	return slices.ContainsFunc(parts, func(p string) bool { return strings.Contains(w.text, match.Fold(p)) })
}

// has: a word of the question starts with one of the prefixes.
func (w question) has(prefixes ...string) bool {
	return slices.ContainsFunc(w.words, func(word string) bool {
		return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(word, match.Fold(p)) })
	})
}

// hasWords: every one of the words is a word of the question (at least its
// start: "Muster" finds "Musters"); nothing to look for is false.
func (w question) hasWords(words ...string) bool {
	if len(words) == 0 {
		return false
	}
	for _, x := range words {
		if !slices.ContainsFunc(w.words, func(y string) bool { return strings.HasPrefix(y, x) }) {
			return false
		}
	}
	return true
}

// without is the question without the words of these names (to look for
// category words that are not part of a name).
func (w question) without(names ...string) question {
	drop := map[string]bool{}
	for _, n := range names {
		for _, x := range match.NameWords(n) {
			drop[x] = true
		}
	}
	if len(drop) == 0 {
		return w
	}
	var kept []string
	for _, x := range strings.Fields(w.text) {
		if !slices.ContainsFunc(match.NameWords(x), func(y string) bool { return drop[y] }) {
			kept = append(kept, x)
		}
	}
	return readQuestion(strings.Join(kept, " "))
}

var (
	followRe  = regexp.MustCompile(`^ (und|nur|auch|was ist mit) `)
	requestRe = regexp.MustCompile(`^ (bitte )?(schreibe?|formuliere?|erstelle?|verfasse?|sende?|schicke?|loesche?|aendere?|reiche?|markiere?|buche?|trage?|mach) `)
	generalRe = regexp.MustCompile(`^ (was (ist|sind|bedeutet|bedeuten|heisst)|wie (funktioniert|funktionieren|wird|werden|geht|berechnet)|warum|wieso|weshalb|erklaer\S*|wer) `)
	listRe    = regexp.MustCompile(`^ (bitte )?(welche|zeig|liste|wo |bei welche[nmr]? rechnung|gab es|gibt es)|welche[nmrs]? (rechnung|erstattung|beleg)`)
	sumRe     = regexp.MustCompile(`^ was hat`)
	openRe    = regexp.MustCompile(`^ (was|wie viel) (ist|steht)`)            // "Was ist noch offen?": the Ausstand
	notSentRe = regexp.MustCompile(`nicht (\S+ ){0,4}(eingereicht|einreich)`) // "noch nicht bei der PKV eingereicht"
	notPaidRe = regexp.MustCompile(`nicht (\S+ ){0,3}erstattet`)              // "noch nicht vollständig erstattet"
	heldRe    = regexp.MustCompile(`zurueckgehalten|zurueckhalte|zurueckbehalten|halte (\S+ ){0,6}zurueck `)
	devRe     = regexp.MustCompile(`(mehr|weniger) (\S+ ){0,3}als (erwartet|gedacht|berechnet|vorgesehen|das soll|soll)`)
	sollIstRe = regexp.MustCompile(` soll (und|/|sowie) ist | ist (und|/) soll `)
	dateRe    = regexp.MustCompile(`\d{1,2}\.\d{1,2}\.`)
)

// followUp: the question builds on the one before – short and starting with
// "und", "nur" …, or pointing back ("davon", "diese Rechnungen").
func (w question) followUp() bool {
	return followRe.MatchString(w.text) && len(strings.Fields(w.text)) <= 6 ||
		w.contains("davon", "darunter", "diese rechnungen", "dieser rechnungen")
}

// request: a task, not a question ("Schreib mir eine E-Mail", "Bitte reiche … ein").
func (w question) request() bool { return requestRe.MatchString(w.text) }

// general: a question about rules or the world ("Was ist …", "Wie funktioniert …").
func (w question) general() bool {
	return generalRe.MatchString(w.text) && !strings.HasPrefix(w.text, " was ist mit ")
}

// ownData: a general-looking question that is about the own data after all
// ("Was ist bei der Beihilfe noch offen?", "Was ist mein Eigenanteil?").
func (w question) ownData() bool {
	return w.contains(" ich ", " mir ", " mich ", " mein", " habe ", " hatte ", "rechnung", "noch offen", "noch aus",
		"ausstehend", "eingereicht", "bezahlt", "eigenanteil")
}

// aboutData: the question is about invoices or refunds at all (to overrule a model that calls it unsupported).
func (w question) aboutData() bool {
	return w.has("rechnung", "erstatt", "eingereicht", "einreich", "klaerung", "zurueck", "frist", "bezahlt", "ausgegeben",
		"ausgaben", "schuld", "steht", "stehen", "eigenanteil", "beleg", "offen")
}

// kind decides by the form of the question; where it says nothing, a
// follow-up keeps the kind before and otherwise the model's stays.
func (w question) kind(k string, follow bool, prev *Query) string {
	list := listRe.MatchString(w.text)
	switch {
	case k == KindPremium || w.has("lohnt", "beitragsrueckerstattung"):
		return KindPremium
	case w.contains("wie viele", "anzahl", "die meisten rechnungen", "am haeufigsten", "am oeftesten"):
		return KindCount
	case w.has("schuldet", "schulden") || w.owing() && !list || w.contains("noch offen") && openRe.MatchString(w.text):
		return KindOutstanding
	case w.most() && list:
		return KindList // "Welche Rechnungen waren am teuersten?": the list, by amount
	case w.most():
		return KindSum
	case list:
		return KindList
	case w.contains("wie viel", "wie hoch", "summe", "gesamtbetrag", "ausgaben", "insgesamt") || sumRe.MatchString(w.text) ||
		sollIstRe.MatchString(w.text):
		return KindSum
	case follow:
		return prev.Kind
	}
	return k
}

// owing: still to come from a payer – "steht noch aus", "fehlt noch",
// "bekomme ich noch", "Was muss die Beihilfe noch zahlen?".
func (w question) owing() bool {
	return w.contains("noch aus ", "steht aus", "stehen aus", "ausstehend", " fehlt", " fehlen", "bekomme ich noch",
		"noch bekomme", "kriege ich noch", "noch zurueck") ||
		!w.contains(" ich ") && w.contains("noch zahlen", "noch bezahlen", "noch ueberweisen", "noch erstatten", "noch zu zahlen",
			"noch zu bezahlen", "noch zu erstatten", "noch zu ueberweisen")
}

// Words for the kinds of payers; a payer's own name is found by its distinctive words.
var (
	payerKindWords = map[model.PayerKind][]string{
		model.Beihilfe: {"beihilfe"},
		model.PKV:      {"pkv", "versicherung", "krankenversicherung", "krankenkasse", "kasse"},
	}
	genericWords = map[string]bool{"beihilfe": true, "beihilfestelle": true, "pkv": true, "versicherung": true,
		"krankenversicherung": true, "krankenkasse": true, "kasse": true, "private": true, "privat": true, "ag": true,
		"vvag": true, "gesellschaft": true}
)

// payers are the payers the question names: by a distinctive word of the
// name ("Debeka" for "Debeka Krankenversicherung"; whole words, at least
// three letters), or by kind ("Beihilfe", "PKV") when exactly one payer has
// that kind. Sorted by name.
func (w question) payers(s *model.Settings) []string {
	var out []string
	add := func(n string) {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	for _, p := range s.Payers {
		for _, x := range match.NameWords(p.Name) {
			if len(x) >= 3 && !genericWords[x] && slices.Contains(w.words, x) {
				add(p.Name)
			}
		}
	}
	for kind, words := range payerKindWords {
		names := payerNames(s, kind)
		if len(names) == 1 && slices.ContainsFunc(w.words, func(x string) bool { return slices.Contains(words, x) }) {
			add(names[0])
		}
	}
	slices.Sort(out)
	return out
}

// The words that name a category (synonym table), as word starts.
var categoryWords = []struct {
	category model.Category
	prefixes []string
}{
	{model.CatDentist, []string{"zahn", "kieferorthop", "kfo"}},
	{model.CatTherapy, []string{"physio", "krankengymnast", "ergothera", "logopaed", "massage", "lymphdrainage", "heilmittel"}},
	{model.CatPharmacy, []string{"apothek", "medikament", "arznei"}},
	{model.CatAids, []string{"hilfsmittel", "brille", "hoergeraet", "kontaktlinse", "sanitaetshaus", "einlage"}},
	{model.CatHospital, []string{"krankenhaus", "klinik", "stationaer"}},
	{model.CatNaturopath, []string{"heilpraktiker"}},
	{model.CatDoctor, []string{"arzt", "aerzt", "hausarzt", "hausaerzt", "facharzt", "fachaerzt"}},
}

func categoryOf(word string) model.Category {
	for _, c := range categoryWords {
		if slices.ContainsFunc(c.prefixes, func(p string) bool { return strings.HasPrefix(word, p) }) {
			return c.category
		}
	}
	return ""
}

// category: the one category the question names; "" for none, for several
// ("Zahnarzt und Physiotherapie" – a query has no "or") and for a question
// about the doctor that cost most (which groups by provider instead).
func (w question) category() string {
	if w.contains("welchem arzt", "welcher arzt", "welche aerzte", "welchen arzt", "welchen aerzten") {
		return ""
	}
	var found model.Category
	for _, x := range w.words {
		switch c := categoryOf(x); {
		case c == "":
		case found == "":
			found = c
		case c != found:
			return ""
		}
	}
	return string(found)
}

// onlyCategory: a provider "name" that is only category words ("Zahnarzt", "Physiotherapie").
func onlyCategory(name string) bool {
	words := match.NameWords(name)
	return len(words) > 0 && !slices.ContainsFunc(words, func(x string) bool { return categoryOf(x) == "" })
}

// statusCue: the question gives a reason to keep the model's status value.
// "erstattet" in a sum is the measure (IST), not a condition.
func (w question) statusCue(status, kind string) bool {
	switch status {
	case StatusOpen:
		return w.has("eingereicht", "einreich")
	case StatusSubmitted:
		return w.has("eingereicht", "einreich", "wart") || w.owing()
	case StatusRefunded:
		return (kind == KindCount || kind == KindList) && w.has("erstattet")
	case StatusHeld:
		return heldRe.MatchString(w.text)
	case StatusNotRefunded:
		return w.has("offen") || w.contains("nicht erstattet", "noch nicht")
	}
	return false
}

// status: the claim status the question names, "" if none clearly.
func (w question) status(kind string) string {
	switch {
	case notSentRe.MatchString(w.text):
		return StatusOpen
	case heldRe.MatchString(w.text):
		return StatusHeld
	case w.has("eingereicht") && w.contains("nicht erstattet", " aber "):
		return StatusSubmitted
	case w.has("erstattung") && (w.owing() || w.has("wart")):
		return StatusSubmitted // "Welche Erstattungen stehen noch aus?", "warten auf die Erstattung"
	case notPaidRe.MatchString(w.text):
		return StatusNotRefunded
	case w.contains("noch offen", "sind offen", " offen ", " offene"):
		return StatusNotRefunded
	case (kind == KindCount || kind == KindList) && w.has("erstattet") && w.deviation() == "":
		return StatusRefunded
	}
	return ""
}

// deviation: "zu wenig" or "zu viel" erstattet – not "kürzlich", not "mehr als 500 Euro".
func (w question) deviation() string {
	if m := devRe.FindStringSubmatch(w.text); m != nil {
		if m[1] == "mehr" {
			return TooMuch
		}
		return TooLittle
	}
	switch {
	case w.contains("zu wenig", "weniger erstattet", "weniger gezahlt", "weniger bezahlt", "weniger ueberwiesen", "unter dem soll") ||
		w.has("gekuerzt", "kuerzung", "kuerzt"):
		return TooLittle
	case w.contains("zu viel", "mehr erstattet", "mehr gezahlt", "mehr ueberwiesen", "mehr bekommen", "ueber dem soll"):
		return TooMuch
	}
	return ""
}

// clarify: Klärungsbedarf asked for ("ja"), excluded ("ohne", "kein": "nein"), or not mentioned.
func (w question) clarify() string {
	switch {
	case !w.has("klaerung"):
		return ""
	case w.contains("ohne klaerung", "kein klaerung", "keine klaerung", "keinen klaerung"):
		return No
	}
	return Yes
}

// paid: paid to the provider or not, "" if the question does not say. "Was
// muss die Beihilfe noch zahlen?" is not about paying the provider; "Wie
// viel habe ich bezahlt?" asks for an amount, not for paid invoices.
func (w question) paid(kind string) string {
	me := w.contains(" ich ")
	switch {
	case w.contains("nicht bezahlt", "unbezahlt", "nicht ueberwiesen", "noch nicht gezahlt") ||
		me && w.contains("zu bezahlen", "zu zahlen", "noch bezahlen", "noch zahlen", "noch ueberweisen"):
		return No
	case w.contains("sind bezahlt", "schon bezahlt", "bereits bezahlt", "bezahlte rechnung", "ich ueberwiesen", "ueberwiesen habe") ||
		(kind == KindCount || kind == KindList) && me && w.has("bezahlt"):
		return Yes
	}
	return ""
}

// period: the question names a part of a year (dates are only for months
// or days) – not "pro Monat", which groups.
func (w question) period() bool {
	if w.contains("letzten monat", "letzter monat", "diesen monat", "diesem monat", "vormonat", "vorigen monat", " im monat",
		"woche", " seit ", "quartal", " tag ", " tagen ") || dateRe.MatchString(w.text) {
		return true
	}
	return slices.ContainsFunc(months, func(m string) bool { return slices.Contains(w.words, match.Fold(m)) })
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

var (
	yearRe       = regexp.MustCompile(`\b(19\d\d|20\d\d)(er)?\b( ?(euro|eur|€))?`)
	yearRangeRe  = regexp.MustCompile(`(?:zwischen|von) (20\d\d) (?:und|bis) (20\d\d)|\b(20\d\d) ?(?:bis|-|–) ?(20\d\d)\b`)
	sinceRe      = regexp.MustCompile(`seit (20\d\d)`)
	thisYearRe   = regexp.MustCompile(`(dieses|diesem|dieser|laufenden|aktuellen) (und \S+ )?jahr|heuer| dieses und `)
	lastYearRe   = regexp.MustCompile(` (letzte[snmr]?|vorige[snmr]?|vergangene[snmr]?) jahr\b|vorjahr`)
	beforeLastRe = regexp.MustCompile(` vorletzte[snmr]? jahr\b`)
	lastNRe      = regexp.MustCompile(`letzten (\d|zwei|drei|vier|fuenf) jahre`)
	groupYearRe  = regexp.MustCompile(`(pro|nach|je) jahr|jaehrlich|welche[mn]? jahr|alle jahre`)
)

var numberWords = map[string]int{"zwei": 2, "drei": 3, "vier": 4, "fuenf": 5}

// years: the years the question names ("2025", "2025 und 2026", "zwischen
// 2023 und 2025", "seit 2024", "dieses Jahr", "vorletztes Jahr", "in den
// letzten drei Jahren"). Without one, a follow-up keeps the years before
// (unless it asks about all), the model's years count if the question
// speaks of a year the rules cannot read, and else all years count.
func (w question) years(given []int, today civil.Date, follow bool, prev *Query) []int {
	var out []int
	add := func(y int) {
		if y >= 1990 && y <= today.Year()+1 && !slices.Contains(out, y) {
			out = append(out, y)
		}
	}
	addRange := func(a, b int) {
		for y := a; y <= b && y-a < 50; y++ {
			add(y)
		}
	}
	for _, m := range yearRangeRe.FindAllStringSubmatch(w.text, -1) {
		addRange(atoi(m[1]+m[3]), atoi(m[2]+m[4]))
	}
	for _, m := range sinceRe.FindAllStringSubmatch(w.text, -1) {
		addRange(atoi(m[1]), today.Year())
	}
	for _, m := range yearRe.FindAllStringSubmatch(w.text, -1) {
		if m[3] == "" { // "über 2000 Euro" is an amount
			add(atoi(m[1]))
		}
	}
	if thisYearRe.MatchString(w.text) {
		add(today.Year())
	}
	if lastYearRe.MatchString(w.text) {
		add(today.Year() - 1)
	}
	if beforeLastRe.MatchString(w.text) {
		add(today.Year() - 2)
	}
	if m := lastNRe.FindStringSubmatch(w.text); m != nil {
		n, ok := numberWords[m[1]]
		if !ok {
			n = atoi(m[1])
		}
		addRange(today.Year()-n+1, today.Year())
	}
	all := w.contains("alle jahre", "insgesamt", "gesamt", "ueberhaupt", "bisher")
	switch {
	case len(out) > 0:
		slices.Sort(out)
		return out
	case follow && !all:
		return slices.Clone(prev.Years)
	case w.has("jahr") && !groupYearRe.MatchString(w.text) && !w.contains("letzten jahren"):
		return slices.Clone(given) // a year the rules cannot read ("vor zwei Jahren"): the model's
	}
	return []int{}
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// group: the grouping the question asks for ("pro Jahr", "nach Monaten",
// "am teuersten", "in welchen Monaten").
func (w question) group(given string, follow bool, prev *Query) string {
	by := func(parts ...string) bool {
		for _, p := range parts {
			if w.contains("pro "+p, "nach "+p, "je "+p) {
				return true
			}
		}
		return false
	}
	ranked := w.most() || w.contains("am haeufigsten", "am oeftesten")
	switch {
	case by("monat") || w.contains("monatlich", "welchen monat", "welchem monat"):
		return GroupMonth
	case by("jahr") || w.contains("jaehrlich", "welchem jahr", "welchen jahren"):
		return GroupYear
	case by("kategorie", "art ", "bereich") || w.contains("welcher kategorie", "welchen kategorien"):
		return GroupCategory
	case by("kostentraeger", "traeger"):
		return GroupPayer
	case by("arzt", "aerzt", "behandler", "praxis") || ranked && w.has("arzt", "aerzt", "behandler", "praxis", "wem", "welche"):
		return GroupProvider
	case w.contains("pro ", "nach ", " je ") || ranked:
		return given
	case follow:
		return prev.GroupBy
	}
	return ""
}

// most: "am meisten", "am teuersten", "die meisten", "die höchsten".
func (w question) most() bool {
	return w.contains("am meisten", "am teuersten", "die meisten", "die hoechsten", "die teuersten")
}

// measure: what is summed, by the words of the question; a follow-up
// without such words keeps the measure before.
func (w question) measure(q Query, follow bool, prev *Query) string {
	switch q.Kind {
	case KindCount, KindPremium, KindOutstanding, KindUnsupported:
		return ""
	}
	payerAsked := q.Get(FieldPayer) != ""
	cue := ""
	switch {
	case sollIstRe.MatchString(w.text):
		return "" // the overview: SOLL, IST, difference, open
	case w.has("eigenanteil") || w.contains("selbst getragen", "selbst bezahlt"):
		cue = MeasureOwnShare
	case w.has("differenz", "abweichung") || q.Kind == KindSum && w.deviation() != "":
		cue = MeasureDiff // "Wie viel hat die Beihilfe weniger erstattet als erwartet?"
	case w.contains("nicht erstattet") || w.has("offen") || w.owing():
		cue = MeasureOwed
	case q.Kind == KindSum && (w.has("erstattet", "erstattung", "zurueckbekommen", "zurueckerstattet", "zurueckgezahlt") ||
		payerAsked && w.has("ueberwiesen", "bekommen", "gezahlt", "ausgezahlt")):
		cue = MeasureActual // "Wie viel hat die PKV überwiesen?": the payer paid – IST
	case w.has("gekostet", "kosten", "kostet", "teuer") ||
		q.Kind == KindSum && w.has("ausgegeben", "ausgaben", "bezahlt", "gesamtbetrag", "rechnungsbetrag"):
		cue = MeasureAmount // in a list "bezahlt" is a condition, not the measure
	case w.has("soll"):
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
