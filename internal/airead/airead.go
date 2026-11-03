// Package airead reads an invoice with the local AI server – an alternative
// to the text recognition where it fails (skewed phone photos, dark
// pictures, long tables). The model reads; this package checks what it read
// and merges it with the text recognition by an order of trust:
//
//	GiroCode > AI confirmed by the recognised text > text recognition (label) > AI alone > text recognition (guess)
//
// The losing value of a field is kept as a hint for the user, never dropped
// silently.
package airead

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/ai"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/extract"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/match"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// MaxPages is the most pages sent in one request (tested with four; more
// would not fit the server's context).
const MaxPages = 4

// PageTimeout is the time allowed per page (measured: about a minute).
const PageTimeout = 300 * time.Second

// Input is a document to read.
type Input struct {
	Pages []string       // the pages as data URLs (JPEG), in order
	Lines []extract.Line // the recognised text of the same pages (for the checks)
	QR    []string       // GiroCode payloads found
	Known []string       // providers of earlier invoices
	Now   time.Time
}

// Other is the value of a field that lost against the one taken.
type Other struct {
	Source string // where it came from: qr, label, guess (text recognition), ai, ai-confirmed
	Value  string // as the DTOs carry it: "2026-03-14", "286.40", text
}

// Result is the merged reading.
type Result struct {
	AIKind    string        // what the AI takes the document for: invoice, refund, prescription, other
	Draft     extract.Draft // text recognition and AI merged; Sources tell which won
	Others    map[string]Other
	FromAI    []string // fields whose value came from the AI (alone or confirmed)
	Confirmed int      // fields where the AI and the text agree
	Pages     int      // pages sent
	Took      time.Duration
}

// Read sends the pages (at most MaxPages) to the AI and merges its answer
// with the text recognition.
func Read(ctx context.Context, c *ai.Client, in Input) (Result, error) {
	if len(in.Pages) == 0 {
		return Result{}, errors.New("keine Seiten zum Auslesen")
	}
	pages := in.Pages[:min(len(in.Pages), MaxPages)]
	if err := c.Health(ctx); err != nil {
		return Result{}, err
	}
	ans, err := c.Chat(ctx, ai.Request{Name: "beleg", Schema: schema, Messages: Messages(pages),
		MaxTokens: 800, Timeout: PageTimeout * time.Duration(len(pages))})
	if err != nil {
		return Result{}, err
	}
	var a Answer
	if err := json.Unmarshal(ans.JSON, &a); err != nil {
		return Result{}, fmt.Errorf("%w (%w)", ai.ErrAnswer, err)
	}
	r := Merge(a, in)
	r.Pages, r.Took = len(pages), ans.Took
	if len(in.Pages) > len(pages) {
		r.Draft.Notes = append(r.Draft.Notes, fmt.Sprintf("Die KI hat nur die ersten %d von %d Seiten gelesen.", len(pages), len(in.Pages)))
	}
	return r, nil
}

// Answer is the model's answer (the fields of the schema).
type Answer struct {
	Art                string `json:"art"`
	Behandler          string `json:"behandler"`
	Zahlungsempfaenger string `json:"zahlungsempfaenger"`
	IBAN               string `json:"iban"`
	Rechnungsnummer    string `json:"rechnungsnummer"`
	Kategorie          string `json:"kategorie"`
	Rechnungsdatum     string `json:"rechnungsdatum"`
	BehandlungVon      string `json:"behandlung_von"`
	BehandlungBis      string `json:"behandlung_bis"`
	Betrag             string `json:"betrag"`
	FaelligAm          string `json:"faellig_am"`
}

