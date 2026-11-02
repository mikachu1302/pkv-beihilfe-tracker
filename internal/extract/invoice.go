package extract

import (
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// Line is one line of text with its position on the page (0..1 of the page
// size; zero if unknown, e.g. for plain text).
type Line struct {
	Text string  `json:"text"`
	Page int     `json:"page"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	W    float64 `json:"w"`
	H    float64 `json:"h"`
}

// Document kinds.
const (
	KindInvoice   = "invoice"
	KindRefundDoc = "refund"
	KindUnknown   = "unknown"
)

// Source says where a value came from.
const (
	SrcQR    = "qr"    // GiroCode – reliable
	SrcLabel = "label" // next to a label such as "Rechnungsbetrag" – likely
	SrcGuess = "guess" // heuristic – check it
)

// Draft is what was recognised.
type Draft struct {
	Kind        string
	Provider    string
	Payee       string
	IBAN        string
	Reference   string
	Category    model.Category
	InvoiceDate civil.Date
	ServiceFrom civil.Date
	ServiceTo   civil.Date
	Amount      money.Cents
	Due         civil.Date
	// Sources maps field names (provider, amount, invoiceDate, …) to SrcQR/SrcLabel/SrcGuess.
	Sources map[string]string
	Notes   []string
}

var (
	dateRe      = regexp.MustCompile(`\b(\d{1,2})\.\s?(\d{1,2})\.\s?(\d{4}|\d{2})\b`)
	amountRe    = regexp.MustCompile(`\d{1,3}(?:\.\d{3})+,\d{2}|\d+,\d{2}`)
	ibanRe      = regexp.MustCompile(`\b([A-Z]{2}\d{2}(?:\s?[0-9A-Z]{4}){3,7}(?:\s?[0-9A-Z]{1,3})?)\b`)
	numberRe    = regexp.MustCompile(`(?i)(?:rechnungs?\s*-?\s*(?:nr|nummer)|re\.?\s*-?\s*nr|rechnung\s+nr|beleg\s*-?\s*nr|belegnummer)\.?\s*:?\s*([A-Z0-9][A-Z0-9/\-.]{2,24})`)
	treatedByRe = regexp.MustCompile(`(?i)(?:behandlung\s+durch|behandelnde[rn]?\s+(?:arzt|ärztin|zahnarzt|zahnärztin)\s*:?|leistungserbringer\s*:)\s+(.+?)(?:[,;:(]|$)`)
	// GebüH (Gebührenverzeichnis für Heilpraktiker), not "Gebühr" or "Gebührenordnung für Ärzte".
	gebuehRe = regexp.MustCompile(`gebüh(?:[^a-zäöüß]|$)`)
	streetRe = regexp.MustCompile(`(?i)(straße|strasse|str\.|weg\b|platz\b|allee\b|gasse\b|ring\b)\s*\d|\b\d{5}\s+[A-ZÄÖÜ][a-zäöüß]`)
)

// Words that point to a refund document or to an invoice (one point each).
var (
	refundWords = []string{"bescheid", "festsetzung", "festgesetzt", "antrag vom", "ihr antrag", "auszahlungsbetrag",
		"beihilfefähig", "erstattungsbetrag", "erstattung", "leistungsabrechnung", "abrechnungsschreiben", "überweisen wir", "beihilfe"}
	refundStrong = []string{"beihilfebescheid", "leistungsabrechnung", "festsetzung der beihilfe", "erstattungsabrechnung"}
	invoiceWords = []string{"rechnung", "goä", "goz", "quittung", "kassenbeleg", "zahlbar", "bitte überweisen", "zahlungsziel", "zu zahlen"}
)

var amountLabels = []struct {
	word   string
	weight int
}{
	{"zu zahlender betrag", 10}, {"zahlbetrag", 10}, {"rechnungsbetrag", 10}, {"endbetrag", 10}, {"gesamtbetrag", 9},
	{"rechnungssumme", 9}, {"gesamtsumme", 8}, {"zu zahlen", 8}, {"summe", 5}, {"gesamt", 4}, {"betrag", 2},
}

var invoiceDateLabels = []string{"rechnungsdatum", "rechnung vom", "datum der rechnung", "ausstellungsdatum", "belegdatum", "datum:", "datum"}

var serviceLabels = []string{"behandlungszeitraum", "leistungszeitraum", "behandlungsdatum", "behandlung vom", "behandlung am",
	"leistungsdatum", "zeitraum", "behandlungstag"}

var providerWords = []string{"dr.", "praxis", "mvz", "apotheke", "zahnarzt", "klinik", "krankenhaus", "physio", "radiolog",
	"labor", "facharzt", "ärzt", "heilpraktiker", "optik", "therapie", "medizinisch", "zentrum", "verrechnungsstelle", "abrechnungsstelle"}

// Analyze recognises a document from its lines and any QR payloads.
func Analyze(lines []Line, qr []string, now time.Time) Draft {
	d := Draft{Kind: KindUnknown, Sources: map[string]string{}}
	text := make([]string, len(lines))
	lower := make([]string, len(lines))
	for i, l := range lines {
		text[i] = strings.Join(strings.Fields(l.Text), " ")
		lower[i] = strings.ToLower(text[i])
	}
	all := strings.Join(lower, "\n")
	set := func(field, src string) { d.Sources[field] = src }

	// Kind: count indicators of both sides.
	score := func(words []string) int {
		n := 0
		for _, w := range words {
			if strings.Contains(all, w) {
				n++
			}
		}
		return n
	}
	refund, invoice := score(refundWords), score(invoiceWords)
	switch {
	case score(refundStrong) > 0 || refund >= 2 && refund > invoice:
		d.Kind = KindRefundDoc
	case invoice > 0 || len(qr) > 0:
		d.Kind = KindInvoice
	}

	// GiroCode first: amount, IBAN, payee and reference are exact.
	for _, p := range qr {
		if e, ok := ParseEPC(p); ok {
			d.Payee, d.IBAN = e.Name, e.IBAN
			set("payee", SrcQR)
			set("iban", SrcQR)
			if e.Amount > 0 {
				d.Amount = e.Amount
				set("amount", SrcQR)
			}
			if e.Reference != "" {
				d.Reference = e.Reference
				set("reference", SrcQR)
			}
			if d.Kind == KindUnknown {
				d.Kind = KindInvoice
			}
			break
		}
	}

	// Amount next to the strongest label (same line or the next one).
	if d.Amount == 0 {
		best, bestW := money.Cents(0), 0
		for i, l := range lower {
			for _, lab := range amountLabels {
				if !strings.Contains(l, lab.word) || lab.weight < bestW {
					continue
				}
				if v, ok := lastAmount(text[i]); ok {
					best, bestW = v, lab.weight
				} else if i+1 < len(text) {
					if v, ok := firstAmount(text[i+1]); ok {
						best, bestW = v, lab.weight
					}
				}
			}
		}
		if best > 0 {
			d.Amount = best
			set("amount", SrcLabel)
		} else if v := largestAmount(text); v > 0 {
			d.Amount = v
			set("amount", SrcGuess)
			d.Notes = append(d.Notes, "Kein „Rechnungsbetrag“ gefunden – größter Betrag übernommen")
		}
	}

	// Dates.
	maxYear := now.Year() + 1
	for _, lab := range invoiceDateLabels {
		if !d.InvoiceDate.IsZero() {
			break
		}
		for i, l := range lower {
			if at := labelIndex(l, lab); at >= 0 {
				if ds := datesIn(text[i][at:], maxYear); len(ds) > 0 {
					d.InvoiceDate = ds[0]
				} else if i+1 < len(text) {
					if ds := datesIn(text[i+1], maxYear); len(ds) > 0 {
						d.InvoiceDate = ds[0]
					}
				}
				if !d.InvoiceDate.IsZero() {
					set("invoiceDate", SrcLabel)
					break
				}
			}
		}
	}
	for _, lab := range serviceLabels {
		if !d.ServiceFrom.IsZero() {
			break
		}
		for i, l := range lower {
			if !strings.Contains(l, lab) {
				continue
			}
			ds := datesIn(text[i], maxYear)
			if len(ds) == 0 && i+1 < len(text) {
				ds = datesIn(text[i+1], maxYear)
			}
			if len(ds) > 0 {
				d.ServiceFrom = ds[0]
				if len(ds) > 1 && ds[len(ds)-1].After(ds[0]) {
					d.ServiceTo = ds[len(ds)-1]
				}
				set("serviceFrom", SrcLabel)
				break
			}
		}
	}
	// Treatment dates from the itemised lines (date + amount on one line).
	if d.ServiceFrom.IsZero() {
		var items []civil.Date
		for i := range text {
			if ds := datesIn(text[i], maxYear); len(ds) > 0 {
				if _, ok := lastAmount(text[i]); ok {
					items = append(items, ds[0])
				}
			}
		}
		if len(items) > 0 {
			slices.SortFunc(items, civil.Date.Compare)
			if d.InvoiceDate.IsZero() || !items[0].Equal(d.InvoiceDate) || len(items) > 1 {
				d.ServiceFrom = items[0]
				if last := items[len(items)-1]; last.After(items[0]) {
					d.ServiceTo = last
				}
				set("serviceFrom", SrcGuess)
			}
		}
	}
	if d.InvoiceDate.IsZero() {
		// The latest date in the upper part of the first page.
		var latest civil.Date
		for i, l := range lines {
			if l.Page > 1 || (l.Y > 0.4 && l.H > 0) {
				continue
			}
			for _, dt := range datesIn(text[i], maxYear) {
				if dt.After(latest) {
					latest = dt
				}
			}
		}
		if !latest.IsZero() {
			d.InvoiceDate = latest
			set("invoiceDate", SrcGuess)
		}
	}
	if !d.ServiceFrom.IsZero() && !d.InvoiceDate.IsZero() && d.ServiceFrom.After(d.InvoiceDate) {
		d.Notes = append(d.Notes, "Behandlungsdatum liegt nach dem Rechnungsdatum – bitte prüfen")
	}

	// Invoice number.
	if d.Reference == "" {
		for _, t := range text {
			if m := numberRe.FindStringSubmatch(t); m != nil {
				d.Reference = strings.TrimRight(m[1], ".")
				set("reference", SrcLabel)
				break
			}
		}
	}

	// IBAN printed on the invoice (a line may also hold a BIC or a tax number first).
	if d.IBAN == "" {
	ibans:
		for _, t := range text {
			for _, m := range ibanRe.FindAllStringSubmatch(strings.ToUpper(t), -1) {
				iban := strings.ReplaceAll(m[1], " ", "")
				if len(iban) >= 15 && validIBAN(iban) {
					d.IBAN = iban
					set("iban", SrcLabel)
					break ibans
				}
			}
		}
	}

	// Billing services put themselves in the letterhead and name the
	// practitioner in the text ("für die Behandlung durch Dr. …").
	for _, t := range text {
		if m := treatedByRe.FindStringSubmatch(t); m != nil {
			if name := personName(m[1]); name != "" {
				d.Provider = name
				set("provider", SrcLabel)
				break
			}
		}
	}
	if d.Provider == "" {
		d.Provider = provider(lines, text, lower)
		if d.Provider != "" {
			set("provider", SrcGuess)
		}
	}
	if d.Payee == "" && d.Sources["provider"] == SrcLabel {
		if head := provider(lines, text, lower); head != "" && head != d.Provider {
			d.Payee = head
			set("payee", SrcGuess)
		}
	}
	d.Category = category(all)
	if d.Category != "" {
		set("category", SrcGuess)
	}
	return d
}

func datesIn(s string, maxYear int) []civil.Date {
	var out []civil.Date
	for _, m := range dateRe.FindAllStringSubmatch(s, -1) {
		dt, err := civil.Parse(m[1] + "." + m[2] + "." + m[3])
		if err != nil {
			continue
		}
		if y := dt.Year(); y < 1990 || y > maxYear {
			continue
		}
		out = append(out, dt)
	}
	return out
}

// amounts returns German-formatted amounts ("1.234,56", "12,50") that stand
// on their own (not part of a longer number).
func amounts(s string) []money.Cents {
	var out []money.Cents
	isDigit := func(c byte) bool { return c >= '0' && c <= '9' }
	for _, loc := range amountRe.FindAllStringIndex(s, -1) {
		a, b := loc[0], loc[1]
		if a > 0 && (isDigit(s[a-1]) || s[a-1] == ',' || s[a-1] == '.') {
			continue
		}
		if b < len(s) && isDigit(s[b]) {
			continue
		}
		if v, err := money.Parse(s[a:b]); err == nil && v > 0 {
			out = append(out, v)
		}
	}
	return out
}

func lastAmount(s string) (money.Cents, bool) {
	a := amounts(s)
	if len(a) == 0 {
		return 0, false
	}
	return a[len(a)-1], true
}

func firstAmount(s string) (money.Cents, bool) {
	a := amounts(s)
	if len(a) == 0 {
		return 0, false
	}
	return a[0], true
}

func largestAmount(text []string) money.Cents {
	var best money.Cents
	for _, t := range text {
		for _, v := range amounts(t) {
			if v > best && v < 10_000_000 {
				best = v
			}
		}
	}
	return best
}

// provider picks the most likely name of the practice from the header.
func provider(lines []Line, text, lower []string) string {
	type cand struct {
		i     int
		score float64
	}
	var cands []cand
	limit := min(len(text), 25)
	salutation := func(l string) bool {
		return strings.HasPrefix(l, "herr") || strings.HasPrefix(l, "frau") || strings.HasPrefix(l, "familie")
	}
	for i := 0; i < limit; i++ {
		t, l := text[i], lower[i]
		if len(t) < 3 || len(t) > 70 || dateRe.MatchString(t) || streetRe.MatchString(t) || strings.ContainsAny(t, "@") {
			continue
		}
		if strings.HasPrefix(l, "rechnung") || strings.HasPrefix(l, "tel") || strings.Contains(l, "fax") || strings.Contains(l, "www.") ||
			strings.Contains(l, "iban") || strings.Contains(l, "seite") || salutation(l) || (i > 0 && salutation(lower[i-1])) {
			continue // header noise, or the patient's name after "Herrn"/"Frau"
		}
		score := 1.0 - float64(i)*0.03
		for _, w := range providerWords {
			if strings.Contains(l, w) {
				score += 2
				break
			}
		}
		if i < len(lines) && lines[i].H > 0 {
			score += math.Min(lines[i].H*40, 2) // bigger letters in the letterhead
			if lines[i].Y < 0.2 {
				score += 0.5
			}
		}
		cands = append(cands, cand{i, score})
	}
	if len(cands) == 0 {
		return ""
	}
	slices.SortStableFunc(cands, func(a, b cand) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		}
		return 0
	})
	return text[cands[0].i]
}

// labelIndex returns where label starts in line at the beginning of a word
// ("datum" in "Datum: …", not in "Geburtsdatum"), or -1.
func labelIndex(line, label string) int {
	for i := 0; i < len(line); {
		j := strings.Index(line[i:], label)
		if j < 0 {
			return -1
		}
		at := i + j
		if r, _ := utf8.DecodeLastRuneInString(line[:at]); at == 0 || !unicode.IsLetter(r) {
			return at
		}
		i = at + 1
	}
	return -1
}

// nameParticles may appear in lower case inside a name.
var nameParticles = map[string]bool{"von": true, "van": true, "de": true, "der": true, "zu": true, "und": true, "u.": true, "&": true}

// personName cuts the name off the text that follows it on the same line:
// "Dr. med. Anna Muster folgende Leistungen" → "Dr. med. Anna Muster".
// Names are capitalised; lower-case words end them, except particles and
// abbreviated titles such as "med." or "dent.".
func personName(s string) string {
	var keep []string
	for _, w := range strings.Fields(s) {
		r, _ := utf8.DecodeRuneInString(w)
		if unicode.IsUpper(r) || unicode.IsDigit(r) || nameParticles[w] || strings.HasSuffix(w, ".") && len(w) <= 7 {
			keep = append(keep, w)
			continue
		}
		break
	}
	for len(keep) > 0 && nameParticles[keep[len(keep)-1]] {
		keep = keep[:len(keep)-1]
	}
	return strings.TrimRight(strings.Join(keep, " "), " .-–")
}

func category(all string) model.Category {
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(all, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("goz", "zahnarzt", "zahnärzt", "kieferorthop"):
		return model.CatDentist
	case has("apotheke", "pzn"):
		return model.CatPharmacy
	case has("physiotherap", "krankengymnastik", "manuelle lymphdrainage", "heilmittel", "ergotherap", "logopäd"):
		return model.CatTherapy
	case has("heilpraktiker"), gebuehRe.MatchString(all):
		return model.CatNaturopath
	case has("krankenhaus", "stationär", "wahlleistung"):
		return model.CatHospital
	case has("optik", "brillengläser", "hörgerät", "sanitätshaus", "hilfsmittel"):
		return model.CatAids
	case has("goä", "goa ", "ärztliche leistung", "facharzt", "praxis"):
		return model.CatDoctor
	}
	return ""
}

// validIBAN checks the ISO 13616 checksum.
func validIBAN(iban string) bool {
	if len(iban) < 15 || len(iban) > 34 {
		return false
	}
	r := iban[4:] + iban[:4]
	rem := 0
	for _, c := range r {
		var v int
		switch {
		case c >= '0' && c <= '9':
			v = int(c - '0')
		case c >= 'A' && c <= 'Z':
			v = int(c-'A') + 10
		default:
			return false
		}
		if v >= 10 {
			rem = (rem*100 + v) % 97
		} else {
			rem = (rem*10 + v) % 97
		}
	}
	return rem == 1
}
