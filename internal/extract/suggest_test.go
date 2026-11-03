package extract

import (
	"slices"
	"strings"
	"testing"
)

func doc(name string, texts ...string) DocText { return DocText{Name: name, Lines: pages(texts...)} }

// without drops the lines containing any of the parts.
func without(text string, parts ...string) string {
	var keep []string
	for _, l := range strings.Split(text, "\n") {
		if !slices.ContainsFunc(parts, func(p string) bool { return strings.Contains(l, p) }) {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

func TestPageMarkers(t *testing.T) {
	for text, want := range map[string]PageMarker{
		"Seite 2 von 3":               {2, 3},
		"Seite2/3":                    {2, 3},
		"- Blatt 1 aus 2 -":           {1, 2},
		"S. 4/4":                      {4, 4},
		"Seite 2 vön 3 (OCR)":         {2, 3},
		"Rechnung R-1 · Seite 1 v. 2": {1, 2},
	} {
		got := PageMarkers([]Line{{Text: text, Page: 1}})
		if got[1] != want {
			t.Errorf("%q: %+v, want %+v", text, got[1], want)
		}
	}
	for _, text := range []string{"Seite 3 von 2", "Seite 1 von 1", "Seite 0 von 4", "Seitenzahl 2/3", "Kassenseite 1/2"} {
		if got := PageMarkers([]Line{{Text: text, Page: 1}}); len(got) != 0 {
			t.Errorf("%q: %+v", text, got)
		}
	}
}

func TestSuggestByPageNumbers(t *testing.T) {
	got := SuggestGroups([]DocText{
		doc("c.jpg", orthoPage3), doc("brief.pdf", pvsInvoice), doc("a.jpg", orthoPage1), doc("b.jpg", orthoPage2),
	}, now)
	if len(got) != 1 || !slices.Equal(got[0].Names, []string{"a.jpg", "b.jpg", "c.jpg"}) || got[0].Reason != "Seiten 1, 2 und 3 von 3" {
		t.Errorf("suggestions %+v", got)
	}
	// Two of three pages are there: they are suggested already.
	got = SuggestGroups([]DocText{doc("c.jpg", orthoPage3), doc("a.jpg", orthoPage1)}, now)
	if len(got) != 1 || !slices.Equal(got[0].Names, []string{"a.jpg", "c.jpg"}) || got[0].Reason != "Seite 1 und 3 von 3" {
		t.Errorf("partial: %+v", got)
	}
}

func TestNoSuggestionWhenAmbiguous(t *testing.T) {
	// Two first pages and one second page that nothing tells apart.
	first := "Seite 1 von 2\nPosition 1   10,00"
	second := "Seite 2 von 2\nPosition 2   20,00"
	if got := SuggestGroups([]DocText{doc("a", first), doc("b", first+"\nPosition 9   1,00"), doc("c", second)}, now); len(got) != 0 {
		t.Errorf("ambiguous: %+v", got)
	}
	// Different invoice numbers contradict the page numbers.
	one := "Rechnungsnummer: 2026-11111\nSeite 1 von 2"
	two := "Rechnungsnummer: 2026-22222\nSeite 2 von 2"
	if got := SuggestGroups([]DocText{doc("a", one), doc("b", two)}, now); len(got) != 0 {
		t.Errorf("contradicting numbers: %+v", got)
	}
}

func TestSuggestBySameInvoiceNumber(t *testing.T) {
	p1 := without(orthoPage1, "Seite 1 von 3")
	p2 := without(orthoPage2, "Seite 2 von 3")
	got := SuggestGroups([]DocText{doc("zwei.jpg", p2), doc("eins.jpg", p1)}, now)
	if len(got) != 1 || !slices.Equal(got[0].Names, []string{"eins.jpg", "zwei.jpg"}) || got[0].Reason != "gleiche Rechnungsnummer" {
		t.Errorf("suggestions %+v", got)
	}
	// The same complete invoice twice (a second copy) is no multi-page document.
	if got := SuggestGroups([]DocText{doc("a", pvsInvoice), doc("b", pvsInvoice)}, now); len(got) != 0 {
		t.Errorf("copies: %+v", got)
	}
}

func TestSuggestContinuationPage(t *testing.T) {
	head := without(orthoPage1, "Seite 1 von 3", "Rechnungsnummer", "Zwischensumme")
	tail := without(orthoPage3, "Seite 3 von 3")
	got := SuggestGroups([]DocText{doc("ende.jpg", tail), doc("brief.pdf", pvsInvoice), doc("anfang.jpg", head)}, now)
	if len(got) != 1 || !slices.Equal(got[0].Names, []string{"anfang.jpg", "ende.jpg"}) || got[0].Reason != "Folgeseite ohne eigenen Briefkopf" {
		t.Errorf("suggestions %+v", got)
	}
	// Two candidates for the tail: no suggestion.
	other := without(orthoPage3, "Seite 3 von 3", "Rechnungsbetrag") + "\nRechnungsbetrag   99,00"
	if got := SuggestGroups([]DocText{doc("ende.jpg", tail), doc("anfang.jpg", head), doc("noch.jpg", other)}, now); len(got) != 0 {
		t.Errorf("ambiguous tail: %+v", got)
	}
}

func TestSuggestionsDoNotMixKinds(t *testing.T) {
	got := SuggestGroups([]DocText{doc("bescheid.jpg", noticePage1), doc("rechnung.jpg", orthoPage2)}, now)
	for _, s := range got {
		if slices.Contains(s.Names, "bescheid.jpg") && slices.Contains(s.Names, "rechnung.jpg") {
			t.Errorf("mixed: %+v", s)
		}
	}
	// A Bescheid over two pages is suggested like an invoice.
	got = SuggestGroups([]DocText{doc("s2.jpg", noticePage2), doc("s1.jpg", noticePage1)}, now)
	if len(got) != 1 || !slices.Equal(got[0].Names, []string{"s1.jpg", "s2.jpg"}) {
		t.Errorf("Bescheid: %+v", got)
	}
}
