package service

import (
	"errors"
	"fmt"
)

// AppearanceDTO holds how the app looks on this device (kept in the device
// configuration, not in the data folder: two devices may look different).
type AppearanceDTO struct {
	Theme string `json:"theme"` // "" = like the system, "light", "dark"
}

// Appearance returns the appearance settings of this device.
func (s *Service) Appearance() AppearanceDTO {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil {
		return AppearanceDTO{}
	}
	return AppearanceDTO{Theme: s.cfg.Theme}
}

// SetTheme sets the colour scheme of this device: "", "light" or "dark".
func (s *Service) SetTheme(theme string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch theme {
	case "", "light", "dark":
	default:
		return fmt.Errorf("unbekanntes Farbschema %q", theme)
	}
	if s.cfg == nil {
		return errors.New("Konfiguration nicht verfügbar")
	}
	old := s.cfg.Theme
	s.cfg.Theme = theme
	if err := s.cfg.Save(s.cfgDir); err != nil {
		s.cfg.Theme = old
		return fmt.Errorf("Farbschema konnte nicht gespeichert werden: %w", err)
	}
	return nil
}