// The fields of the answer, in the order the model writes them, with what
// the prompt says about them. The wording was tested with
// scripts/ki-belegtest.py.
var fields = []struct{ key, text string }{
	{"art", "Art des Belegs: rechnung (auch Quittung, Kassenbon, Kostenvoranschlag), bescheid (Erstattung der " +
		"Beihilfe oder Krankenversicherung), rezept (Verordnung), sonstiges"},
	{"behandler", "Name der Praxis, des Arztes, der Apotheke oder des Rechnungsstellers"},
	{"zahlungsempfaenger", "Empfänger der Zahlung, falls nicht der Behandler (z. B. Abrechnungsstelle)"},
	{"iban", "IBAN für die Überweisung"},
	{"rechnungsnummer", "Rechnungsnummer"},
	{"kategorie", "Art der Leistung: arzt (Arztpraxis, Facharzt, Labor, Radiologie, MVZ – Abrechnung nach GOÄ), " +
		"zahnarzt (GOZ), apotheke (Medikamente, Kassenbon einer Apotheke), heilmittel (Physiotherapie, Ergotherapie, " +
		"Logopädie), hilfsmittel (Brille, Hörgerät, Sanitätshaus), krankenhaus, heilpraktiker (GebüH), sonstiges"},
	{"rechnungsdatum", "Datum der Rechnung (auf Quittungen und Kassenbons: das Kaufdatum)"},
	{"behandlung_von", "erster Behandlungstag – aus „Behandlungszeitraum“/„Leistungszeitraum“, sonst das früheste Datum der Positionen"},
	{"behandlung_bis", "letzter Behandlungstag – aus „Behandlungszeitraum“/„Leistungszeitraum“, sonst das späteste Datum der Positionen"},
	{"betrag", "Gesamtbetrag der Rechnung in Euro – der Endbetrag („Rechnungsbetrag“, „Gesamtbetrag“, auf Kassenbons " +
		"„Summe“), auch wenn schon bezahlt; nicht ein Übertrag oder eine Zwischensumme"},
	{"faellig_am", "Datum, bis zu dem zu zahlen ist (nur wenn als Datum angegeben)"},
}

var (
	kinds      = []string{"rechnung", "bescheid", "rezept", "sonstiges"}
	categories = func() []string {
		out := []string{""} // none
		for _, c := range model.Categories {
			out = append(out, string(c))
		}
		return out
	}()
)

const system = "Du liest Belege der deutschen privaten Krankenversicherung und Beihilfe aus: Arztrechnungen, " +
	"Zahnarztrechnungen, Rezepte, Rechnungen von Apotheken und Therapeuten. Antworte nur mit JSON."

// prompt asks for the fields (built once from fields).
var prompt = func() string {
	var b strings.Builder
	b.WriteString("Lies den Beleg (alle Seiten zusammen) und gib diese Felder als JSON-Objekt zurück:\n")
	for _, f := range fields {
		fmt.Fprintf(&b, "- %s: %s\n", f.key, f.text)
	}
	b.WriteString("\nRegeln: Datumsangaben als JJJJ-MM-TT. Betrag mit Punkt als Dezimaltrennzeichen, ohne Währung " +
		"(z. B. 1234.56). IBAN ohne Leerzeichen. Steht ein Feld nicht auf dem Beleg, gib eine leere " +
		"Zeichenkette zurück – nichts schätzen oder erfinden.")
	return b.String()
}()

// schema is the JSON schema of the answer with the properties in the order
// of fields (llama.cpp writes them in this order).
var schema = func() json.RawMessage {
	var b strings.Builder
	b.WriteString(`{"type":"object","properties":{`)
	for i, f := range fields {
		prop := map[string]any{"type": "string"}
		switch f.key {
		case "art":
			prop["enum"] = kinds
		case "kategorie":
			prop["enum"] = categories
		}
		p, _ := json.Marshal(prop)
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%q:%s", f.key, p)
	}
	keys := make([]string, len(fields))
	for i, f := range fields {
		keys[i] = f.key
	}
	req, _ := json.Marshal(keys)
	fmt.Fprintf(&b, `},"required":%s,"additionalProperties":false}`, req)
	return json.RawMessage(b.String())
}()

