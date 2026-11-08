package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/ai"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/ai/aitest"
)

// withAI points the service at a fake AI server that wants the key "geheim-schluessel-7f3a" and switches the AI on.
func withAI(t *testing.T, s *Service, fake *aitest.Server) {
	t.Helper()
	fake.Key = "geheim-schluessel-7f3a"
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	if _, err := s.SetAISettings(AISettingsDTO{On: true, URL: srv.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetAIKey(fake.Key); err != nil {
		t.Fatal(err)
	}
}

func TestAISettings(t *testing.T) {
	s := newTestService(t)
	a := s.AISettings()
	if a.On || a.URL != ai.DefaultURL || a.Model != ai.DefaultModel || a.KeySet {
		t.Errorf("defaults %+v", a)
	}

	a, err := s.SetAISettings(AISettingsDTO{On: true, URL: " http://10.0.0.5:8080/ ", Model: "qwen3-vl-4b"})
	if err != nil || !a.On || a.URL != "http://10.0.0.5:8080/v1" || a.Model != "qwen3-vl-4b" {
		t.Errorf("set %+v %v", a, err)
	}
	for _, bad := range []string{"10.0.0.5:8080", "ftp://server/v1", "http://", "http://server/v1?x=1"} {
		if _, err := s.SetAISettings(AISettingsDTO{URL: bad}); err == nil {
			t.Errorf("address %q accepted", bad)
		}
	}

	// The key: stored, never handed out – only its end.
	a, err = s.SetAIKey("  sk-0123456789abcdef7f3a \n")
	if err != nil || !a.KeySet || a.KeyEnd != "7f3a" {
		t.Errorf("key %+v %v", a, err)
	}
	if b, _ := json.Marshal(a); strings.Contains(string(b), "0123456789") {
		t.Errorf("key in the DTO: %s", b)
	}
	if a, _ := s.SetAIKey("kurz"); !a.KeySet || a.KeyEnd != "" {
		t.Errorf("short key shows its end: %+v", a)
	}
	if _, err := s.SetAIKey("zwei teile"); err == nil {
		t.Error("key with a space accepted")
	}

	// Device-local: in the configuration (readable by the user only), not in the data folder.
	cfg := filepath.Join(s.cfgDir, "config.json")
	b, err := os.ReadFile(cfg)
	if err != nil || !strings.Contains(string(b), `"key": "kurz"`) {
		t.Errorf("config %s %v", b, err)
	}
	// Windows has no such file modes (Go reports 0666 there).
	if fi, err := os.Stat(cfg); runtime.GOOS != "windows" && (err != nil || fi.Mode().Perm() != 0o600) {
		t.Errorf("config mode %v %v", fi.Mode(), err)
	}
	again := NewWithConfigDir(s.cfgDir)
	t.Cleanup(func() { _ = again.ServiceShutdown() })
	if a := again.AISettings(); !a.On || a.Model != "qwen3-vl-4b" || !a.KeySet {
		t.Errorf("after restart %+v", a)
	}

	// Back to the defaults; removing the key.
	a, _ = s.SetAISettings(AISettingsDTO{On: false, URL: "", Model: ""})
	if a.URL != ai.DefaultURL || a.Model != ai.DefaultModel {
		t.Errorf("defaults again %+v", a)
	}
	if a, _ := s.SetAIKey(""); a.KeySet {
		t.Error("key not removed")
	}
	if b, _ := os.ReadFile(cfg); strings.Contains(string(b), `"ai"`) {
		t.Errorf("defaults still in the config: %s", b)
	}
}

func TestTestAI(t *testing.T) {
	s := newTestService(t)
	fake := &aitest.Server{}
	withAI(t, s, fake)
	res, err := s.TestAI(t.Context())
	if err != nil || !res.OK || len(res.Checks) != 3 || res.Checks[1].Text != "Schlüssel angenommen" {
		t.Errorf("test %+v %v", res, err)
	}

	// Wrong key: the server is there, the key is refused, the model is not asked for.
	if _, err := s.SetAIKey("falsch-falsch-falsch"); err != nil {
		t.Fatal(err)
	}
	res, _ = s.TestAI(t.Context())
	if res.OK || len(res.Checks) != 2 || !res.Checks[0].OK || !strings.Contains(res.Checks[1].Text, "Schlüssel") {
		t.Errorf("wrong key %+v", res)
	}

	// Another model.
	_, _ = s.SetAIKey(fake.Key)
	fake.Models = []string{"llama-3.2-3b"}
	res, _ = s.TestAI(t.Context())
	if res.OK || !strings.Contains(res.Checks[2].Text, "geeignet: llama-3.2-3b") {
		t.Errorf("model %+v", res)
	}

	// The home server: a model that reads images and one for text only (it
	// searches the web) – the test names only the first, and refuses the second.
	fake.Models = []string{"qwen3-vl-8b", "qwen3.5-4b"}
	fake.Inputs = map[string][]string{"qwen3-vl-8b": {"text", "image"}, "qwen3.5-4b": {"text"}}
	res, _ = s.TestAI(t.Context())
	if !res.OK || res.Checks[2].Text != "Modell „qwen3-vl-8b“ vorhanden, liest Bilder" {
		t.Errorf("image model %+v", res)
	}
	cur := s.AISettings()
	cur.Model = "qwen3.5-4b"
	if _, err := s.SetAISettings(cur); err != nil {
		t.Fatal(err)
	}
	res, _ = s.TestAI(t.Context())
	if res.OK || !strings.Contains(res.Checks[2].Text, "liest keine Bilder") || !strings.HasSuffix(res.Checks[2].Text, "geeignet: qwen3-vl-8b") {
		t.Errorf("text-only model %+v", res)
	}
	cur.Model = "gibtsnicht"
	_, _ = s.SetAISettings(cur)
	if res, _ = s.TestAI(t.Context()); res.OK || strings.Contains(res.Checks[2].Text, "qwen3.5-4b") {
		t.Errorf("missing model names the text-only one: %+v", res)
	}
	cur.Model = ai.DefaultModel
	_, _ = s.SetAISettings(cur)

	// Works while switched off (testing before switching on); a request needs it on.
	cur = s.AISettings()
	cur.On = false
	if _, err := s.SetAISettings(cur); err != nil {
		t.Fatal(err)
	}
	if res, _ := s.TestAI(t.Context()); len(res.Checks) == 0 {
		t.Error("no test while switched off")
	}
	if _, err := s.aiClient(false); !errors.Is(err, ai.ErrNotSet) {
		t.Errorf("client while off: %v", err)
	}

	// Cancelled: no result, the context error.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.TestAI(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}

// The choice in Settings · KI offers only models that read images (or that
// the server says nothing about); it works while the AI is off.
func TestAIModels(t *testing.T) {
	s := newTestService(t)
	fake := &aitest.Server{Models: []string{"qwen3-vl-8b", "qwen3.5-4b", "anderes"},
		Inputs: map[string][]string{"qwen3-vl-8b": {"text", "image"}, "qwen3.5-4b": {"text"}}}
	withAI(t, s, fake)
	cur := s.AISettings()
	cur.On = false
	if _, err := s.SetAISettings(cur); err != nil {
		t.Fatal(err)
	}
	got, err := s.AIModels(t.Context())
	if err != nil || strings.Join(got, ",") != "qwen3-vl-8b,anderes" {
		t.Errorf("models %v %v", got, err)
	}
	fake.Models = []string{"qwen3.5-4b"}
	if got, err := s.AIModels(t.Context()); err != nil || got == nil || len(got) != 0 {
		t.Errorf("only text-only: %#v %v", got, err) // an empty list, not null (the UI falls back to typing)
	}
	_, _ = s.SetAIKey("falsch-falsch-falsch")
	if _, err := s.AIModels(t.Context()); !errors.Is(err, ai.ErrKey) {
		t.Errorf("wrong key: %v", err)
	}
}

func TestTestAIUnreachable(t *testing.T) {
	s := newTestService(t)
	srv := httptest.NewServer(&aitest.Server{})
	url := srv.URL
	srv.Close()
	if _, err := s.SetAISettings(AISettingsDTO{On: true, URL: url}); err != nil {
		t.Fatal(err)
	}
	res, err := s.TestAI(t.Context())
	if err != nil || res.OK || len(res.Checks) != 1 || !strings.Contains(res.Checks[0].Text, "nicht erreichbar") {
		t.Errorf("unreachable %+v %v", res, err)
	}
}

func TestReadWithAI(t *testing.T) {
	s := newTestService(t)
	if _, err := s.SaveInvoice(sampleInvoice(), nil, nil); err != nil { // "Dr. Muster" is known now
		t.Fatal(err)
	}
	page := []string{"data:image/jpeg;base64,/9j/AA=="}
	if _, err := s.ReadWithAI(t.Context(), AIReadInputDTO{Pages: page}); !errors.Is(err, ai.ErrNotSet) {
		t.Errorf("AI off: %v", err)
	}
	fake := &aitest.Server{Answer: func(aitest.Request) string {
		return `{"art":"rechnung","behandler":"Dr. Muster","zahlungsempfaenger":"","iban":"","rechnungsnummer":"R-4711",
			"kategorie":"arzt","rechnungsdatum":"2026-10-20","behandlung_von":"2026-10-06","behandlung_bis":"",
			"betrag":"98.50","faellig_am":""}`
	}}
	withAI(t, s, fake)
	if _, err := s.ReadWithAI(t.Context(), AIReadInputDTO{Pages: []string{"/etc/passwd"}}); err == nil {
		t.Error("a page that is no image accepted")
	}

	// The text recognition read the number and a different total next to its label.
	lines := []LineDTO{{Text: "Praxis Dr. Muster", Page: 1}, {Text: "Rechnung Nr. R-4711", Page: 1}, {Text: "Rechnungsbetrag 89,50 €", Page: 1}}
	r, err := s.ReadWithAI(t.Context(), AIReadInputDTO{Pages: page, Lines: lines})
	if err != nil {
		t.Fatal(err)
	}
	inv := r.Draft.Invoice
	if inv.Provider != "Dr. Muster" || inv.InvoiceDate != "2026-10-20" || inv.Category != "arzt" || inv.Reference != "R-4711" {
		t.Errorf("draft %+v", inv)
	}
	// The AI's total is not in the text: the label wins, the AI's value is the hint.
	if inv.Amount != "89.50" || r.Others["amount"] != (AIOtherDTO{Source: "ai", Value: "98.50"}) {
		t.Errorf("amount %s, others %+v", inv.Amount, r.Others)
	}
	if r.Sources["provider"] != "ai-confirmed" || r.Sources["reference"] != "ai-confirmed" || r.Sources["invoiceDate"] != "ai" {
		t.Errorf("sources %v", r.Sources)
	}
	if r.Kind != "invoice" || r.AIKind != "invoice" || r.Pages != 1 || r.Draft.View.Year != 2026 || len(fake.Requests()) != 1 {
		t.Errorf("result %+v", r)
	}
}

func TestAsk(t *testing.T) {
	s := newTestService(t)
	inv := sampleInvoice()
	if _, err := s.SaveInvoice(inv, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ask(t.Context(), AskInputDTO{Question: "Wie viele Rechnungen?"}); !errors.Is(err, ai.ErrNotSet) {
		t.Errorf("AI off: %v", err)
	}
	// The model answers with too much: a payer and a year the question does not name.
	fake := &aitest.Server{Answer: func(aitest.Request) string {
		return `{"typ":"anzahl","messgroesse":"","jahre":[2025],"filter":[{"feld":"kostentraeger","wert":"Muster PKV"},
			{"feld":"status","wert":"nicht_eingereicht"}],"gruppierung":"","grund":""}`
	}}
	withAI(t, s, fake)
	a, err := s.Ask(t.Context(), AskInputDTO{Question: "Wie viele Rechnungen sind noch nicht eingereicht?"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Answer != "1 Rechnung" || len(a.Invoices) != 1 || a.Invoices[0].Provider != "Dr. Muster" || a.Query.String() != "Anzahl · nicht eingereicht" {
		t.Errorf("answer %+v", a)
	}
	if len(a.Chips) != 2 || a.Chips[1] != (ChipDTO{Key: "status", Text: "nicht eingereicht"}) {
		t.Errorf("chips %+v", a.Chips)
	}
	// What went to the server: the question, today, the payers – and no invoice.
	req := fake.Requests()[0]
	sys := req.Text("system")
	if !strings.Contains(sys, "Heute ist der 02.11.2026") || !strings.Contains(sys, "Beihilfe Bund, Muster PKV") || strings.Contains(sys, "Dr. Muster") {
		t.Errorf("instructions:\n%s", sys)
	}
	if req.Text("user") != "Wie viele Rechnungen sind noch nicht eingereicht?" {
		t.Errorf("question %q", req.Text("user"))
	}

	// A follow-up: the earlier question and query go along.
	_, err = s.Ask(t.Context(), AskInputDTO{Question: "Und 2026?", History: []TurnDTO{{Question: "Wie viele …?", Query: a.Query}}})
	if err != nil {
		t.Fatal(err)
	}
	if req := fake.Requests()[1]; len(req.Messages) != 4 || req.Messages[2].Role != "assistant" || !strings.Contains(req.Messages[2].Text, `"nicht_eingereicht"`) {
		t.Errorf("follow-up request %+v", req.Messages)
	}

	// The chips changed by hand: run without the AI.
	q := a.Query
	q.Filters = nil
	r, err := s.RunQuery(q)
	if err != nil || r.Answer != "1 Rechnung" || len(fake.Requests()) != 2 {
		t.Errorf("run %+v %v", r, err)
	}
	v, err := s.AskVocabulary()
	if err != nil || len(v.Payers) != 2 || v.Statuses[0].Label != "nicht eingereicht" || len(v.Groups) != 5 || v.Years[0] != 2026 {
		t.Errorf("vocabulary %+v %v", v, err)
	}
}

func TestAIInputChecks(t *testing.T) {
	s := newTestService(t)
	if _, err := s.SetAISettings(AISettingsDTO{URL: "http://nutzer:geheim@10.0.0.5:8080/v1"}); err == nil {
		t.Error("address with a password accepted")
	}
	if _, err := s.SetAIKey("abc\x00def"); err == nil {
		t.Error("key with a control character accepted")
	}

	// A question while a document is read fails at once.
	started, release := make(chan struct{}), make(chan struct{})
	withAI(t, s, &aitest.Server{Answer: func(r aitest.Request) string {
		if strings.Contains(r.Text("system"), "Belege") {
			close(started)
			<-release
		}
		return `{"typ":"anzahl"}`
	}})
	reading := make(chan error)
	go func() {
		_, err := s.ReadWithAI(t.Context(), AIReadInputDTO{Pages: []string{"data:image/jpeg;base64,/9j/AA=="}})
		reading <- err
	}()
	<-started
	if _, err := s.Ask(t.Context(), AskInputDTO{Question: "Wie viele Rechnungen?"}); !errors.Is(err, ai.ErrBusy) {
		t.Errorf("busy: %v", err)
	}
	close(release)
	<-reading
}
