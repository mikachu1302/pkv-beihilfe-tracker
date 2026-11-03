package extract

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Suggestions which inbox documents are pages of one document. They are
// based on the content only (page numbers, invoice numbers, letterhead and
// total) – never on when the files arrived – and only made when they are
// unambiguous. Merging is always left to the user.

// DocText is the recognised text of one inbox document.
type DocText struct {
	Name  string
	Lines []Line
	QR    []string
}

// GroupSuggestion proposes to merge documents, in page order.
type GroupSuggestion struct {
	Names  []string
	Reason string
}

// PageMarker is a page number printed on a page: "Seite 2 von 3".
type PageMarker struct {
	Page, Of int
}

// pageMarkerRe finds "Seite 2 von 3", "Seite 2/3", "Blatt 2 von 3", "S. 2/3",
// also as OCR reads them ("Seite2von3", "Seite 2 vön 3", "Seite 2 v. 3").
var pageMarkerRe = regexp.MustCompile(`(?i)\b(?:seite|blatt|s\.)\s*(\d{1,2})\s*(?:von|vön|v\.|aus|/)\s*(\d{1,2})\b`)

// PageMarkers returns the page numbers printed on the lines, at most one per page.
func PageMarkers(lines []Line) map[int]PageMarker {
	out := map[int]PageMarker{}
	for _, l := range lines {
		if _, ok := out[l.Page]; ok {
			continue
		}
		m := pageMarkerRe.FindStringSubmatch(l.Text)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		of, _ := strconv.Atoi(m[2])
		if n >= 1 && of >= 2 && n <= of && of <= 50 {
			out[l.Page] = PageMarker{n, of}
		}
	}
	return out
}

// docFacts is what the suggestions look at in one document.
type docFacts struct {
	i        int // position in the input (the inbox order)
	name     string
	kind     string
	ref      string // invoice number, upper case letters and digits only
	provider string // lower case, letters and digits only
	iban     string
	header   bool // letterhead or a labelled invoice date
	dated    bool // a labelled invoice date: usually the first page
	total    bool // a labelled total or a GiroCode amount
	pages    []int
	of       int        // page total of the printed page numbers (0: none)
	tokens   [][]string // normalised words per line, to find another document's number
}

// amountWord is a word that is an amount ("204,31", "1.234,56").
var amountWord = regexp.MustCompile(`^\d{1,3}(?:\.\d{3})*,\d{2}\D*$`)

var leadingWords = regexp.MustCompile(`^(?i)(?:rechnung(?:snummer)?|rg|re|nr|no)\.?\s*`)

func normRef(s string) string {
	s = leadingWords.ReplaceAllString(strings.TrimSpace(s), "")
	out := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToUpper(r)
		}
		return -1
	}, s)
	if len([]rune(out)) < 5 || !strings.ContainsAny(out, "0123456789") {
		return "" // too short or without digits: no telling invoice number
	}
	return out
}

func normWord(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToUpper(r)
		}
		return -1
	}, s)
}

func facts(i int, d DocText, now time.Time) docFacts {
	a := Analyze(d.Lines, d.QR, now)
	f := docFacts{i: i, name: d.Name, kind: a.Kind, iban: a.IBAN,
		provider: strings.ToLower(normWord(a.Provider)),
		header:   a.Provider != "" || a.Sources["invoiceDate"] == SrcLabel,
		dated:    a.Sources["invoiceDate"] == SrcLabel,
		total:    a.Sources["amount"] == SrcQR || finalTotal(d.Lines)}
	if a.Sources["reference"] != "" {
		f.ref = normRef(a.Reference)
	}
	markers := PageMarkers(d.Lines)
	pages := make([]int, 0, len(markers))
	for p := range markers {
		pages = append(pages, p)
	}
	slices.Sort(pages) // the first page's total decides
	for _, p := range pages {
		if m := markers[p]; f.of == 0 || m.Of == f.of {
			f.of = m.Of
			f.pages = append(f.pages, m.Page)
		}
	}
	slices.Sort(f.pages)
	for _, l := range d.Lines {
		var words []string
		for _, w := range strings.Fields(l.Text) {
			if amountWord.MatchString(w) {
				continue // an amount is never an invoice number
			}
			if n := normWord(w); n != "" {
				words = append(words, n)
			}
		}
		f.tokens = append(f.tokens, words)
	}
	return f
}