// Messages is the request for these pages: the instructions, then the pages as images.
func Messages(pages []string) []ai.Message {
	parts := []ai.Part{ai.TextPart(prompt)}
	for _, p := range pages {
		parts = append(parts, ai.ImagePart(p))
	}
	return []ai.Message{{Role: "system", Content: system}, {Role: "user", Content: parts}}
}

// The order of trust (higher wins).
var trust = map[string]int{extract.SrcQR: 5, extract.SrcAIConfirmed: 4, extract.SrcLabel: 3, extract.SrcAI: 2, extract.SrcGuess: 1}

// Merge checks the AI's answer and merges it with the text recognition of in.
func Merge(a Answer, in Input) Result {
	ocr := extract.Analyze(in.Lines, in.QR, in.Now)
	mine, notes := check(a, ocr, extract.NewEvidence(in.Lines), in)
	r := Result{AIKind: kindOf(a.Art), Draft: ocr, Others: map[string]Other{}}
	r.Draft.Sources = map[string]string{}
	r.Draft.Notes = append(r.Draft.Notes, notes...)
	theirs := valuesOf(ocr)
	for _, f := range fieldNames {
		o, m := theirs[f], mine[f]
		if o.value != "" && o.source == "" {
			o.source = ocrSource(ocr, f)
		}
		switch {
		case m.value == "":
			r.Draft.Sources[f] = o.source
			continue
		case o.value == "":
			r.take(f, m)
		case same(f, o.value, m.value):
			// Read twice the same way: confirmed (a GiroCode stays the GiroCode),
			// in the words of the more trusted reading.
			v := o.value
			if trust[m.source] > trust[o.source] {
				v = m.value
			}
			if o.source != extract.SrcQR {
				o.source = extract.SrcAIConfirmed
			}
			r.take(f, field{v, o.source})
			r.Confirmed++
			continue
		case trust[m.source] > trust[o.source]:
			r.take(f, m)
			r.Others[f] = o.other()
		default:
			r.Draft.Sources[f] = o.source
			r.Others[f] = m.other()
			continue
		}
		if m.source == extract.SrcAIConfirmed {
			r.Confirmed++
		}
	}
	r.period()
	r.notes()
	for f, s := range r.Draft.Sources {
		if s == "" {
			delete(r.Draft.Sources, f)
		}
	}
	if r.Draft.Kind == extract.KindUnknown && (r.AIKind == "invoice" || r.AIKind == "prescription") {
		r.Draft.Kind = extract.KindInvoice
	}
	if r.AIKind == "refund" && r.Draft.Kind != extract.KindRefundDoc {
		r.Draft.Notes = append(r.Draft.Notes, "Die KI hält das für einen Erstattungsbescheid – dann bitte als Erstattungsbeleg behandeln.")
	}
	return r
}

// period keeps the treatment period in order: start and end may come from
// different readings, and an end before the start cannot be saved. The
// less trusted of the two then gives way (and shows as the other reading).
func (r *Result) period() {
	d := &r.Draft
	if d.ServiceFrom.IsZero() || d.ServiceTo.IsZero() || !d.ServiceTo.Before(d.ServiceFrom) {
		return
	}
	loser := "serviceTo"
	if trust[d.Sources["serviceFrom"]] < trust[d.Sources["serviceTo"]] {
		loser = "serviceFrom"
	}
	r.Others[loser] = Other{Source: d.Sources[loser], Value: valuesOf(*d)[loser].value}
	set(d, loser, "")
	delete(d.Sources, loser)
	r.FromAI = slices.DeleteFunc(r.FromAI, func(f string) bool { return f == loser })
}

