package check

import (
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// The made-up patient of scripts/ki-testbelege.py (Beihilfe 50 %, PKV 50 %)
// with the documents as the interpretation of the AI's transcription
// returns them (internal/airead/testdata/abgleich-erwartet.json).

var d = civil.MustParse

func eur(s string) money.Cents {
	c, err := money.Parse(s)
	if err != nil {
		panic(err)
	}
	return c
}

func settings() *model.Settings {
	return &model.Settings{
		Payers: []model.Payer{
			{ID: "bh", Kind: model.Beihilfe, Name: "Beihilfestelle Musterland"},
			{ID: "pkv", Kind: model.PKV, Name: "Muster Krankenversicherung"},
		},
		Coverage: []model.CoveragePeriod{
			{From: d("2020-01-01"), Shares: []model.Share{{PayerID: "bh", Rate: money.Percent(50)}, {PayerID: "pkv", Rate: money.Percent(50)}}},
		},
	}
}

func submitted() map[string]model.Claim {
	m := model.Mark{Date: d("2026-11-04")}
	return map[string]model.Claim{"bh": {Submitted: m}, "pkv": {Submitted: m}}
}

func goaeInvoice() *model.Invoice {
	return &model.Invoice{ID: "goae", Provider: "Dr. med. Hans Probe", Payee: "Muster-PVS GmbH",
		IBAN: "DE48120300004455667788", Reference: "R-2026-1187", Category: model.CatDoctor,
		InvoiceDate: d("2026-10-12"), ServiceFrom: d("2026-09-08"), ServiceTo: d("2026-09-29"),
		Amount: eur("243,87"), Claims: submitted()}
}

func gozInvoice() *model.Invoice {
	return &model.Invoice{ID: "goz", Provider: "Dr. Ruth Beispiel", IBAN: "DE75250100309182736450",
		Reference: "ZA-2026-0412", Category: model.CatDentist,
		InvoiceDate: d("2026-11-03"), ServiceFrom: d("2026-10-06"), ServiceTo: d("2026-10-27"),
		Amount: eur("931,59"), Claims: submitted()}
}

func physioInvoice() *model.Invoice {
	return &model.Invoice{ID: "physio", Provider: "Physiotherapie Lotte Probe", IBAN: "DE18370501981234509876",
		Reference: "PT-2026-091", Category: model.CatTherapy,
		InvoiceDate: d("2026-10-23"), ServiceFrom: d("2026-09-22"), ServiceTo: d("2026-10-13"),
		Amount: eur("311,00"), Claims: submitted()}
}

func pos(date, tooth, code, text string, n int, amount string, page int) Position {
	p := Position{Tooth: tooth, Code: code, Text: text, Count: n, Amount: eur(amount), Page: page}
	if date != "" {
		p.Date = d(date)
	}
	return p
}

// goaeBill: a GOÄ invoice through a billing service, 2 pages with a carry-over.
func goaeBill() *Bill {
	return &Bill{Source: "rechnung-goae.pdf", Provider: "Hans Probe", Payee: "Muster-PVS GmbH",
		Reference: "R-2026-1187", IBAN: "DE48 1203 0000 4455 6677 88", Date: d("2026-10-12"),
		Total: eur("243,87"),
		Positions: []Position{
			pos("2026-09-08", "", "1", "Beratung – auch mittels Fernsprecher", 1, "10,72", 1),
			pos("2026-09-08", "", "7", "Vollständige körperliche Untersuchung mindestens eines Organsystems", 1, "21,45", 1),
			pos("2026-09-08", "", "250", "Blutentnahme mittels Spritze, Kanüle oder Katheter aus der Vene", 1, "4,20", 1),
			pos("2026-09-08", "", "3550", "Blutbild und Blutbildbestandteile", 1, "4,02", 1),
			pos("2026-09-08", "", "3560", "Glukose", 1, "2,68", 1),
			pos("2026-09-08", "", "3561", "Glykierte Hämoglobine (HbA1, HbA1c)", 1, "13,41", 1),
			pos("2026-09-08", "", "3585.H1", "Kreatinin", 1, "2,68", 1),
			pos("2026-09-15", "", "3", "Eingehende, das gewöhnliche Maß übersteigende Beratung – auch mittels Fernsprecher", 1, "30,60", 1),
			pos("2026-09-15", "", "651", "Elektrokardiographische Untersuchung in Ruhe", 1, "26,54", 1),
			pos("2026-09-15", "", "410", "Ultraschalluntersuchung eines Organs", 1, "20,98", 1),
			pos("2026-09-15", "", "420", "Ultraschalluntersuchung von bis zu drei weiteren Organen im Anschluss an Nr. 410, je Organ", 2, "16,78", 1),
			pos("2026-09-22", "", "1", "Beratung – auch mittels Fernsprecher", 1, "10,72", 2),
			pos("2026-09-22", "", "5", "Symptombezogene Untersuchung", 1, "10,72", 2),
			pos("2026-09-22", "", "34", "Erörterung (Dauer mindestens 20 Minuten) der Auswirkungen einer Krankheit auf die Lebensgestaltung", 1, "40,22", 2),
			pos("2026-09-29", "", "1", "Beratung – auch mittels Fernsprecher", 1, "10,72", 2),
			pos("2026-09-29", "", "75", "Ausführlicher schriftlicher Krankheits- und Befundbericht", 1, "17,43", 2),
		}}
}

// gozBill: a dental invoice with tooth numbers and an attached lab invoice (3 pages).
func gozBill() *Bill {
	return &Bill{Source: "rechnung-goz.pdf", Provider: "Ruth Beispiel", Reference: "ZA-2026-0412",
		IBAN: "DE75250100309182736450", Date: d("2026-11-03"),
		Total: eur("931,59"), Fee: eur("435,86"), Expenses: eur("495,73"),
		Positions: []Position{
			pos("2026-10-06", "36", "0090", "Intraorale Infiltrationsanästhesie", 1, "7,76", 1),
			pos("2026-10-06", "36", "2210", "Versorgung eines Zahnes durch eine Vollkrone (Hohlkehl- oder Stufenpräparation)", 1, "217,06", 1),
			pos("2026-10-06", "36", "2270", "Provisorium im direkten Verfahren mit Abformung, je Zahn", 1, "34,93", 1),
			pos("2026-10-06", "OK/UK", "0050", "Abformung oder Teilabformung eines Kiefers für ein Situationsmodell", 2, "31,04", 1),
			pos("2026-10-20", "36", "0090", "Intraorale Infiltrationsanästhesie", 1, "7,76", 1),
			pos("2026-10-20", "36", "2197", "Adhäsive Befestigung (plastischer Aufbau, Stift, Inlay, Krone, Teilkrone, Veneer)", 1, "16,82", 1),
			pos("2026-10-20", "36", "2030", "Besondere Maßnahmen beim Präparieren oder Füllen von Kavitäten", 1, "8,41", 2),
			pos("2026-10-27", "", "Ä1", "Beratung – auch mittels Fernsprecher", 1, "10,72", 2),
			pos("2026-10-27", "", "1040", "Professionelle Zahnreinigung, je Zahn", 28, "101,36", 2),
		}}
}

// physioBill: the physiotherapy invoice – one session of Krankengymnastik
// more than prescribed.
func physioBill() *Bill {
	b := &Bill{Source: "rechnung-physio.png", Provider: "Lotte Probe", Reference: "PT-2026-091",
		IBAN: "DE18370501981234509876", Date: d("2026-10-23"), Total: eur("311,00")}
	for _, day := range []string{"2026-09-22", "2026-09-25", "2026-09-29", "2026-10-02", "2026-10-06", "2026-10-09", "2026-10-13"} {
		b.Positions = append(b.Positions, pos(day, "", "KG", "Krankengymnastik, Einzelbehandlung", 1, "32,00", 1))
		if day != "2026-10-13" {
			b.Positions = append(b.Positions, pos(day, "", "WP", "Wärmepackung (Fango)", 1, "14,50", 1))
		}
	}
	return b
}

func line(date, provider, billed, eligible string, rate float64, refund string, notes ...string) NoticeLine {
	return NoticeLine{Date: d(date), Provider: provider, Billed: eur(billed), Eligible: eur(eligible),
		Rate: money.Percent(rate), Refund: eur(refund), Notes: notes, Page: 1}
}

// beihilfeNotice: the Beihilfebescheid – note numbers in the lines, the texts on page 2.
func beihilfeNotice() *Notice {
	return &Notice{Source: "bescheid-beihilfe.pdf", PayerID: "bh", Payer: "Beihilfestelle Musterland",
		Date: d("2026-11-18"), ApplicationDate: d("2026-11-04"), PaidOut: eur("516,70"),
		Deductions: []model.Deduction{{Label: "Kostendämpfungspauschale 2026", Amount: eur("100,00")}},
		Lines: []NoticeLine{
			line("2026-10-12", "Dr. med. Hans Probe", "243,87", "233,38", 50, "116,69", "1"),
			line("2026-10-21", "Rosen-Apotheke", "23,92", "18,92", 50, "9,46", "2"),
			line("2026-10-23", "Physiotherapie Lotte Probe", "311,00", "247,80", 50, "123,90", "3", "4"),
			line("2026-11-03", "Zahnarztpraxis Dr. Ruth Beispiel", "931,59", "733,30", 50, "366,65", "5"),
		},
		Notes: []Note{
			{"1", "Der Gebührensatz über dem 2,3-fachen (Nr. 3 GOÄ am 15.09.2026) ist nicht ausreichend begründet. Beihilfefähig ist der 2,3-fache Satz; nicht beihilfefähig: 10,49 €."},
			{"2", "Nicht verschreibungspflichtige Arzneimittel sind nicht beihilfefähig (§ 22 BVO); nicht beihilfefähig: 5,00 €."},
			{"3", "Aufwendungen für Heilmittel sind nur bis zu den Höchstbeträgen der Anlage 9 zur BVO beihilfefähig (Krankengymnastik 28,20 €, Wärmepackung 13,10 € je Behandlung)."},
			{"4", "Verordnet waren 6 Behandlungen Krankengymnastik; die 7. Behandlung vom 13.10.2026 ist nicht beihilfefähig."},
			{"5", "Aufwendungen für zahntechnische Leistungen (Material- und Laborkosten) sind zu 60 % beihilfefähig (§ 16 BVO)."},
		}}
}

// pkvNotice: the PKV statement with a deductible.
func pkvNotice() *Notice {
	return &Notice{Source: "abrechnung-pkv.png", PayerID: "pkv", Payer: "Muster Krankenversicherung",
		Date: d("2026-11-20"), ApplicationDate: d("2026-11-04"), PaidOut: eur("670,70"),
		Deductions: []model.Deduction{{Label: "Selbstbehalt", Amount: eur("75,00")}},
		Lines: []NoticeLine{
			line("2026-10-12", "Dr. med. Hans Probe", "243,87", "243,87", 50, "121,94"),
			line("2026-10-21", "Rosen-Apotheke", "23,92", "18,92", 50, "9,46", "A3"),
			line("2026-10-23", "Physiotherapie L. Probe", "311,00", "297,00", 50, "148,50", "H1"),
			line("2026-11-03", "Zahnarztpraxis Dr. Beispiel", "931,59", "931,59", 50, "465,80"),
		},
		Notes: []Note{
			{"A3", "Nicht verschreibungspflichtige Arzneimittel sind im Tarif BT50 nicht versichert."},
			{"H1", "Heilmittel erstatten wir bis zu den Höchstbeträgen des Tarifs (Krankengymnastik 30,00 € je Behandlung)."},
		}}
}

// costPlan: the Heil- und Kostenplan for the crown of the GOZ invoice.
func costPlan() *Plan {
	return &Plan{Source: "kostenplan-zahn.png", Provider: "Ruth Beispiel", Date: d("2026-09-15"),
		Fee: eur("336,35"), Expenses: eur("450,00"), Total: eur("786,35"),
		Positions: []Position{
			pos("", "36", "Ä925a", "Röntgendiagnostik der Zähne, bis zwei Einzelaufnahmen", 1, "20,98", 1),
			pos("", "36", "0090", "Intraorale Infiltrationsanästhesie", 2, "15,52", 1),
			pos("", "36", "2210", "Versorgung eines Zahnes durch eine Vollkrone (Hohlkehl- oder Stufenpräparation)", 1, "217,06", 1),
			pos("", "36", "2270", "Provisorium im direkten Verfahren mit Abformung, je Zahn", 1, "34,93", 1),
			pos("", "OK/UK", "0050", "Abformung oder Teilabformung eines Kiefers für ein Situationsmodell", 2, "31,04", 1),
			pos("", "36", "2197", "Adhäsive Befestigung (plastischer Aufbau, Stift, Inlay, Krone, Teilkrone, Veneer)", 1, "16,82", 1),
		}}
}

// prescription: 6 × Krankengymnastik and 6 × Fango.
func prescription() *Prescription {
	return &Prescription{Source: "verordnung-physio.png", Prescriber: "Klara Beispiel",
		Date: d("2026-09-10"), ValidUntil: d("2026-10-08"),
		Remedies: []Remedy{{Name: "Krankengymnastik", Count: 6}, {Name: "Fango", Count: 6}}}
}
