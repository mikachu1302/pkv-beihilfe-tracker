package config

import "testing"

func TestLoadCreatesDeviceIDOnce(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(dir)
	if err != nil || len(c.DeviceID) != 6 || c.DataDir != "" {
		t.Fatalf("first load: %+v, %v", c, err)
	}
	c.DataDir = "/irgendwo/daten"
	if err := c.Save(dir); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(dir)
	if err != nil || c2.DeviceID != c.DeviceID || c2.DataDir != "/irgendwo/daten" {
		t.Fatalf("second load: %+v, %v", c2, err)
	}
}
