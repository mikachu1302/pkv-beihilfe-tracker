package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/ai"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/ask"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/calc"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
)

// QuestionTimeout bounds the AI's work on one question (a few seconds usually).
const QuestionTimeout = 60 * time.Second

// QueryDTO is a query as the AI writes it (German keys, see internal/ask).
type QueryDTO = ask.Query

// FilterDTO is one condition of a query.
type FilterDTO = ask.Filter

// TurnDTO is an earlier question with the query the app ran (for follow-ups).
type TurnDTO = ask.Turn

// ChipDTO is one part of how the app understood a question.
type ChipDTO struct {
	Key  string `json:"key"` // "typ", "jahre", "gruppierung" or a filter field
	Text string `json:"text"`
}

// FigureDTO is a labelled number of an answer ("3.041,23 €").
type FigureDTO struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// GroupDTO is one line of a grouped answer.
type GroupDTO struct {
	Label string `json:"label"`
	Count int    `json:"count"`
	Value string `json:"value"`
}

// AnswerDTO is the app's answer to a question: the query it ran (as chips),
// the result in a sentence, figures, groups and the invoices it is about.
type AnswerDTO struct {
	Query    QueryDTO        `json:"query"`
	Chips    []ChipDTO       `json:"chips"`
	Answer   string          `json:"answer"`
	Figures  []FigureDTO     `json:"figures"`
	Groups   []GroupDTO      `json:"groups"`
	Invoices []InvoiceRowDTO `json:"invoices"`
	Seconds  float64         `json:"seconds"` // how long the AI took; 0 when run without it
}

// AskInputDTO is a question and the conversation so far.
type AskInputDTO struct {
	Question string    `json:"question"`
	History  []TurnDTO `json:"history"` // the last questions of this session with their queries
}

// Ask has the AI turn the question into a query, corrects it (see
// ask.Normalize) and runs it on the data. The question, today's date and the
// app's vocabulary (payer names, categories) go to the AI server – no invoice.
func (s *Service) Ask(ctx context.Context, in AskInputDTO) (AnswerDTO, error) {
	question := strings.TrimSpace(in.Question)
	if question == "" {
		return AnswerDTO{}, errors.New("bitte eine Frage eingeben")
	}
	if len([]rune(question)) > 500 {
		return AnswerDTO{}, errors.New("die Frage ist zu lang (höchstens 500 Zeichen)")
	}
	c, err := s.aiClient(false)
	if err != nil {
		return AnswerDTO{}, err
	}
	if c.Lock.Busy() {
		// A document takes minutes; a question should not wait for it unnoticed.
		return AnswerDTO{}, errors.New("Die KI liest gerade einen Beleg – die Frage bitte gleich noch einmal stellen.")
	}
	s.mu.Lock()
	_, data, err := s.ready()
	if err != nil {
		s.mu.Unlock()
		return AnswerDTO{}, err
	}
	settings := data.Settings // a copy: the AI call runs without the lock
	today := s.today()
	s.mu.Unlock()

	if err := c.Health(ctx); err != nil {
		return AnswerDTO{}, err
	}
	a, err := c.Chat(ctx, ai.Request{Name: "abfrage", Schema: ask.Schema(&settings), MaxTokens: 400, Timeout: QuestionTimeout,
		Messages: ask.Messages(&settings, today, in.History, question)})
	if err != nil {
		return AnswerDTO{}, err
	}
	var q ask.Query
	if err := json.Unmarshal(a.JSON, &q); err != nil {
		return AnswerDTO{}, fmt.Errorf("%w (%s)", ai.ErrAnswer, err)
	}
	var prev *ask.Query
	if n := len(in.History); n > 0 {
		prev = &in.History[n-1].Query
	}
	q = ask.Normalize(q, question, prev, &settings, today)
	out, err := s.RunQuery(q)
	out.Seconds = a.Took.Seconds()
	return out, err
}

// RunQuery runs a query without the AI – for a query whose chips the user
// changed (a filter removed, another year chosen).
func (s *Service) RunQuery(q QueryDTO) (AnswerDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return AnswerDTO{}, err
	}
	today := s.today()
	if q.Years == nil {
		q.Years = []int{}
	}
	if q.Filters == nil {
		q.Filters = []ask.Filter{}
	}
	r := ask.Run(ask.Data{Settings: &data.Settings, Invoices: data.Invoices, Allowances: data.Allowances}, q, today)
	out := AnswerDTO{Query: q, Answer: r.Answer, Chips: []ChipDTO{}, Figures: []FigureDTO{}, Groups: []GroupDTO{}, Invoices: []InvoiceRowDTO{}}
	for _, c := range ask.Describe(q) {
		out.Chips = append(out.Chips, ChipDTO(c))
	}
	for _, f := range r.Figures {
		out.Figures = append(out.Figures, FigureDTO(f))
	}
	for _, g := range r.Groups {
		out.Groups = append(out.Groups, GroupDTO(g))
	}
	for _, id := range r.Invoices {
		i := slices.IndexFunc(data.Invoices, func(inv model.Invoice) bool { return inv.ID == id })
		if i >= 0 {
			inv := &data.Invoices[i]
			out.Invoices = append(out.Invoices, rowDTO(inv, calc.Invoice(&data.Settings, inv, today)))
		}
	}
	return out, nil
}

// ChoiceDTO is a value to choose with its label.
type ChoiceDTO struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// AskVocabularyDTO are the values the chips can be changed to.
type AskVocabularyDTO struct {
	Years      []int       `json:"years"`
	Payers     []ChoiceDTO `json:"payers"`
	Statuses   []ChoiceDTO `json:"statuses"`
	Categories []ChoiceDTO `json:"categories"`
	Groups     []ChoiceDTO `json:"groups"`
}

// AskVocabulary lists the values a chip can be changed to.
func (s *Service) AskVocabulary() (AskVocabularyDTO, error) {
	years, err := s.Years()
	if err != nil {
		return AskVocabularyDTO{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return AskVocabularyDTO{}, err
	}
	v := AskVocabularyDTO{Years: years}
	for _, p := range data.Settings.Payers {
		v.Payers = append(v.Payers, ChoiceDTO{p.Name, p.Name})
	}
	for _, st := range ask.Statuses {
		v.Statuses = append(v.Statuses, ChoiceDTO{st, ask.StatusLabels[st]})
	}
	for _, c := range ask.Categories {
		v.Categories = append(v.Categories, ChoiceDTO{c, ask.CategoryLabels[c]})
	}
	for _, g := range ask.Groups[1:] {
		v.Groups = append(v.Groups, ChoiceDTO{g, ask.GroupLabels[g]})
	}
	return v, nil
}
