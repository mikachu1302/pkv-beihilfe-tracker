package extract

import (
	"strings"
	"testing"
	"time"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// All documents below are made up.

var now = time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)

func lines(text string) []Line {
	var out []Line
	for _, t := range strings.Split(strings.TrimSpace(text), "\n") {
		out = append(out, Line{Text: t, Page: 1})
	}
	return out
}

const pvsInvoice = `
Privatärztliche Verrechnungsstelle Beispiel eG
Musterweg 1 · 12345 Musterstadt
Herrn
Max Beispiel
Hauptstraße 5
54321 Beispielort
Rechnung
Rechnungsnummer: 2026-004711          Rechnungsdatum: 14.03.2026
für die Behandlung durch Dr. med. Anna Muster, Fachärztin für Innere Medizin
Behandlungszeitraum: 03.03.2026 - 05.03.2026
Datum   GOÄ   Leistung                      Faktor   Betrag
03.03.2026  1   Beratung                     2,3   10,72
03.03.2026  5   Symptombezogene Untersuchung 2,3   10,72
05.03.2026  250 Blutentnahme                 1,8    4,20
Rechnungsbetrag 286,40 EUR
Bitte überweisen Sie den Betrag bis zum 13.04.2026 auf IBAN DE89 3704 0044 0532 0130 00`

const pvsQR = "BCD\n002\n1\nSCT\n\nPVS Beispiel eG\nDE89370400440532013000\nEUR286.40\n\n\nRechnung 2026-004711"

func TestBillingServiceInvoiceWithGiroCode(t *testing.T) {
	d := Analyze(lines(pvsInvoice), []string{"something else", pvsQR}, now)
	checks := []struct{ name, got, want string }{
		{"kind", d.Kind, KindInvoice},
		{"provider", d.Provider, "Dr. med. Anna Muster"},
		{"payee", d.Payee, "PVS Beispiel eG"},
		{"iban", d.IBAN, "DE89370400440532013000"},
		{"reference", d.Reference, "Rechnung 2026-004711"},
		{"invoiceDate", d.InvoiceDate.String(), "2026-03-14"},
		{"serviceFrom", d.ServiceFrom.String(), "2026-03-03"},
		{"serviceTo", d.ServiceTo.String(), "2026-03-05"},
		{"category", string(d.Category), string(model.CatDoctor)},
		{"amount source", d.Sources["amount"], SrcQR},
		{"provider source", d.Sources["provider"], SrcLabel},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if d.Amount != 28640 {
		t.Errorf("amount = %s", d.Amount)
	}
	// Without the QR code the amount comes from the label, IBAN from the text.
	d = Analyze(lines(pvsInvoice), nil, now)
	if d.Amount != 28640 || d.Sources["amount"] != SrcLabel || d.IBAN != "DE89370400440532013000" ||
		d.Reference != "2026-004711" || d.Payee != "Privatärztliche Verrechnungsstelle Beispiel eG" {
		t.Errorf("without QR: %+v", d)
	}
}

func TestPharmacyReceipt(t *testing.T) {
	d := Analyze(lines(`
Apotheke am Markt
Marktplatz 3, 12345 Musterstadt
Tel. 01234 5678
Kassenbeleg / Quittung
Datum: 05.03.2026 10:42
PZN 01234567 Ibuprofen 400       1 x 7,95
PZN 07654321 Nasenspray          1 x 4,50
Summe EUR 12,45
Bar 20,00
Rückgeld 7,55`), nil, now)
	if d.Kind != KindInvoice || d.Provider != "Apotheke am Markt" || d.Amount != 1245 || d.InvoiceDate.String() != "2026-03-05" ||
		d.Category != model.CatPharmacy || !d.ServiceFrom.IsZero() {
		t.Errorf("%+v", d)
	}
}

func TestDentalInvoice(t *testing.T) {
	d := Analyze(lines(`
Zahnarztpraxis Dr. Sonnenschein
Lindenallee 7, 12345 Musterstadt
Rechnung Nr. Z-2026-118
Datum: 20.02.2026
Datum       GOZ    Leistung                   Faktor  Betrag
10.02.2026  0010   Eingehende Untersuchung    2,3    12,94
10.02.2026  4050   Entfernung harter Beläge   2,3     3,62
17.02.2026  2060   Füllung                    3,5   120,40
Gesamtbetrag: 1.000,00 €`), nil, now)
	if d.Provider != "Zahnarztpraxis Dr. Sonnenschein" || d.Amount != 100000 || d.InvoiceDate.String() != "2026-02-20" ||
		d.ServiceFrom.String() != "2026-02-10" || d.ServiceTo.String() != "2026-02-17" || d.Reference != "Z-2026-118" ||
		d.Category != model.CatDentist {
		t.Errorf("%+v", d)
	}
}

func TestRefundDocumentAndUnknown(t *testing.T) {
	d := Analyze(lines(`
Beihilfestelle Beispiel
Beihilfebescheid
Ihr Antrag vom 02.04.2026
Die Beihilfe wird wie folgt festgesetzt:`), nil, now)
	if d.Kind != KindRefundDoc {
		t.Errorf("kind = %s", d.Kind)
	}
	if d := Analyze(lines("Liebe Grüße aus dem Urlaub"), nil, now); d.Kind != KindUnknown || d.Amount != 0 {
		t.Errorf("%+v", d)
	}
}

func TestLargestAmountAsFallback(t *testing.T) {
	d := Analyze(lines("Physiotherapie Bewegung\nKrankengymnastik 6 x 32,50 195,00\nVielen Dank"), nil, now)
	if d.Amount != 19500 || d.Sources["amount"] != SrcGuess || len(d.Notes) == 0 || d.Category != model.CatTherapy {
		t.Errorf("%+v", d)
	}
}

func TestAmounts(t *testing.T) {
	got := amounts("12,50 25,00 1.234,56 Faktor 2,3 Nr. 4711,995 14.03.2026")
	want := []money.Cents{1250, 2500, 123456}
	if len(got) != len(want) {
		t.Fatalf("amounts = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("amounts[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestParseEPC(t *testing.T) {
	e, ok := ParseEPC("BCD\r\n001\r\n1\r\nSCT\r\nCOBADEFFXXX\r\nPraxis Dr. Muster\r\nDE89 3704 0044 0532 0130 00\r\nEUR12.5\r\n\r\nRF18539007547034\r\n")
	if !ok || e.BIC != "COBADEFFXXX" || e.IBAN != "DE89370400440532013000" || e.Amount != 1250 || e.Reference != "RF18539007547034" {
		t.Errorf("%+v %v", e, ok)
	}
	if _, ok := ParseEPC("https://example.org"); ok {
		t.Error("URL taken for a GiroCode")
	}
	if !validIBAN("DE89370400440532013000") || validIBAN("DE89370400440532013001") {
		t.Error("IBAN checksum")
	}
}

func TestKindOfNoticeWithoutKeyword(t *testing.T) {
	d := Analyze(lines(`
Beihilfestelle Beispiel
Bescheid vom 20.10.2026
Ihr Antrag vom 01.09.2026
Datum       Leistungserbringer   Betrag   Satz     Beihilfe
05.08.2026  Labor Nord           57,60    70,00 %  40,32
Auszahlungsbetrag                                  40,32`), nil, now)
	if d.Kind != KindRefundDoc {
		t.Errorf("kind = %s", d.Kind)
	}
}
