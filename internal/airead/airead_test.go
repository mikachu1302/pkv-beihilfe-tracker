package airead

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/ai"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/ai/aitest"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/extract"
)

var now = time.Date(2026, 11, 3, 10, 0, 0, 0, time.Local)

// lines turns text into recognised lines of page 1, top to bottom.
func lines(text string) []extract.Line {
	var out []extract.Line
	for i, t := range strings.Split(strings.TrimSpace(text), "\n") {
		out = append(out, extract.Line{Text: strings.TrimSpace(t), Page: 1, X: 0.1, Y: 0.05 + float64(i)*0.04, W: 0.6, H: 0.02})
	}
	return out
}

// A made-up invoice as the text recognition reads it.
const invoiceText = `
Dr. med. Anna Beispiel
Fachärztin für Allgemeinmedizin
Musterstraße 1, 12345 Musterstadt
Rechnungsdatum: 14.03.2026
Rechnungsnummer: 2026-0815
Behandlungszeitraum: 03.03.2026 - 10.03.2026
03.03.2026 Beratung GOÄ 1 2,3 10,72
10.03.2026 Ganzkörperstatus GOÄ 8 2,3 245,00
Rechnungsbetrag 286,40 €
Bitte überweisen Sie auf DE89 3704 0044 0532 0130 00`

// full is an answer with every field.
func full() Answer {
	return Answer{Art: "rechnung", Behandler: "Dr. med. Anna Beispiel", IBAN: "DE89370400440532013000",
		Rechnungsnummer: "2026-0815", Kategorie: "arzt", Rechnungsdatum: "2026-03-14", BehandlungVon: "2026-03-03",
		BehandlungBis: "2026-03-10", Betrag: "286.40"}
}

func TestMergeAgreeing(t *testing.T) {
	r := Merge(full(), Input{Lines: lines(invoiceText), Now: now})
	d := r.Draft
	if d.Provider != "Dr. med. Anna Beispiel" || d.Amount.String() != "286.40" || d.IBAN != "DE89370400440532013000" ||
		d.Reference != "2026-0815" || d.InvoiceDate.String() != "2026-03-14" || d.ServiceTo.String() != "2026-03-10" {
		t.Fatalf("draft %+v", d)
	}
	// Read the same way twice: confirmed.
	for _, f := range []string{"amount", "iban", "reference", "invoiceDate", "serviceFrom", "serviceTo"} {
		if d.Sources[f] != extract.SrcAIConfirmed {
			t.Errorf("%s: source %q", f, d.Sources[f])
		}
	}
	if len(r.Others) != 0 || r.Confirmed < 6 {
		t.Errorf("others %v, confirmed %d", r.Others, r.Confirmed)
	}
}

func TestMergeDarkPhoto(t *testing.T) {
	// The text recognition read nothing: everything from the AI, nothing confirmed by the text.
	r := Merge(full(), Input{Now: now, Known: []string{"Dr. med. Anna  Beispiel"}})
	d := r.Draft
	if d.Kind != extract.KindInvoice || d.Amount.String() != "286.40" || d.Category != "arzt" || len(r.FromAI) != 8 {
		t.Fatalf("draft %+v, from AI %v", d, r.FromAI)
	}
	if d.Sources["amount"] != extract.SrcAI || d.Sources["invoiceDate"] != extract.SrcAI {
		t.Errorf("sources %v", d.Sources)
	}
	// A provider of an earlier invoice.
	if d.Sources["provider"] != extract.SrcAIConfirmed || r.Confirmed != 1 {
		t.Errorf("provider %q, confirmed %d", d.Sources["provider"], r.Confirmed)
	}
}

func TestMergeConflicts(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		qr     []string
		change func(*Answer)
		field  string
		want   string // value taken
		source string // its source
		other  Other  // the losing value
	}{
		{"AI amount in the text beats the label", invoiceText, nil, func(a *Answer) { a.Betrag = "245.00" },
			"amount", "245.00", extract.SrcAIConfirmed, Other{extract.SrcLabel, "286.40"}},
		{"AI amount not in the text loses", invoiceText, nil, func(a *Answer) { a.Betrag = "268.40" },
			"amount", "286.40", extract.SrcLabel, Other{extract.SrcAI, "268.40"}},
		{"GiroCode wins", invoiceText, []string{"BCD\n002\n1\nSCT\n\nPraxis Beispiel\nDE89370400440532013000\nEUR286.40\n\n\nRE 2026-0815"},
			func(a *Answer) { a.Betrag = "245.00" }, "amount", "286.40", extract.SrcQR, Other{extract.SrcAIConfirmed, "245.00"}},
		{"a date in the text is no confirmation", invoiceText, nil, func(a *Answer) { a.BehandlungBis = "2026-03-03" },
			"serviceTo", "2026-03-10", extract.SrcLabel, Other{extract.SrcAI, "2026-03-03"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := full()
			tc.change(&a)
			r := Merge(a, Input{Lines: lines(tc.text), QR: tc.qr, Now: now})
			got := valuesOf(r.Draft)[tc.field]
			if got.value != tc.want || got.source != tc.source || r.Others[tc.field] != tc.other {
				t.Errorf("%s = %+v, other %+v", tc.field, got, r.Others[tc.field])
			}
		})
	}
}

