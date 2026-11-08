package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/ai"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/config"
)

// AISettingsDTO is the connection to the local AI server as the frontend
// sees it. The API key never goes back to the frontend: only whether one is
// stored and its last characters.
type AISettingsDTO struct {
	On     bool   `json:"on"`
	URL    string `json:"url"`
	Model  string `json:"model"`
	KeySet bool   `json:"keySet"`
	KeyEnd string `json:"keyEnd"` // last four characters of a long key, else ""
}

// AICheckDTO is one step of the connection test.
type AICheckDTO struct {
	OK   bool   `json:"ok"`
	Text string `json:"text"`
}

// AITestDTO is the result of the connection test.
type AITestDTO struct {
	OK     bool         `json:"ok"`
	Checks []AICheckDTO `json:"checks"`
}

func aiSettingsOf(c config.AISettings) AISettingsDTO {
	d := AISettingsDTO{On: c.On, URL: c.URL, Model: c.Model, KeySet: c.Key != ""}
	if d.URL == "" {
		d.URL = ai.DefaultURL
	}
	if d.Model == "" {
		d.Model = ai.DefaultModel
	}
	if len(c.Key) >= 12 { // showing the end of a short key would give away too much of it
		d.KeyEnd = c.Key[len(c.Key)-4:]
	}
	return d
}

// AISettings returns the AI settings of this device.
func (s *Service) AISettings() AISettingsDTO {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil {
		return aiSettingsOf(config.AISettings{})
	}
	return aiSettingsOf(s.cfg.AI)
}

// SetAISettings switches the AI on or off and sets the server address and
// the model (empty = the defaults). The key is set with SetAIKey.
func (s *Service) SetAISettings(in AISettingsDTO) (AISettingsDTO, error) {
	u, err := serverURL(in.URL)
	if err != nil {
		return AISettingsDTO{}, err
	}
	model := strings.TrimSpace(in.Model)
	if u == ai.DefaultURL {
		u = ""
	}
	if model == ai.DefaultModel {
		model = ""
	}
	if err := s.updateConfig("KI-Einstellung", func(c *config.Config) {
		c.AI.On, c.AI.URL, c.AI.Model = in.On, u, model
	}); err != nil {
		return AISettingsDTO{}, err
	}
	return s.AISettings(), nil
}

// SetAIKey stores the API key of the AI server on this device; "" removes it.
func (s *Service) SetAIKey(key string) (AISettingsDTO, error) {
	key = strings.TrimSpace(key)
	if strings.ContainsFunc(key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return AISettingsDTO{}, errors.New("Der Schlüssel enthält Leerzeichen – bitte genau so einfügen, wie er auf dem Server steht")
	}
	if err := s.updateConfig("API-Schlüssel", func(c *config.Config) { c.AI.Key = key }); err != nil {
		return AISettingsDTO{}, err
	}
	return s.AISettings(), nil
}

// serverURL checks and tidies a server address (see ai.NormalizeURL); "" is the default.
func serverURL(in string) (string, error) {
	if strings.TrimSpace(in) == "" {
		return ai.DefaultURL, nil
	}
	return ai.NormalizeURL(in)
}

// aiClient is the client for the configured server. Off counts as not set
// up unless anyway is true (the connection test works before switching on).
func (s *Service) aiClient(anyway bool) (*ai.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil {
		return nil, errors.New("Konfiguration nicht verfügbar")
	}
	if !s.cfg.AI.On && !anyway {
		return nil, fmt.Errorf("%w – in den Einstellungen unter „KI“ einschalten", ai.ErrNotSet)
	}
	d := aiSettingsOf(s.cfg.AI)
	return &ai.Client{URL: d.URL, Model: d.Model, Key: s.cfg.AI.Key, Lock: s.aiLock}, nil
}

// TestAI checks the connection: is the server there, does it take the key,
// does it have the model. Each step that ran is in the result.
func (s *Service) TestAI(ctx context.Context) (AITestDTO, error) {
	c, err := s.aiClient(true)
	if err != nil {
		return AITestDTO{}, err
	}
	out := AITestDTO{Checks: []AICheckDTO{}}
	step := func(ok bool, text string) bool {
		out.Checks = append(out.Checks, AICheckDTO{OK: ok, Text: text})
		return ok
	}
	if err := c.Health(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return AITestDTO{}, err
		}
		step(false, err.Error())
		return out, nil
	}
	step(true, "Server erreichbar")
	models, err := c.Models(ctx)
	switch {
	case errors.Is(err, context.Canceled):
		return AITestDTO{}, err
	case err != nil:
		step(false, err.Error())
		return out, nil
	case c.Key == "":
		step(true, "Kein Schlüssel gespeichert – der Server verlangt auch keinen")
	default:
		step(true, "Schlüssel angenommen")
	}
	// The models the app may use: not listed as text-only (ai.Client refuses those).
	var mine *ai.Model
	var usable []string
	for i, m := range models {
		if m.Is(c.Model) {
			mine = &models[i]
		}
		if !m.TextOnly() {
			usable = append(usable, m.ID)
		}
	}
	offer := "der Server hat kein Modell, das Bilder liest"
	if len(usable) > 0 {
		offer = "geeignet: " + strings.Join(usable, ", ")
	}
	switch {
	case len(models) == 0:
		step(false, "Der Server nennt keine Modelle")
		return out, nil
	case mine == nil:
		step(false, fmt.Sprintf("Modell „%s“ nicht gefunden – %s", c.Model, offer))
		return out, nil
	case mine.TextOnly():
		step(false, fmt.Sprintf("Modell „%s“ liest keine Bilder – die App nutzt nur Modelle, die auch Bilder lesen; %s", c.Model, offer))
		return out, nil
	case mine.Inputs != nil:
		out.OK = step(true, fmt.Sprintf("Modell „%s“ vorhanden, liest Bilder", c.Model))
	default:
		out.OK = step(true, fmt.Sprintf("Modell „%s“ vorhanden", c.Model))
	}
	return out, nil
}