// totalLabels name the amount to pay. Sub totals ("Zwischensumme",
// "Übertrag") and column heads ("Betrag") do not count.
var totalLabels = []string{"zu zahlender betrag", "zahlbetrag", "rechnungsbetrag", "endbetrag", "gesamtbetrag", "rechnungssumme",
	"gesamtsumme", "zu zahlen", "auszahlungsbetrag", "gesamterstattung", "summe", "gesamt"}

// finalTotal reports whether the page shows the total of the document –
// usually on the last page.
func finalTotal(lines []Line) bool {
	for i, l := range lines {
		low := strings.ToLower(l.Text)
		if strings.Contains(low, "zwischen") || strings.Contains(low, "übertrag") {
			continue
		}
		if !slices.ContainsFunc(totalLabels, func(w string) bool { return strings.Contains(low, w) }) {
			continue
		}
		if len(amounts(l.Text)) > 0 || (i+1 < len(lines) && len(amounts(lines[i+1].Text)) > 0) {
			return true
		}
	}
	return false
}

// mentions reports whether the document's text contains ref (with letters
// also split into up to three words: "R 2026 0815"). A number of digits only
// must have at least 7 and stand as one word – shorter ones are found in
// postcodes, dates and amounts all the time.
func (f *docFacts) mentions(ref string) bool {
	if ref == "" {
		return false
	}
	numeric := strings.Trim(ref, "0123456789") == ""
	if numeric && len(ref) < 7 {
		return false
	}
	join := 3
	if numeric {
		join = 1
	}
	for _, words := range f.tokens {
		for i := range words {
			joined := ""
			for j := i; j < len(words) && j < i+join; j++ {
				joined += words[j]
				if joined == ref {
					return true
				}
				if len(joined) >= len(ref) {
					break
				}
			}
		}
	}
	return false
}

// sameSender reports positive evidence that two pages come from the same
// sender: the same IBAN or the same provider.
func sameSender(a, b *docFacts) bool {
	if a.iban != "" && a.iban == b.iban {
		return true
	}
	return a.provider != "" && b.provider != "" && (strings.Contains(a.provider, b.provider) || strings.Contains(b.provider, a.provider))
}

func kindsFit(a, b string) bool { return a == b || a == KindUnknown || b == KindUnknown }

// contradict reports whether two documents name different invoices or senders.
func contradict(a, b *docFacts) bool {
	switch {
	case a.ref != "" && b.ref != "" && a.ref != b.ref:
		return true
	case a.iban != "" && b.iban != "" && a.iban != b.iban:
		return true
	case a.provider != "" && b.provider != "" && !strings.Contains(a.provider, b.provider) && !strings.Contains(b.provider, a.provider):
		return true
	}
	return !kindsFit(a.kind, b.kind)
}

