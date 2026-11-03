package extract

import (
	"strings"
	"testing"
)

// pages turns made-up page texts into lines numbered through, as the
// frontend sends them for a document of several files.
func pages(texts ...string) []Line {
	var out []Line
	for i, text := range texts {
		for _, t := range strings.Split(strings.TrimSpace(text), "\n") {
			out = append(out, Line{Text: t, Page: i + 1})
		}
	}
	return out
}

const orthoPage1 = `
Dr. med. Petra Beispiel
Fachärztin für Orthopädie
Beispielallee 7 · 12345 Musterstadt
Seite 1 von 3
Frau
Erika Muster
Lindenweg 3
54321 Beispielort
Rechnung
Rechnungsnummer: R-2026-0815        Rechnungsdatum: 21.09.2026
Behandlungszeitraum: 01.09.2026 - 15.09.2026
Datum   GOÄ   Leistung                  Faktor   Betrag
01.09.2026  1    Beratung                 2,3     10,72
01.09.2026  7    Untersuchung             2,3     21,45
Zwischensumme                                     120,50`

const orthoPage2 = `
Dr. med. Petra Beispiel · Rechnung R-2026-0815
Seite 2 von 3
Übertrag                                          120,50
08.09.2026  410  Ultraschall              2,3     26,81
Zwischensumme                                     230,10`

const orthoPage3 = `
Seite 3 von 3
Übertrag                                          230,10
15.09.2026  3306 Chirotherapie            2,3     19,57
Rechnungsbetrag                                   312,40 EUR
Bitte überweisen Sie den Betrag bis zum 21.10.2026
IBAN DE89 3704 0044 0532 0130 00`

func TestInvoiceOverThreePages(t *testing.T) {
	d := Analyze(pages(orthoPage1, orthoPage2, orthoPage3), nil, now)
	checks := []struct{ name, got, want string }{
		{"kind", d.Kind, KindInvoice},
		{"provider", d.Provider, "Dr. med. Petra Beispiel"},
		{"reference", d.Reference, "R-2026-0815"},
		{"invoiceDate", d.InvoiceDate.String(), "2026-09-21"},
		{"serviceFrom", d.ServiceFrom.String(), "2026-09-01"},
		{"serviceTo", d.ServiceTo.String(), "2026-09-15"},
		{"amount", d.Amount.String(), "312.40"},
		{"amount source", d.Sources["amount"], SrcLabel},
		{"iban", d.IBAN, "DE89370400440532013000"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestInvoicePagesWithPlainSum(t *testing.T) {
	// Only "Summe" on the last page: the sub totals before it must not win.
	last := strings.Replace(orthoPage3, "Rechnungsbetrag", "Summe", 1)
	d := Analyze(pages(orthoPage1, orthoPage2, last), nil, now)
	if d.Amount.String() != "312.40" {
		t.Errorf("amount = %s, want 312.40", d.Amount)
	}
}

func TestPageMarkerNeverProvider(t *testing.T) {
	// A continuation page photographed first: its page line is no provider.
	// It has no letterhead at all.
	d := Analyze(pages(orthoPage3), nil, now)
	if d.Provider != "" {
		t.Errorf("provider = %q, want none", d.Provider)
	}
}

const noticePage1 = `
Beihilfestelle Beispiel
Bescheid vom 28.04.2026
Ihr Antrag vom 02.04.2026
Seite 1 von 2
Festsetzung der Beihilfe
Beleg  Datum       Leistungserbringer        Rechnungsbetrag  beihilfefähig  Satz     Beihilfe
1      03.03.2026  Dr. Muster                286,40           257,43         70,00 %  180,20
2      05.03.2026  Apotheke am Markt          45,10            45,10         70,00 %   31,57`

const noticePage2 = `
Beihilfestelle Beispiel · Bescheid vom 28.04.2026
Seite 2 von 2
Beleg  Datum       Leistungserbringer        Rechnungsbetrag  beihilfefähig  Satz     Beihilfe
3      10.03.2026  Apotheke am Markt          12,50            12,50         70,00 %    8,75
Summe                                         344,00           315,03                  220,52
Kostendämpfungspauschale                                                              -100,00
Auszahlungsbetrag                                                                      120,52`

func TestNoticeOverTwoPages(t *testing.T) {
	d := AnalyzeRefund(pages(noticePage1, noticePage2), now)
	if d.Date.String() != "2026-04-28" || d.ApplicationDate.String() != "2026-04-02" || d.PaidOut != 12052 {
		t.Errorf("header: date %s, application %s, paid out %s", d.Date, d.ApplicationDate, d.PaidOut)
	}
	if len(d.Positions) != 3 {
		t.Fatalf("positions: %+v", d.Positions)
	}
	if p := d.Positions[2]; p.Date.String() != "2026-03-10" || p.Provider != "Apotheke am Markt" {
		t.Errorf("position on page 2: %+v", p)
	}
	if len(d.Deductions) != 1 || d.Deductions[0].Amount != 10000 {
		t.Errorf("deductions: %+v", d.Deductions)
	}
}
