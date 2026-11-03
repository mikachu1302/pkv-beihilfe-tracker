// Package ai talks to the local AI server: an OpenAI-compatible chat API in
// the home network (llama.cpp's llama-server). Only net/http – no SDK.
//
// The server is shared (other devices use it too) and slow (about a minute
// per document page), so the app sends one request at a time and every
// request starts with a click. Errors are worded for the UI, in German.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Defaults of the settings: the server in the home network (see docs/PLAN-v0.5.md).
const (
	DefaultURL   = "http://192.168.178.38:8080/v1"
	DefaultModel = "qwen3-vl-8b"
)

// Timeouts of the quick requests: the health check before a request and the
// model list. A quick request that times out means the server is not there.
const (
	HealthTimeout = 3 * time.Second
	ListTimeout   = 6 * time.Second
)

// Errors a caller may tell apart (errors.Is); the messages are shown as they are.
var (
	ErrUnreachable = errors.New("KI-Server nicht erreichbar")
	ErrKey         = errors.New("Der KI-Server lehnt den API-Schlüssel ab")
	ErrModel       = errors.New("Modell auf dem KI-Server nicht vorhanden")
	ErrTimeout     = errors.New("Der KI-Server hat nicht rechtzeitig geantwortet")
	ErrAnswer      = errors.New("Die Antwort des KI-Servers ist nicht lesbar")
	ErrNotSet      = errors.New("KI ist nicht eingerichtet")
	ErrBusy        = errors.New("Die KI ist gerade beschäftigt (liest einen Beleg oder beantwortet eine Frage) – bitte gleich noch einmal versuchen")
)

// Client sends requests to one server. The zero Lock lets requests run in
// parallel; the app shares one Lock between all its clients.
type Client struct {
	URL   string // base URL of the API, ending in /v1
	Model string
	Key   string // API key, sent as "Authorization: Bearer …"; "" = none
	Lock  *Lock
	HTTP  *http.Client // nil = a client without its own timeout (the contexts set them)
}

// Lock lets one request through at a time. Waiting for it stops with the
// context (the user cancelled).
type Lock struct{ ch chan struct{} }

// NewLock returns an open lock.
func NewLock() *Lock { return &Lock{ch: make(chan struct{}, 1)} }

// acquire waits for the lock (or the context); with noWait it gives up at once with ErrBusy.
func (l *Lock) acquire(ctx context.Context, noWait bool) (release func(), err error) {
	if l == nil {
		return func() {}, nil
	}
	if noWait {
		select {
		case l.ch <- struct{}{}:
			return func() { <-l.ch }, nil
		default:
			return nil, ErrBusy
		}
	}
	select {
	case l.ch <- struct{}{}:
		return func() { <-l.ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// base is the API URL without a trailing slash; an error if it is no http(s) URL.
func (c *Client) base() (string, error) {
	u, err := NormalizeURL(c.URL)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNotSet, err)
	}
	return u, nil
}

// NormalizeURL checks and tidies a server address: http(s) with a host, no
// user name or password in it (it would be shown in messages), no query; an
// address without a path gets /v1 (llama.cpp, LM Studio and Ollama serve
// the API there).
func NormalizeURL(in string) (string, error) {
	in = strings.TrimSpace(in)
	u, err := url.Parse(in)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("„%s“ ist keine Serveradresse – Beispiel: %s", in, DefaultURL)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if u.Path == "" {
		u.Path = "/v1"
	}
	return u.String(), nil
}

// Health asks the server whether it is up (llama.cpp: GET /health, no key,
// next to /v1). A server without /health (404) counts as up.
func (c *Client) Health(ctx context.Context) error {
	base, err := c.base()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, HealthTimeout)
	defer cancel()
	health := strings.TrimSuffix(base, "/v1") + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, health, nil)
	if err != nil {
		return err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return c.quick(c.netError(ctx, err, HealthTimeout))
	}
	defer drain(resp)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return c.keyError(resp.StatusCode) // a proxy in front of the server that wants the key for /health too
	case resp.StatusCode == http.StatusServiceUnavailable:
		// llama.cpp answers 503 while it loads the model.
		return fmt.Errorf("%w: Der KI-Server startet noch (Modell wird geladen) – bitte gleich noch einmal versuchen", ErrUnreachable)
	case resp.StatusCode >= 300:
		return fmt.Errorf("%w: %s antwortet mit HTTP %d", ErrUnreachable, health, resp.StatusCode)
	}
	return nil
}