// SuggestGroups proposes which of the documents (in inbox order) belong
// together. Each document is in at most one suggestion.
func SuggestGroups(docs []DocText, now time.Time) []GroupSuggestion {
	all := make([]docFacts, len(docs))
	for i, d := range docs {
		all[i] = facts(i, d, now)
	}
	used := map[int]bool{}
	var out []GroupSuggestion
	add := func(group []*docFacts, reason string) {
		for _, f := range group {
			used[f.i] = true
		}
		out = append(out, GroupSuggestion{Names: order(group), Reason: reason})
	}

	// 1. Printed page numbers with the same page total.
	byTotal := map[int][]*docFacts{}
	var totals []int
	for i := range all {
		if f := &all[i]; f.of > 0 {
			if byTotal[f.of] == nil {
				totals = append(totals, f.of)
			}
			byTotal[f.of] = append(byTotal[f.of], f)
		}
	}
	for _, of := range totals {
		for _, group := range consistentGroups(byTotal[of]) {
			if len(group) >= 2 {
				add(group, pagesReason(group, of))
			}
		}
	}

	// 2. One document's invoice number on another one.
	for i := range all {
		a := &all[i]
		if used[a.i] || a.ref == "" {
			continue
		}
		group := []*docFacts{a}
		for j := range all {
			b := &all[j]
			if b.i == a.i || used[b.i] || (b.ref != a.ref && !b.mentions(a.ref)) ||
				slices.ContainsFunc(group, func(m *docFacts) bool { return contradict(m, b) }) {
				continue
			}
			if a.header && a.total && b.header && b.total {
				continue // two complete documents (a reminder, a second copy): not pages of one
			}
			group = append(group, b)
		}
		if len(group) >= 2 {
			add(group, "gleiche Rechnungsnummer")
		}
	}

	// 3. A page with letterhead but no total and a page with a total but no
	// letterhead, with the same IBAN or provider – only when there is exactly
	// one of each left.
	var heads, tails []*docFacts
	for i := range all {
		f := &all[i]
		switch {
		case used[f.i]:
		case f.header && !f.total:
			heads = append(heads, f)
		case !f.header && f.total:
			tails = append(tails, f)
		}
	}
	if len(heads) == 1 && len(tails) == 1 && !contradict(heads[0], tails[0]) && heads[0].kind != KindRefundDoc &&
		sameSender(heads[0], tails[0]) {
		add([]*docFacts{heads[0], tails[0]}, "Folgeseite ohne eigenen Briefkopf")
	}
	return out
}

// consistentGroups splits documents with the same page total into groups
// that do not contradict each other. A group in which two documents claim
// the same page is ambiguous and dropped.
func consistentGroups(docs []*docFacts) [][]*docFacts {
	var groups [][]*docFacts
	for _, d := range docs {
		placed := false
		for gi, g := range groups {
			if !slices.ContainsFunc(g, func(o *docFacts) bool { return contradict(o, d) }) {
				groups[gi] = append(g, d)
				placed = true
				break
			}
		}
		if !placed {
			groups = append(groups, []*docFacts{d})
		}
	}
	var out [][]*docFacts
	for _, g := range groups {
		seen := map[int]bool{}
		clash := false
		for _, d := range g {
			for _, p := range d.pages {
				clash = clash || seen[p]
				seen[p] = true
			}
		}
		if !clash {
			out = append(out, g)
		}
	}
	return out
}

// order sorts a group into page order: by printed page numbers; else the
// page with the invoice date (or letterhead) first and the one with the
// total last; else the inbox order.
func order(group []*docFacts) []string {
	rank := func(f *docFacts) int {
		switch {
		case len(f.pages) > 0:
			return f.pages[0] * 10
		case f.dated:
			return 0
		case f.total:
			return 1000
		case f.header:
			return 100
		}
		return 500
	}
	sorted := slices.Clone(group)
	slices.SortStableFunc(sorted, func(a, b *docFacts) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		return a.i - b.i
	})
	names := make([]string, len(sorted))
	for i, f := range sorted {
		names[i] = f.name
	}
	return names
}

// pagesReason: "Seite 1 und 2 von 2", "Seiten 1, 2 und 3 von 3".
func pagesReason(group []*docFacts, of int) string {
	var pages []int
	for _, f := range group {
		pages = append(pages, f.pages...)
	}
	slices.Sort(pages)
	parts := make([]string, len(pages))
	for i, p := range pages {
		parts[i] = strconv.Itoa(p)
	}
	list := parts[0]
	if len(parts) > 1 {
		list = strings.Join(parts[:len(parts)-1], ", ") + " und " + parts[len(parts)-1]
	}
	word := "Seite"
	if len(parts) > 2 {
		word = "Seiten"
	}
	return fmt.Sprintf("%s %s von %d", word, list, of)
}
