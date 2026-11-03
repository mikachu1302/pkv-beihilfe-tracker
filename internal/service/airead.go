package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/airead"
)

// AIReadInputDTO is a document to read with the AI: its pages as images and
// what the text recognition found on them (for the checks).
type AIReadInputDTO struct {
	Pages []string  `json:"pages"` // data URLs (JPEG); the first airead.MaxPages are sent
	Lines []LineDTO `json:"lines"`
	QR    []string  `json:"qr"`
}

// AIOtherDTO is the value of a field that lost against the one taken.
type AIOtherDTO struct {
	Source string `json:"source"` // qr, label, guess (text recognition), ai, ai-confirmed
	Value  string `json:"value"`  // "2026-03-14", "286.40", text
}

// AIReadDTO is what the AI read, merged with the text recognition.
type AIReadDTO struct {
	Kind      string                `json:"kind"`   // invoice, refund, unknown (as AnalysisDTO)
	AIKind    string                `json:"aiKind"` // what the AI takes it for: invoice, refund, prescription, other
	Draft     InvoiceDetailDTO      `json:"draft"`
	Sources   map[string]string     `json:"sources"` // field → qr | ai-confirmed | label | ai | guess
	Others    map[string]AIOtherDTO `json:"others"`  // field → the value that lost
	FromAI    []string              `json:"fromAI"`  // fields whose value came from the AI
	Confirmed int                   `json:"confirmed"`
	Notes     []string              `json:"notes"`
	Pages     int                   `json:"pages"`   // pages sent
	Seconds   int                   `json:"seconds"` // how long the AI took
}

// maxPageBytes bounds one page image (a 1,400 px JPEG is a few hundred KB).
const maxPageBytes = 8 << 20

var pageTypes = []string{"data:image/jpeg;base64,", "data:image/png;base64,", "data:image/webp;base64,"}

// ReadWithAI reads a document with the local AI server and merges the
// answer with the text recognition (GiroCode > AI confirmed > text
// recognition > AI alone). It takes about a minute per page; cancelling
// the call stops the request.
func (s *Service) ReadWithAI(ctx context.Context, in AIReadInputDTO) (AIReadDTO, error) {
	if len(in.Pages) == 0 {
		return AIReadDTO{}, errors.New("keine Seiten zum Auslesen")
	}
	for i, p := range in.Pages {
		if len(p) > maxPageBytes || !slices.ContainsFunc(pageTypes, func(t string) bool { return strings.HasPrefix(p, t) }) {
			return AIReadDTO{}, fmt.Errorf("Seite %d ist kein Bild, das die KI lesen kann", i+1)
		}
	}
	c, err := s.aiClient(false)
	if err != nil {
		return AIReadDTO{}, err
	}
	known, err := s.knownProviders()
	if err != nil {
		return AIReadDTO{}, err
	}
	// Without the lock: the AI takes minutes, the app keeps working meanwhile.
	r, err := airead.Read(ctx, c, airead.Input{Pages: in.Pages, Lines: in.Lines, QR: in.QR, Known: known, Now: s.now()})
	if err != nil {
		return AIReadDTO{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return AIReadDTO{}, err
	}
	inv := draftInvoice(r.Draft)
	out := AIReadDTO{Kind: r.Draft.Kind, AIKind: r.AIKind, Draft: s.detail(&data.Settings, &inv), Sources: r.Draft.Sources,
		Others: map[string]AIOtherDTO{}, FromAI: r.FromAI, Confirmed: r.Confirmed, Notes: r.Draft.Notes,
		Pages: r.Pages, Seconds: int(math.Round(r.Took.Seconds()))}
	for f, o := range r.Others {
		out.Others[f] = AIOtherDTO(o)
	}
	if out.FromAI == nil {
		out.FromAI = []string{}
	}
	if out.Notes == nil {
		out.Notes = []string{}
	}
	return out, nil
}

// knownProviders are the providers of the invoices (for confirming the AI's reading).
func (s *Service) knownProviders() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, data, err := s.ready()
	if err != nil {
		return nil, err
	}
	var out []string
	for i := range data.Invoices {
		if inv := &data.Invoices[i]; !inv.Deleted && inv.Provider != "" && !slices.Contains(out, inv.Provider) {
			out = append(out, inv.Provider)
		}
	}
	return out, nil
}