// Models lists the models the server offers (GET /v1/models, with the key).
func (c *Client) Models(ctx context.Context) ([]string, error) {
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/models", nil, &out, ListTimeout); err != nil {
		return nil, c.quick(err)
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// Message is one chat message. Content is a string or a list of Parts.
type Message struct {
	Role    string `json:"role"` // system, user, assistant
	Content any    `json:"content"`
}

// Part is a piece of a user message: text or an image.
type Part struct {
	Type     string    `json:"type"` // text, image_url
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL holds an image as a data URL (data:image/jpeg;base64,…).
type ImageURL struct {
	URL string `json:"url"`
}

// TextPart and ImagePart build the parts of a message.
func TextPart(s string) Part { return Part{Type: "text", Text: s} }

// ImagePart wraps a data URL.
func ImagePart(dataURL string) Part {
	return Part{Type: "image_url", ImageURL: &ImageURL{URL: dataURL}}
}

// Request is a chat request whose answer must be JSON of Schema.
type Request struct {
	Messages  []Message
	Schema    any           // JSON schema of the answer (enforced by a grammar in llama.cpp); a map or json.RawMessage
	Name      string        // name of the schema, e.g. "beleg"
	MaxTokens int           // 0 = 800
	Timeout   time.Duration // for this request, after the lock was taken; 0 = 60 s
	NoWait    bool          // fail with ErrBusy instead of waiting for another request
}

// Timings are llama.cpp's speed figures of a request.
type Timings struct {
	PromptTokens    int     `json:"prompt_n"`
	PromptPerSecond float64 `json:"prompt_per_second"`
	Tokens          int     `json:"predicted_n"`
	PerSecond       float64 `json:"predicted_per_second"`
}

// Answer is the model's JSON answer and how long it took.
type Answer struct {
	JSON    json.RawMessage
	Timings Timings
	Took    time.Duration
}

// Chat sends one request (waiting for the lock first) and returns the JSON
// object the model answered with.
func (c *Client) Chat(ctx context.Context, r Request) (Answer, error) {
	release, err := c.Lock.acquire(ctx, r.NoWait)
	if err != nil {
		return Answer{}, err
	}
	defer release()
	timeout := r.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	maxTokens := r.MaxTokens
	if maxTokens == 0 {
		maxTokens = 800
	}
	body := map[string]any{
		"model": c.Model, "temperature": 0, "max_tokens": maxTokens, "messages": r.Messages,
		"response_format": map[string]any{"type": "json_schema",
			"json_schema": map[string]any{"name": r.Name, "strict": true, "schema": r.Schema}},
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Timings Timings `json:"timings"`
	}
	start := time.Now()
	if err := c.do(ctx, http.MethodPost, "/chat/completions", body, &out, timeout); err != nil {
		return Answer{}, err
	}
	if len(out.Choices) == 0 {
		return Answer{}, fmt.Errorf("%w (keine Antwort enthalten)", ErrAnswer)
	}
	ch := out.Choices[0]
	obj, ok := jsonObject(ch.Message.Content)
	if !ok {
		if ch.FinishReason == "length" {
			return Answer{}, fmt.Errorf("%w (zu lang, abgeschnitten)", ErrAnswer)
		}
		return Answer{}, ErrAnswer
	}
	return Answer{JSON: obj, Timings: out.Timings, Took: time.Since(start)}, nil
}

// jsonObject finds the JSON object in s – also inside a ```json fence or
// after some text (servers without schema support).
func jsonObject(s string) (json.RawMessage, bool) {
	start, end := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}')
	if start < 0 || end < start {
		return nil, false
	}
	raw := json.RawMessage(s[start : end+1])
	var probe map[string]any
	if json.Unmarshal(raw, &probe) != nil {
		return nil, false
	}
	return raw, true
}

// do sends a request to the API and decodes the JSON answer into out.
func (c *Client) do(ctx context.Context, method, path string, in, out any, timeout time.Duration) error {
	base, err := c.base()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return c.netError(ctx, err, timeout)
	}
	defer drain(resp)
	if resp.StatusCode >= 300 {
		return c.statusError(resp)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return c.netError(ctx, err, timeout)
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("%w (%w)", ErrAnswer, err)
	}
	return nil
}

