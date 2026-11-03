package ask

import (
	"bufio"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/ai"
)

// TestQuestionsLive sends the question set to the real AI server – with the
// app's own schema, instructions and corrections – and reports how many
// questions were understood right. Only on request (it takes minutes and
// needs the server):
//
//	KI_TEST=1 go test ./internal/ask -run Live -v
//
// Server and model: KI_SERVER, KI_MODEL (defaults: the server in the home
// network); the key: LLM_API_KEY or API_KEY in the .env of the repository.
// KI_TEST_OUT=<file> saves the answers (made-up questions only).
func TestQuestionsLive(t *testing.T) {
	if os.Getenv("KI_TEST") == "" {
		t.Skip("KI_TEST=1 sends the question set to the AI server")
	}
	set, s, today := loadQuestions(t)
	c := &ai.Client{URL: env("KI_SERVER", ai.DefaultURL), Model: env("KI_MODEL", ai.DefaultModel), Key: apiKey()}
	if err := c.Health(t.Context()); err != nil {
		t.Fatal(err)
	}
	type result struct {
		Question string   `json:"frage"`
		Raw      Query    `json:"antwort"`
		Final    Query    `json:"abfrage"`
		Wrong    []string `json:"fehler"`
		RawWrong []string `json:"fehlerOhneKorrektur"`
		Seconds  float64  `json:"sekunden"`
	}
	var results []result
	var history []Turn
	right, rawRight := 0, 0
	var took []float64
	for i, item := range set.Questions {
		a, err := c.Chat(t.Context(), ai.Request{Name: "abfrage", Schema: Schema(s), MaxTokens: 400, Timeout: 90 * time.Second,
			Messages: Messages(s, today, history, item.Question)})
		if err != nil {
			t.Fatalf("%d. %s: %v", i+1, item.Question, err)
		}
		var raw Query
		if err := json.Unmarshal(a.JSON, &raw); err != nil {
			t.Fatalf("%d. %s: %s", i+1, item.Question, a.JSON)
		}
		var prev *Query
		if len(history) > 0 {
			prev = &history[len(history)-1].Query
		}
		final := Normalize(raw, item.Question, prev, s, today)
		history = append(history, Turn{item.Question, final})
		r := result{Question: item.Question, Raw: raw, Final: final, Wrong: checkQuery(final, item.Want),
			RawWrong: checkQuery(valid(raw, s, today), item.Want), Seconds: a.Took.Seconds()}
		results = append(results, r)
		took = append(took, r.Seconds)
		mark := "✓"
		if len(r.Wrong) > 0 {
			mark = "✗"
		} else {
			right++
		}
		if len(r.RawWrong) == 0 {
			rawRight++
		}
		t.Logf("%2d. %s %5.1f s  %s", i+1, mark, r.Seconds, item.Question)
		if len(r.Wrong) > 0 {
			t.Logf("       %s\n       %s", final.JSON(), strings.Join(r.Wrong, "; "))
		}
	}
	n := len(set.Questions)
	t.Logf("Richtig verstanden: %d von %d (%d %%), ohne die Korrekturen der App %d (%d %%) · Median %.1f s",
		right, n, right*100/n, rawRight, rawRight*100/n, median(took))
	if out := os.Getenv("KI_TEST_OUT"); out != "" {
		b, _ := json.MarshalIndent(results, "", " ")
		if err := os.WriteFile(out, b, 0o644); err != nil {
			t.Error(err)
		}
	}
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// apiKey is LLM_API_KEY, else API_KEY (or LLM_API_KEY) from the .env of the repository.
func apiKey() string {
	if k := os.Getenv("LLM_API_KEY"); k != "" {
		return k
	}
	f, err := os.Open("../../.env")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, value, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if ok && (name == "API_KEY" || name == "LLM_API_KEY") {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := slices.Clone(v)
	slices.Sort(s)
	return s[len(s)/2]
}