func TestMergeBeatsGuesses(t *testing.T) {
	// A receipt the text recognition only guessed at.
	text := `Stadt-Apotheke
Kassenbon
Ibuprofen 400 7,49
Nasenspray 4,98
Summe EUR 12,47`
	ocr := extract.Analyze(lines(text), nil, now)
	a := Answer{Art: "rechnung", Behandler: "Stadt-Apotheke am Markt", Kategorie: "apotheke", Rechnungsdatum: "2026-10-28", Betrag: "12.47"}
	r := Merge(a, Input{Lines: lines(text), Now: now})
	if ocr.Sources["provider"] != extract.SrcGuess {
		t.Fatalf("test needs a guessed provider: %v", ocr.Sources)
	}
	if r.Draft.Provider != "Stadt-Apotheke am Markt" || r.Others["provider"].Value != ocr.Provider {
		t.Errorf("provider %q, other %+v", r.Draft.Provider, r.Others["provider"])
	}
	if r.Draft.InvoiceDate.String() != "2026-10-28" || r.Draft.Sources["invoiceDate"] != extract.SrcAI {
		t.Errorf("date %v %q", r.Draft.InvoiceDate, r.Draft.Sources["invoiceDate"])
	}
}

func TestChecks(t *testing.T) {
	a := full()
	a.IBAN = "DE89 3704 0044 0532 0130 01" // wrong check digits
	a.BehandlungVon = "2026-06-01"         // months after the invoice
	a.BehandlungBis = "2026-06-02"
	a.FaelligAm = "14.04.2026" // German format is fine
	a.Betrag = "zweihundert"
	a.Kategorie = "zauberei"
	a.Rechnungsnummer = strings.Repeat("x", 200)
	r := Merge(a, Input{Now: now})
	d := r.Draft
	if d.IBAN != "" || !d.ServiceFrom.IsZero() || !d.ServiceTo.IsZero() || d.Amount != 0 || d.Category != "" || d.Reference != "" {
		t.Errorf("implausible values taken: %+v", d)
	}
	if d.Due.String() != "2026-04-14" {
		t.Errorf("due %v", d.Due)
	}
	notes := strings.Join(d.Notes, "\n")
	for _, want := range []string{"IBAN der KI (DE89370400440532013001)", "Behandlung von 01.06.2026", "Behandlung bis 02.06.2026", "„zweihundert“"} {
		if !strings.Contains(notes, want) {
			t.Errorf("note %q missing in\n%s", want, notes)
		}
	}

	// Far away dates, treatment end before its start, a payee that is the provider.
	a = full()
	a.Rechnungsdatum = "1996-03-14"
	a.BehandlungVon, a.BehandlungBis = "2026-03-10", "2026-03-03"
	a.Zahlungsempfaenger = "DR. MED. ANNA BEISPIEL"
	d = Merge(a, Input{Now: now}).Draft
	if !d.InvoiceDate.IsZero() || !d.ServiceTo.IsZero() || d.ServiceFrom.IsZero() || d.Payee != "" {
		t.Errorf("draft %+v", d)
	}
}

// The model copies dates and amounts as printed (asked to convert them, it
// turned 20.11.2026 into 2022-11-20 and left amounts empty).
func TestValuesAsPrinted(t *testing.T) {
	a := full()
	a.Rechnungsdatum, a.BehandlungVon, a.BehandlungBis, a.FaelligAm = "14.03.2026", "3.3.26", "10. März 2026", "14. Apr. 2026"
	a.Betrag = "1.286,40 €"
	d := Merge(a, Input{Now: now}).Draft
	if d.InvoiceDate.String() != "2026-03-14" || d.ServiceFrom.String() != "2026-03-03" ||
		d.ServiceTo.String() != "2026-03-10" || d.Due.String() != "2026-04-14" || d.Amount != 128640 || len(d.Notes) != 0 {
		t.Errorf("draft %+v", d)
	}
	for _, s := range []string{"286,40", "286,40 €", "EUR 286,40", "286.40"} {
		a.Betrag = s
		if d := Merge(a, Input{Now: now}).Draft; d.Amount != 28640 {
			t.Errorf("Betrag %q: %v", s, d.Amount)
		}
	}

	for s, want := range map[string]string{
		"2026-11-20": "2026-11-20", "20.11.2026": "2026-11-20", "20.11.26": "2026-11-20",
		"20. November 2026": "2026-11-20", "20 November 2026": "2026-11-20", "20. Nov. 2026": "2026-11-20",
		"1. Maerz 2026": "2026-03-01", "1. Mrz. 2026": "2026-03-01", "5. Sept. 2026": "2026-09-05",
		"30. Juni 2026": "2026-06-30", "30. Juli 2026": "2026-07-30",
	} {
		if d, err := readDate(s); err != nil || d.String() != want {
			t.Errorf("readDate(%q) = %v, %v; want %s", s, d, err, want)
		}
	}
	// Not dates: no month, a word that only starts like one, too short to
	// tell (Juni/Juli), a day the month does not have, a year missing.
	for _, s := range []string{"20. Novitäten 2026", "20. Ju 2026", "31. Februar 2026", "20. November",
		"November 2026", "20.11.", "Datum: 20.11.2026"} {
		if d, err := readDate(s); err == nil {
			t.Errorf("readDate(%q) = %v, want an error", s, d)
		}
	}
}

