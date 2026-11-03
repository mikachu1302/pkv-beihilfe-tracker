package service

import (
	"errors"
	"fmt"
	"slices"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/config"
)

// AppearanceDTO holds how the app looks on this device (kept in the device
// configuration, not in the data folder: two devices may look different).
type AppearanceDTO struct {
	Theme  string `json:"theme"`  // "" = like the system, "light", "dark"
	Design string `json:"design"` // "new" (the default) or "classic"
	// DesignHint: show the one-time hint where to switch back to the classic
	// design (devices that had a data folder before v0.4.0).
	DesignHint bool            `json:"designHint"`
	View       string          `json:"view"` // default invoice view: "detail", "compact", "table", "classic"
	Table      TableOptionsDTO `json:"table"`
}

// TableOptionsDTO are the options of the table view.
type TableOptionsDTO struct {
	Dense  bool     `json:"dense"`
	Sum    bool     `json:"sum"`
	Hidden []string `json:"hidden"` // column kinds not shown
}

// Themes, designs, invoice views and table column kinds known to this
// version, defaults first (keep in sync with frontend/src/lib/theme.ts,
// design.ts, appearance.ts). Unknown values in the configuration (from a
// newer version) fall back to the default.
var (
	themes       = []string{"", "light", "dark"}
	designs      = []string{"new", "classic"}
	views        = []string{"detail", "compact", "table", "classic"}
	tableColumns = []string{"service", "invoice", "provider", "category", "reference", "amount", "paid",
		"expected", "actual", "diff", "submitted", "refunded", "note"}
)

// known returns v if it is one of values, else the first (the default).
func known(v string, values []string) string {
	if slices.Contains(values, v) {
		return v
	}
	return values[0]
}

// Appearance returns the appearance settings of this device.
func (s *Service) Appearance() AppearanceDTO {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil {
		return appearanceOf(&config.Config{DesignHintSeen: true})
	}
	return appearanceOf(s.cfg)
}

func appearanceOf(c *config.Config) AppearanceDTO {
	a := AppearanceDTO{
		Theme: known(c.Theme, themes), Design: known(c.Design, designs), View: known(c.View, views),
		// Not on the setup screen: a device set up now gets no hint (SetDataDir marks it seen).
		DesignHint: !c.DesignHintSeen && c.DataDir != "",
		Table:      TableOptionsDTO{Dense: c.Table.Dense, Sum: !c.Table.NoSum, Hidden: []string{}},
	}
	for _, h := range c.Table.Hidden {
		if slices.Contains(tableColumns, h) {
			a.Table.Hidden = append(a.Table.Hidden, h)
		}
	}
	return a
}

// SetTheme sets the colour scheme of this device: "", "light" or "dark".
func (s *Service) SetTheme(theme string) error {
	if !slices.Contains(themes, theme) {
		return fmt.Errorf("unbekanntes Farbschema %q", theme)
	}
	return s.updateConfig("Farbschema", func(c *config.Config) { c.Theme = theme })
}

// SetDesign sets the design of this device: "new" or "classic". Choosing a
// design also closes the one-time hint about it.
func (s *Service) SetDesign(design string) error {
	if !slices.Contains(designs, design) {
		return fmt.Errorf("unbekanntes Design %q", design)
	}
	return s.updateConfig("Design", func(c *config.Config) {
		c.Design = design
		c.DesignHintSeen = true
	})
}

// CloseDesignHint marks the one-time hint about the new design as seen.
func (s *Service) CloseDesignHint() error {
	return s.updateConfig("Hinweis", func(c *config.Config) { c.DesignHintSeen = true })
}

// SetView sets the invoice view the list starts with: "detail", "compact",
// "table" or "classic".
func (s *Service) SetView(view string) error {
	if !slices.Contains(views, view) {
		return fmt.Errorf("unbekannte Ansicht %q", view)
	}
	return s.updateConfig("Standardansicht", func(c *config.Config) { c.View = view })
}

// SetTableOptions sets the options of the table view.
func (s *Service) SetTableOptions(t TableOptionsDTO) error {
	var hidden []string
	for _, h := range t.Hidden {
		if !slices.Contains(tableColumns, h) {
			return fmt.Errorf("unbekannte Spalte %q", h)
		}
		if !slices.Contains(hidden, h) {
			hidden = append(hidden, h)
		}
	}
	return s.updateConfig("Tabellenansicht", func(c *config.Config) {
		c.Table = config.TableOptions{Dense: t.Dense, NoSum: !t.Sum, Hidden: hidden}
	})
}

// updateConfig changes the device configuration and saves it; on failure
// the change is undone. what names the setting in the error.
func (s *Service) updateConfig(what string, change func(*config.Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil {
		return errors.New("Konfiguration nicht verfügbar")
	}
	old := s.cfg.Clone()
	change(s.cfg)
	if err := s.cfg.Save(s.cfgDir); err != nil {
		*s.cfg = *old
		return fmt.Errorf("%s konnte nicht gespeichert werden: %w", what, err)
	}
	return nil
}