// notes drops the text recognition's notes about values that were replaced
// and checks the merged dates again.
func (r *Result) notes() {
	d := &r.Draft
	d.Notes = slices.DeleteFunc(d.Notes, func(n string) bool {
		return n == extract.NoteAmountGuessed && d.Sources["amount"] != extract.SrcGuess || n == extract.NoteServiceAfterInvoice
	})
	if !d.ServiceFrom.IsZero() && !d.InvoiceDate.IsZero() && d.ServiceFrom.After(d.InvoiceDate) {
		d.Notes = append(d.Notes, extract.NoteServiceAfterInvoice)
	}
}

// take sets field f of the draft to v.
func (r *Result) take(f string, v field) {
	set(&r.Draft, f, v.value)
	r.Draft.Sources[f] = v.source
	if v.source == extract.SrcAI || v.source == extract.SrcAIConfirmed {
		r.FromAI = append(r.FromAI, f)
	}
}

func kindOf(art string) string {
	switch art {
	case "rechnung":
		return "invoice"
	case "bescheid":
		return "refund"
	case "rezept":
		return "prescription"
	}
	return "other"
}

// ocrSource is the source of a value the text recognition found without
// naming one: the end of the treatment comes with its start, the rest
// counts as found next to a label.
func ocrSource(d extract.Draft, f string) string {
	if f == "serviceTo" && d.Sources["serviceFrom"] != "" {
		return d.Sources["serviceFrom"]
	}
	return extract.SrcLabel
}

// same compares two values of a field: names as names, numbers without
// case and punctuation – and an invoice number the AI read in part (the
// text recognition's "2026-0815", the AI's "0815") as the same, so the
// complete one stays.
func same(f, ocr, ai string) bool {
	switch f {
	case "amount", "invoiceDate", "serviceFrom", "serviceTo", "due", "category":
		return ocr == ai
	case "provider", "payee":
		return sameName(ocr, ai)
	case "reference":
		o, a := match.Fold(ocr), match.Fold(ai)
		o, a = strings.Map(alnum, o), strings.Map(alnum, a)
		return o == a || len(a) >= 4 && strings.Contains(o, a)
	}
	return extract.SameText(ocr, ai)
}

func alnum(r rune) rune {
	if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
		return r
	}
	return -1
}

// field is a value with its source.
type field struct{ value, source string }

func (f field) other() Other { return Other{Source: f.source, Value: f.value} }

// The fields merged, as named in Draft.Sources.
var fieldNames = []string{"provider", "payee", "iban", "reference", "category", "invoiceDate", "serviceFrom", "serviceTo", "amount", "due"}

func valuesOf(d extract.Draft) map[string]field {
	date := func(c civil.Date) string {
		if c.IsZero() {
			return ""
		}
		return c.String()
	}
	amount := ""
	if d.Amount > 0 {
		amount = d.Amount.String()
	}
	v := map[string]string{"provider": d.Provider, "payee": d.Payee, "iban": d.IBAN, "reference": d.Reference,
		"category": string(d.Category), "invoiceDate": date(d.InvoiceDate), "serviceFrom": date(d.ServiceFrom),
		"serviceTo": date(d.ServiceTo), "amount": amount, "due": date(d.Due)}
	out := map[string]field{}
	for k, x := range v {
		out[k] = field{x, d.Sources[k]}
	}
	return out
}

// set puts a checked value (see check) into the draft.
func set(d *extract.Draft, f, v string) {
	date := func() civil.Date { c, _ := civil.Parse(v); return c }
	switch f {
	case "provider":
		d.Provider = v
	case "payee":
		d.Payee = v
	case "iban":
		d.IBAN = v
	case "reference":
		d.Reference = v
	case "category":
		d.Category = model.Category(v)
	case "invoiceDate":
		d.InvoiceDate = date()
	case "serviceFrom":
		d.ServiceFrom = date()
	case "serviceTo":
		d.ServiceTo = date()
	case "amount":
		c, _ := money.ParseDecimal(v)
		d.Amount = c
	case "due":
		d.Due = date()
	}
}