// statusError turns an HTTP error status into a message for the user.
func (c *Client) statusError(resp *http.Response) error {
	msg := serverMessage(resp)
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return c.keyError(resp.StatusCode)
	case http.StatusNotFound:
		if strings.Contains(strings.ToLower(msg), "model") {
			return fmt.Errorf("%w: „%s“", ErrModel, c.Model)
		}
		return fmt.Errorf("%w: %s gibt es nicht – ist die Adresse richtig (endet auf /v1)?", ErrUnreachable, resp.Request.URL)
	case http.StatusServiceUnavailable:
		return fmt.Errorf("%w: Der KI-Server ist beschäftigt oder startet noch – bitte gleich noch einmal versuchen", ErrUnreachable)
	}
	if strings.Contains(strings.ToLower(msg), "model") && strings.Contains(strings.ToLower(msg), "not found") {
		return fmt.Errorf("%w: „%s“", ErrModel, c.Model)
	}
	if msg != "" {
		return fmt.Errorf("Der KI-Server meldet einen Fehler (HTTP %d): %s", resp.StatusCode, msg)
	}
	return fmt.Errorf("Der KI-Server meldet einen Fehler (HTTP %d)", resp.StatusCode)
}

// keyError: the server refused the key – or wants one and none is stored.
func (c *Client) keyError(status int) error {
	if c.Key == "" {
		return fmt.Errorf("%w: Der KI-Server verlangt einen API-Schlüssel (HTTP %d) – in den Einstellungen unter „KI“ eintragen", ErrKey, status)
	}
	return fmt.Errorf("%w (HTTP %d) – in den Einstellungen unter „KI“ den Schlüssel prüfen", ErrKey, status)
}

// serverMessage is the error text of an OpenAI-style error body, shortened.
func serverMessage(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	msg := strings.TrimSpace(string(b))
	if json.Unmarshal(b, &e) == nil && e.Error.Message != "" {
		msg = e.Error.Message
	}
	if len(msg) > 200 {
		msg = msg[:200] + " …"
	}
	return msg
}

// netError explains a failed connection: cancelled by the user (the context
// error as it is), timed out, or not reachable.
func (c *Client) netError(ctx context.Context, err error, timeout time.Duration) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return ctx.Err()
	}
	var ne net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout() {
		return fmt.Errorf("%w (nach %s)", ErrTimeout, seconds(timeout))
	}
	return c.notThere()
}

// quick turns a timeout of a quick request into "not reachable": a server
// that is there answers those at once.
func (c *Client) quick(err error) error {
	if errors.Is(err, ErrTimeout) {
		return c.notThere()
	}
	return err
}

func (c *Client) notThere() error {
	return fmt.Errorf("%w: %s antwortet nicht – läuft der Rechner mit dem KI-Server, ist er im WLAN?", ErrUnreachable, c.host())
}

// seconds formats d for the user: "300 s", "0,05 s".
func seconds(d time.Duration) string {
	return strings.Replace(strconv.FormatFloat(d.Seconds(), 'f', -1, 64), ".", ",", 1) + " s"
}

func (c *Client) host() string {
	if u, err := url.Parse(c.URL); err == nil && u.Host != "" {
		return u.Host
	}
	return c.URL
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}
