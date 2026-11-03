package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	if fi, err := os.Stat(cfg); err != nil || fi.Mode().Perm() != 0o600 {
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
	if res.OK || !strings.Contains(res.Checks[2].Text, "vorhanden: llama-3.2-3b") {
		t.Errorf("model %+v", res)
	}

	// Works while switched off (testing before switching on); a request needs it on.
	cur := s.AISettings()
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
