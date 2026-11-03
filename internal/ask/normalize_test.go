package ask

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
)

// questionSet is testdata/fragen.json: made-up questions with the query
// they mean ("erwartet": flat fields, filters as keys; "folge": the number
// of the question a follow-up builds on).
type questionSet struct {
	Today     string   `json:"heute"`
	Payers    []string `json:"kostentraeger"`
	Questions []struct {
		Question string         `json:"frage"`
		Want     map[string]any `json:"erwartet"`
		FollowUp int            `json:"folge"`
	} `json:"fragen"`
}

func loadQuestions(t *testing.T) (questionSet, *model.Settings, civil.Date) {
	t.Helper()
	b, err := os.ReadFile("testdata/fragen.json")
	if err != nil {
		t.Fatal(err)
	}
	var set questionSet
	if err := json.Unmarshal(b, &set); err != nil {
		t.Fatal(err)
	}
	s := &model.Settings{Payers: []model.Payer{{ID: "bh", Kind: model.Beihilfe, Name: set.Payers[0]}, {ID: "pkv", Kind: model.PKV, Name: set.Payers[1]}}}
	return set, s, civil.MustParse(set.Today)
}

// checkQuery compares a query with the expected fields: they must match, and
// filters or a grouping the question does not ask for are wrong too (they
// change the answer).
func checkQuery(q Query, want map[string]any) []string {
	got := map[string]string{"typ": q.Kind, "messgroesse": q.Measure, "gruppierung": q.GroupBy, "grund": q.Reason}
	for _, f := range q.Filters {
		got[f.Field] = f.Value
	}
	var wrong []string
	for k, v := range want {
		if k == "jahre" {
			var years []int
			for _, y := range v.([]any) {
				years = append(years, int(y.(float64)))
			}
			if !slices.Equal(years, q.Years) && !(len(years) == 0 && len(q.Years) == 0) {
				wrong = append(wrong, fmt.Sprintf("jahre %v statt %v", q.Years, years))
			}
			continue
		}
		if got[k] != v.(string) {
			wrong = append(wrong, fmt.Sprintf("%s „%s“ statt „%s“", k, got[k], v))
		}
	}
	if _, ok := want["jahre"]; !ok && len(q.Years) > 0 && want["typ"] != KindUnsupported {
		wrong = append(wrong, fmt.Sprintf("jahre %v zu viel", q.Years))
	}
	if want["typ"] != KindUnsupported {
		for _, k := range append(slices.Clone(FilterFields), "gruppierung") {
			if _, ok := want[k]; !ok && got[k] != "" {
				wrong = append(wrong, fmt.Sprintf("%s „%s“ zu viel", k, got[k]))
			}
		}
	}
	slices.Sort(wrong)
	return wrong
}

// normalizeAll corrects the model's answers question by question; a
// follow-up gets the corrected query of the question it builds on, the
// others the one before (as in a conversation).
func normalizeAll(set questionSet, answers []Query, s *model.Settings, today civil.Date) []Query {
	out := make([]Query, len(answers))
	for i, a := range answers {
		var prev *Query
		switch {
		case set.Questions[i].FollowUp > 0:
			prev = &out[set.Questions[i].FollowUp-1]
		case i > 0:
			prev = &out[i-1]
		}
		out[i] = Normalize(a, set.Questions[i].Question, prev, s, today)
	}
	return out
}

// The corrections against what the model answered in the tests of
// 2026-11-03 (testdata/antworten-2026-11-03.json; run 1 with every filter
// field required – 25 % right, run 2 with the filter list – 68 % right).
func TestNormalizeRecordedAnswers(t *testing.T) {
	set, s, today := loadQuestions(t)
	b, err := os.ReadFile("testdata/antworten-2026-11-03.json")
	if err != nil {
		t.Fatal(err)
	}
	var runs map[string]json.RawMessage
	if err := json.Unmarshal(b, &runs); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"lauf1", "lauf2"} {
		var answers []Query
		if err := json.Unmarshal(runs[name], &answers); err != nil {
			t.Fatal(err)
		}
		right := 0
		var report strings.Builder
		for i, q := range normalizeAll(set, answers, s, today) {
			if wrong := checkQuery(q, set.Questions[i].Want); len(wrong) > 0 {
				fmt.Fprintf(&report, "%2d. %s\n    %s\n    %s\n", i+1, set.Questions[i].Question, q.JSON(), strings.Join(wrong, "; "))
			} else {
				right++
			}
		}
		share := right * 100 / len(answers)
		t.Logf("%s: %d von %d richtig (%d %%)\n%s", name, right, len(answers), share, report.String())
		if share < 90 {
			t.Errorf("%s: only %d %% right after the corrections", name, share)
		}
	}
}