func TestRefundNote(t *testing.T) {
	r := Merge(Answer{Art: "bescheid", Betrag: "120.00"}, Input{Now: now})
	if r.AIKind != "refund" || !strings.Contains(strings.Join(r.Draft.Notes, " "), "Erstattungsbescheid") {
		t.Errorf("%+v", r)
	}
}

func TestSchemaAndPrompt(t *testing.T) {
	var s struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Properties) != len(fields) || len(s.Required) != len(fields) || s.Required[0] != "art" ||
		len(s.Properties["kategorie"].Enum) != 9 || s.Properties["kategorie"].Enum[0] != "" {
		t.Errorf("schema %s", schema)
	}
	// The order of the properties is the order of fields.
	if i, j := strings.Index(string(schema), `"art"`), strings.Index(string(schema), `"faellig_am"`); i > j {
		t.Errorf("order %s", schema)
	}
	m := Messages([]string{"data:image/jpeg;base64,AA", "data:image/jpeg;base64,BB"})
	if len(m) != 2 || len(m[1].Content.([]ai.Part)) != 3 || !strings.Contains(prompt, "- betrag: Gesamtbetrag") ||
		!strings.Contains(prompt, "so, wie sie gedruckt sind") || strings.Contains(prompt, "JJJJ") {
		t.Errorf("messages %+v", m)
	}
}

func client(t *testing.T, fake http.Handler) *ai.Client {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return &ai.Client{URL: srv.URL + "/v1", Model: "qwen3-vl-8b", Key: "key", Lock: ai.NewLock()}
}

func TestRead(t *testing.T) {
	answer, _ := json.Marshal(full())
	fake := &aitest.Server{Key: "key", Answer: func(aitest.Request) string { return string(answer) }}
	c := client(t, fake)
	pages := []string{"data:image/jpeg;base64,AA", "data:image/jpeg;base64,BB", "data:image/jpeg;base64,CC",
		"data:image/jpeg;base64,DD", "data:image/jpeg;base64,EE"}
	r, err := Read(t.Context(), c, Input{Pages: pages, Lines: lines(invoiceText), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if r.Pages != MaxPages || r.Draft.Amount.String() != "286.40" || !strings.Contains(strings.Join(r.Draft.Notes, " "), "ersten 4 von 5 Seiten") {
		t.Errorf("result %+v", r)
	}
	req := fake.Requests()[0]
	if len(req.Messages) != 2 || len(req.Messages[1].Images) != MaxPages || req.Messages[1].Images[0] != pages[0] ||
		!strings.Contains(req.Text("system"), "Antworte nur mit JSON") {
		t.Errorf("request %+v", req)
	}

	if _, err := Read(t.Context(), c, Input{Now: now}); err == nil {
		t.Error("no pages accepted")
	}
}

func TestReadErrors(t *testing.T) {
	ok := Input{Pages: []string{"data:image/jpeg;base64,AA"}, Now: now}

	// Key refused.
	c := client(t, &aitest.Server{Key: "anderer"})
	if _, err := Read(t.Context(), c, ok); !errors.Is(err, ai.ErrKey) {
		t.Errorf("key: %v", err)
	}
	// Not JSON of the schema (a server without grammar support).
	c = client(t, &aitest.Server{Key: "key", Answer: func(aitest.Request) string { return `{"betrag": 286.40}` }})
	if _, err := Read(t.Context(), c, ok); !errors.Is(err, ai.ErrAnswer) {
		t.Errorf("wrong JSON: %v", err)
	}
	// Not reachable.
	srv := httptest.NewServer(http.NotFoundHandler())
	gone := &ai.Client{URL: srv.URL + "/v1"}
	srv.Close()
	if _, err := Read(t.Context(), gone, ok); !errors.Is(err, ai.ErrUnreachable) {
		t.Errorf("unreachable: %v", err)
	}
	// Cancelled while the model reads.
	started := make(chan struct{})
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			return
		}
		_, _ = io.Copy(io.Discard, r.Body) // only then does the server notice a closed connection
		close(started)
		<-r.Context().Done()
	})
	c = client(t, slow)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-started
		cancel()
	}()
	if _, err := Read(ctx, c, ok); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
}

