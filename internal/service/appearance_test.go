package service

import (
	"slices"
	"testing"
)

func TestTheme(t *testing.T) {
	s := newTestService(t)
	if got := s.Appearance().Theme; got != "" {
		t.Errorf("default theme %q", got)
	}
	if err := s.SetTheme("dark"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTheme("lila"); err == nil {
		t.Error("unknown theme accepted")
	}
	// Kept in the device configuration: a new start reads it again.
	again := NewWithConfigDir(s.cfgDir)
	t.Cleanup(func() { _ = again.ServiceShutdown() })
	if got := again.Appearance().Theme; got != "dark" {
		t.Errorf("after restart %q", got)
	}
}

func TestDesignAndViews(t *testing.T) {
	s := newTestService(t)
	a := s.Appearance()
	if a.Design != "new" || a.View != "detail" || !a.Table.Sum || a.Table.Dense || len(a.Table.Hidden) != 0 {
		t.Errorf("defaults %+v", a)
	}
	// newTestService sets the data folder on a fresh device: no hint about the old design.
	if a.DesignHint {
		t.Error("hint on a freshly set up device")
	}

	if err := s.SetDesign("classic"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDesign("retro"); err == nil {
		t.Error("unknown design accepted")
	}
	if err := s.SetView("table"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetView("kacheln"); err == nil {
		t.Error("unknown view accepted")
	}
	if err := s.SetTableOptions(TableOptionsDTO{Dense: true, Sum: false, Hidden: []string{"note", "note", "reference"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTableOptions(TableOptionsDTO{Hidden: []string{"farbe"}}); err == nil {
		t.Error("unknown column accepted")
	}

	again := NewWithConfigDir(s.cfgDir)
	t.Cleanup(func() { _ = again.ServiceShutdown() })
	a = again.Appearance()
	if a.Design != "classic" || a.View != "table" || a.Table.Sum || !a.Table.Dense || !slices.Equal(a.Table.Hidden, []string{"note", "reference"}) {
		t.Errorf("after restart %+v", a)
	}
}

func TestDesignHintForExistingDevices(t *testing.T) {
	s := newTestService(t)
	// A device that had its data folder before v0.4.0: the flag is not set.
	s.cfg.DesignHintSeen = false
	if !s.Appearance().DesignHint {
		t.Fatal("no hint for an existing device")
	}
	if err := s.CloseDesignHint(); err != nil {
		t.Fatal(err)
	}
	if s.Appearance().DesignHint {
		t.Error("hint still shown after closing it")
	}
	s.cfg.DesignHintSeen = false
	if err := s.SetDesign("new"); err != nil {
		t.Fatal(err)
	}
	if s.Appearance().DesignHint {
		t.Error("hint still shown after choosing a design")
	}
}
