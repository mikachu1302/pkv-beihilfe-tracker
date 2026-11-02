package extract

import (
	"testing"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// Made-up documents in two typical layouts.

const beihilfeNotice = `
Beihilfestelle Beispiel
Bescheid vom 28.04.2026
Ihr Antrag vom 02.04.2026
Festsetzung der Beihilfe
Beleg  Datum       Leistungserbringer        Rechnungsbetrag  beihilfefähig  Satz     Beihilfe
1      03.03.2026  Dr. Muster                286,40           257,43         70,00 %  180,20
2      05.03.2026  Apotheke am Markt          45,10            45,10         70,00 %   31,57
3      10.03.2026  Apotheke am Markt          12,50            12,50         70,00 %    8,75
Summe                                         344,00           315,03                  220,52
Kostendämpfungspauschale                                                              -100,00
Auszahlungsbetrag                                                                      120,52
Der Betrag wird am 30.04.2026 auf Ihr Konto überwiesen.`

const pkvStatement = `
Muster Krankenversicherung AG
Leistungsabrechnung vom 15.05.2026
Behandlungsdatum  Leistungserbringer   Rechnungsbetrag  Erstattung
03.03.2026        Dr. Muster           286,40            85,92
05.03.2026        Apotheke am Markt     45,10            13,53
Gesamterstattung                                         99,45`

func TestBeihilfeNotice(t *testing.T) {
	d := AnalyzeRefund(lines(beihilfeNotice), now)
	if d.Date.String() != "2026-04-28" || d.ApplicationDate.String() != "2026-04-02" || d.PaidOut != 12052 {
		t.Errorf("header: date %s, application %s, paid out %s", d.Date, d.ApplicationDate, d.PaidOut)
	}
	if len(d.Deductions) != 1 || d.Deductions[0].Label != "Kostendämpfungspauschale" || d.Deductions[0].Amount != 10000 {
		t.Errorf("deductions: %+v", d.Deductions)
	}
	if len(d.Positions) != 3 {
		t.Fatalf("positions: %+v", d.Positions)
	}
	p := d.Positions[0]
	if p.Date.String() != "2026-03-03" || p.Provider != "Dr. Muster" || len(p.Amounts) != 3 {
		t.Errorf("first position: %+v", p)
	}
	layout := GuessLayout(d.Positions)
	if layout != (Layout{Billed: 1, Eligible: 2, Refund: -1}) {
		t.Errorf("layout %+v", layout)
	}
	billed, eligible, refund := layout.Apply(p.Amounts)
	if billed != 28640 || eligible != 25743 || refund != 18020 {
		t.Errorf("apply: %s %s %s", billed, eligible, refund)
	}
	var total money.Cents
	for _, p := range d.Positions {
		_, _, r := layout.Apply(p.Amounts)
		total += r
	}
	if total-d.Deductions[0].Amount != d.PaidOut {
		t.Errorf("checksum: %s − %s ≠ %s", total, d.Deductions[0].Amount, d.PaidOut)
	}
}

func TestPKVStatement(t *testing.T) {
	d := AnalyzeRefund(lines(pkvStatement), now)
	if d.Date.String() != "2026-05-15" || d.PaidOut != 9945 || len(d.Positions) != 2 || len(d.Deductions) != 0 {
		t.Fatalf("%+v", d)
	}
	layout := GuessLayout(d.Positions)
	if layout != (Layout{Billed: 1, Refund: -1}) {
		t.Errorf("layout %+v", layout)
	}
	if b, e, r := layout.Apply(d.Positions[1].Amounts); b != 4510 || e != 0 || r != 1353 {
		t.Errorf("apply: %s %s %s", b, e, r)
	}
	if d.Positions[1].Provider != "Apotheke am Markt" {
		t.Errorf("provider %q", d.Positions[1].Provider)
	}
}

func TestLayoutEdgeCases(t *testing.T) {
	if b, _, r := (Layout{Billed: 1, Refund: -1}).Apply([]money.Cents{500}); b != 0 || r != 500 {
		t.Errorf("single amount: %s %s", b, r)
	}
	if _, _, r := (Layout{Billed: 1, Refund: 4}).Apply([]money.Cents{1, 2}); r != 0 {
		t.Errorf("out of range: %s", r)
	}
	if d := AnalyzeRefund(lines("Sehr geehrte Damen und Herren"), now); len(d.Positions) != 0 || len(d.Notes) == 0 {
		t.Errorf("empty document: %+v", d)
	}
}

func TestRefundHeaderAndRemarksFromReview(t *testing.T) {
	d := AnalyzeRefund(lines(`
Beihilfestelle Beispiel
Max Beispiel, Geburtsdatum 14.02.1992
Bescheid vom 28.04.2026
03.03.2026  Dr. Muster  Kürzung nach Ziffer 5  286,40  257,43  180,20
05.03.2026  Apotheke     45,10   45,10   31,57
Eigenbehalt                         -10,00
Auszahlungsbetrag                  201,77`), now)
	if d.Date.String() != "2026-04-28" {
		t.Errorf("date %s (birth date taken?)", d.Date)
	}
	if len(d.Positions) != 2 || len(d.Deductions) != 1 || d.Deductions[0].Amount != 1000 {
		t.Errorf("positions %d, deductions %+v", len(d.Positions), d.Deductions)
	}
}