// Findings of the review of 2026-11-03.
func TestMergeConsistency(t *testing.T) {
	// The period from two readings: an end before the start gives way.
	text := `Praxis Dr. Beispiel
Rechnungsdatum: 14.03.2026
Behandlungsdatum: 05.03.2026
Rechnungsbetrag 120,00 €`
	a := Answer{Art: "rechnung", BehandlungVon: "2026-02-20", BehandlungBis: "2026-03-01"}
	r := Merge(a, Input{Lines: lines(text), Now: now})
	if r.Draft.ServiceFrom.String() != "2026-03-05" || !r.Draft.ServiceTo.IsZero() ||
		r.Others["serviceTo"] != (Other{extract.SrcAI, "2026-03-01"}) || slices.Contains(r.FromAI, "serviceTo") {
		t.Errorf("period %v–%v, others %+v, from AI %v", r.Draft.ServiceFrom, r.Draft.ServiceTo, r.Others, r.FromAI)
	}

	// Treatment dates are checked against the invoice date that is taken (the labelled one).
	a = Answer{Art: "rechnung", Rechnungsdatum: "2025-03-14", BehandlungVon: "2026-03-03", BehandlungBis: "2026-03-10"}
	r = Merge(a, Input{Lines: lines(invoiceText), Now: now})
	if r.Draft.InvoiceDate.String() != "2026-03-14" || r.Draft.ServiceFrom.String() != "2026-03-03" ||
		strings.Contains(strings.Join(r.Draft.Notes, " "), "Behandlung von") {
		t.Errorf("dates %v %v, notes %v", r.Draft.InvoiceDate, r.Draft.ServiceFrom, r.Draft.Notes)
	}

	// A due date weeks ahead is fine (only invoice and treatment cannot lie in the future).
	r = Merge(Answer{Rechnungsdatum: "2026-11-01", FaelligAm: "2026-12-15"}, Input{Now: now})
	if r.Draft.Due.String() != "2026-12-15" {
		t.Errorf("due %v, notes %v", r.Draft.Due, r.Draft.Notes)
	}

	// A part of the invoice number is no confirmation; the complete number stays.
	for _, part := range []string{"0815", "2026"} {
		r = Merge(Answer{Rechnungsnummer: part}, Input{Lines: lines(invoiceText), Now: now})
		if r.Draft.Reference != "2026-0815" {
			t.Errorf("AI %q: reference %q, others %+v", part, r.Draft.Reference, r.Others)
		}
	}

	// The text recognition's note about its guessed amount goes when the AI's amount is taken.
	receipt := `Stadt-Apotheke
Ibuprofen 400 7,49
Nasenspray 4,98`
	r = Merge(Answer{Betrag: "12.47"}, Input{Lines: lines(receipt), Now: now})
	if r.Draft.Amount.String() != "12.47" || slices.Contains(r.Draft.Notes, extract.NoteAmountGuessed) {
		t.Errorf("amount %v, notes %v", r.Draft.Amount, r.Draft.Notes)
	}

	// Names compare as names: the provider of an earlier invoice confirms with or without "Praxis".
	r = Merge(Answer{Behandler: "Praxis Dr. Anna Beispiel"}, Input{Now: now, Known: []string{"Dr. Anna Beispiel"}})
	if r.Draft.Sources["provider"] != extract.SrcAIConfirmed {
		t.Errorf("provider source %q", r.Draft.Sources["provider"])
	}
	// An IBAN needs the form, not only the checksum.
	r = Merge(Answer{IBAN: "12345678901234567890"}, Input{Now: now})
	if r.Draft.IBAN != "" {
		t.Errorf("IBAN %q", r.Draft.IBAN)
	}
}

func TestReadTellsAboutPagesLeftOut(t *testing.T) {
	c := client(t, &aitest.Server{Key: "key", Answer: func(aitest.Request) string { return "{}" }})
	r, err := Read(t.Context(), c, Input{Pages: []string{"data:image/jpeg;base64,AA"}, TotalPages: 6, Now: now})
	if err != nil || !strings.Contains(strings.Join(r.Draft.Notes, " "), "ersten 1 von 6 Seiten") {
		t.Errorf("%v %v", r.Draft.Notes, err)
	}
}
