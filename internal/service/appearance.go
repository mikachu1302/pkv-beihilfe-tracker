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
	// DesignHint: show the one-time hint where to switch back to the classic design.
	DesignHint bool            `json:"designHint"`
	View       string          `json:"view"` // default invoice view: "detail", "compact", "table", "classic"
	Table      TableOptionsDTO `json:"table"`
}

// TableOptionsDTO are the options of the table view.
type TableOptionsDTO struct {
	Dense  bool     `json:"dense"`
	Sum    bool     `json:"sum"`
	Hidden []string `json:"hidden"` // column keys not shown
}

// Designs, invoice views and table columns known to this version. Unknown
// values in the configuration (from a newer version) fall back to the default.
var (
	designs     = []string{"new", "classic"}
	views       = []string{"detail", "compact", "table", "classic"}
	tableColumn = []string{"service", "invoice", "provider", "category", "reference", "amount", "paid",
		"expected", "actual", "diff", "submitted", "refunded", "note"}
)

// Appearance returns the appearance settings of this device.
func (s *Service) Appearance() AppearanceDTO {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil {
		return AppearanceDTO{Design: "new", View: "detail", Table: TableOptionsDTO{Sum: true, Hidden: []string{}}}
	}
	return appearanceOf(s.cfg)
}

func appearanceOf(c *config.Config) AppearanceDTO {
	a := AppearanceDTO{Theme: c.Theme, Design: c.Design, View: c.View, DesignHint: !c.DesignHintSeen,
		Table: TableOptionsDTO{Dense: c.Table.Dense, Sum: !c.Table.NoSum, Hidden: []string{}}}
	if !slices.Contains(designs, a.Design) {
		a.Design = "new"
	}
	if !slices.Contains(views, a.View) {
		a.View = "detail"
	}
	for _, h := range c.Table.Hidden {
		if slices.Contains(tableColumn, h) {
			a.Table.Hidden = append(a.Table.Hidden, h)
		}
	}
	return a
}

// SetTheme sets the colour scheme of this device: "", "light" or "dark".
func (s *Service) SetTheme(theme string) error {
	switch theme {
	case "", "light", "dark":
	default:
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
	return s.updateConfig("Standardansicht", func(c *config.Config) {
		c.View = view
		if view == "detail" {
			c.View = "" // the default
		}
	})
}

// SetTableOptions sets the options of the table view.
func (s *Service) SetTableOptions(t TableOptionsDTO) error {
	var hidden []string
	for _, h := range t.Hidden {
		if !slices.Contains(tableColumn, h) {
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
	old := *s.cfg
	old.Table.Hidden = slices.Clone(s.cfg.Table.Hidden)
	change(s.cfg)
	if err := s.cfg.Save(s.cfgDir); err != nil {
		*s.cfg = old
		return fmt.Errorf("%s konnte nicht gespeichert werden: %w", what, err)
	}
	return nil
}
