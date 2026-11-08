package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/ai/aitest"
)

// fakeServer answers like llama-server and wants the key "key".
func fakeServer(t *testing.T, answer string) (*httptest.Server, *aitest.Server) {
	t.Helper()
	fake := &aitest.Server{Key: "key", Answer: func(aitest.Request) string { return answer }}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return srv, fake
}

func schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"betrag": map[string]any{"type": "string"}}}
}

func TestChat(t *testing.T) {
	srv, fake := fakeServer(t, "```json\n{\"betrag\": \"20.29\"}\n```")
	c := &Client{URL: srv.URL + "/v1/", Model: "qwen3-vl-8b", Key: "key", Lock: NewLock()}
	if err := c.Health(t.Context()); err != nil {
		t.Fatal(err)
	}
	models, err := c.Models(t.Context())
	if err != nil || len(models) != 1 || models[0].ID != "qwen3-vl-8b" || models[0].Inputs != nil {
		t.Fatalf("models %v %v", models, err)
	}
	a, err := c.Chat(t.Context(), Request{Name: "beleg", Schema: schema(),
		Messages: []Message{{Role: "user", Content: []Part{TextPart("Lies den Beleg"), ImagePart("data:image/jpeg;base64,AAAA")}}}})
	if err != nil {
		t.Fatal(err)
	}
	if string(a.JSON) != `{"betrag": "20.29"}` || a.Timings.PromptTokens != 1675 || a.Timings.PerSecond != 6.8 {
		t.Errorf("answer %s %+v", a.JSON, a.Timings)
	}
	r := fake.Requests()[0]
	if r.Model != "qwen3-vl-8b" || r.Schema["type"] != "object" || r.Text("user") != "Lies den Beleg" ||
		len(r.Messages[0].Images) != 1 || r.Messages[0].Images[0] != "data:image/jpeg;base64,AAAA" {
		t.Errorf("request %+v", r)
	}
}

func TestErrors(t *testing.T) {
	srv, _ := fakeServer(t, "Das kann ich nicht lesen.")
	cases := []struct {
		name   string
		client Client
		want   error
		text   string
	}{
		{"wrong key", Client{URL: srv.URL + "/v1", Key: "falsch"}, ErrKey, "Schlüssel prüfen"},
		{"no key", Client{URL: srv.URL + "/v1"}, ErrKey, "HTTP 401"},
		{"no JSON", Client{URL: srv.URL + "/v1", Key: "key"}, ErrAnswer, ""},
		{"wrong path", Client{URL: srv.URL + "/api", Key: "key"}, ErrUnreachable, "endet auf /v1"},
		{"bad address", Client{URL: "192.168.178.38:8080"}, ErrNotSet, "keine Serveradresse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.client.Chat(t.Context(), Request{Schema: schema()})
			if !errors.Is(err, tc.want) || !strings.Contains(err.Error(), tc.text) {
				t.Errorf("got %v, want %v with %q", err, tc.want, tc.text)
			}
		})
	}
}

// The home server also offers a text-only model that searches the web:
// nothing goes to a model the server lists as reading no images.
func TestTextOnlyModel(t *testing.T) {
	srv, fake := fakeServer(t, `{"ok": "ja"}`)
	fake.Models = []string{"qwen3-vl-8b", "qwen3.5-4b", "anderes"}
	fake.Inputs = map[string][]string{"qwen3-vl-8b": {"text", "image"}, "qwen3.5-4b": {"text"}}
	chat := func(model string) error {
		_, err := (&Client{URL: srv.URL + "/v1", Model: model, Key: "key"}).Chat(t.Context(), Request{Schema: schema()})
		return err
	}
	err := chat("qwen3.5-4b")
	if !errors.Is(err, ErrTextOnly) || !strings.Contains(err.Error(), "„qwen3.5-4b“") || len(fake.Requests()) != 0 {
		t.Errorf("text-only model: %v, %d requests sent", err, len(fake.Requests()))
	}
	// Reads images; the server does not say; not listed (the server decides).
	for _, model := range []string{"qwen3-vl-8b", "anderes", "nicht-gelistet"} {
		if err := chat(model); err != nil {
			t.Errorf("%s: %v", model, err)
		}
	}
	if n := len(fake.Requests()); n != 3 {
		t.Errorf("%d requests sent, want 3", n)
	}

	// Without the model list nothing is sent.
	var chats atomic.Int32
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			chats.Add(1)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(broken.Close)
	if _, err := (&Client{URL: broken.URL + "/v1", Model: "qwen3.5-4b"}).Chat(t.Context(), Request{}); err == nil || chats.Load() != 0 {
		t.Errorf("without a model list: %v, %d chats", err, chats.Load())
	}

	// Aliases count as the model.
	m := Model{ID: "qwen3.5-4b", Aliases: []string{"chat"}, Inputs: []string{"text"}}
	if !m.Is("chat") || !m.Is("qwen3.5-4b") || m.Is("qwen3") || !m.TextOnly() {
		t.Errorf("model %+v", m)
	}
	if (Model{ID: "x"}).TextOnly() || (Model{ID: "x", Inputs: []string{"text", "image"}}).TextOnly() {
		t.Error("unknown or image model counted as text-only")
	}
}

func TestUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close() // nothing listens there any more
	c := &Client{URL: addr + "/v1"}
	err := c.Health(t.Context())
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), strings.TrimPrefix(addr, "http://")) {
		t.Errorf("health %v", err)
	}
	if _, err := c.Chat(t.Context(), Request{}); !errors.Is(err, ErrUnreachable) {
		t.Errorf("chat %v", err)
	}
}

func TestModelLoadingAndMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":503,"message":"Loading model"}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"model 'gibtsnicht' not found"}}`))
	}))
	t.Cleanup(srv.Close)
	c := &Client{URL: srv.URL + "/v1", Model: "gibtsnicht"}
	if err := c.Health(t.Context()); !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "startet noch") {
		t.Errorf("health %v", err)
	}
	if _, err := c.Chat(t.Context(), Request{}); !errors.Is(err, ErrModel) || !strings.Contains(err.Error(), "gibtsnicht") {
		t.Errorf("chat %v", err)
	}
}

// slowServer answers chat requests only when release is closed (or the request is cancelled).
func slowServer(t *testing.T, release chan struct{}, started *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" { // the model check before each request
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		_, _ = io.Copy(io.Discard, r.Body) // only then does the server notice a closed connection
		started.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":\"ja\"}"}}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	return srv
}

func TestTimeoutAndCancel(t *testing.T) {
	var started atomic.Int32
	srv := slowServer(t, make(chan struct{}), &started)
	c := &Client{URL: srv.URL + "/v1"}

	_, err := c.Chat(t.Context(), Request{Timeout: 50 * time.Millisecond})
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("timeout: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error)
	go func() {
		_, err := c.Chat(ctx, Request{Timeout: time.Minute})
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling did not stop the request")
	}
}

func TestOneRequestAtATime(t *testing.T) {
	var started atomic.Int32
	release := make(chan struct{})
	srv := slowServer(t, release, &started)
	lock := NewLock()
	c := &Client{URL: srv.URL + "/v1", Lock: lock}

	first := make(chan error)
	go func() {
		_, err := c.Chat(t.Context(), Request{})
		first <- err
	}()
	for started.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	// A second request waits for the lock; cancelling it while it waits sends nothing.
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Chat(ctx, Request{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("waiting request: %v", err)
	}
	if n := started.Load(); n != 1 {
		t.Errorf("%d requests reached the server", n)
	}
	// A request that must not wait gives up at once.
	if _, err := c.Chat(t.Context(), Request{NoWait: true}); !errors.Is(err, ErrBusy) {
		t.Errorf("no wait: %v", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	// Free again.
	if _, err := c.Chat(t.Context(), Request{NoWait: true}); err != nil {
		t.Fatal(err)
	}
}

func TestKeyErrorsAndAddresses(t *testing.T) {
	// A proxy that wants the key for /health too.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	t.Cleanup(srv.Close)
	err := (&Client{URL: srv.URL + "/v1"}).Health(t.Context())
	if !errors.Is(err, ErrKey) || !strings.Contains(err.Error(), "verlangt einen API-Schlüssel") {
		t.Errorf("no key: %v", err)
	}
	err = (&Client{URL: srv.URL + "/v1", Key: "falsch"}).Health(t.Context())
	if !errors.Is(err, ErrKey) || !strings.Contains(err.Error(), "den Schlüssel prüfen") {
		t.Errorf("wrong key: %v", err)
	}

	for in, want := range map[string]string{
		"http://10.0.0.5:8080": "http://10.0.0.5:8080/v1", " https://ki.local/v1/ ": "https://ki.local/v1",
		"http://user:pass@10.0.0.5/v1": "", "10.0.0.5:8080": "", "ftp://x/v1": "", "http://x/v1?a=1": "",
	} {
		got, err := NormalizeURL(in)
		if got != want || (want == "") != (err != nil) {
			t.Errorf("NormalizeURL(%q) = %q, %v", in, got, err)
		}
	}
}
