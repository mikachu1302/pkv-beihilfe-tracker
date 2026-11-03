package service

import "testing"

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
