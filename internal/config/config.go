// Package config keeps the device-local settings: where the data folder is
// and this device's ID. They live in the OS config folder, never in the data
// folder (which may be shared by several devices).
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

const fileName = "config.json"

// Config is the device-local configuration.
type Config struct {
	DataDir  string `json:"dataDir"`
	DeviceID string `json:"deviceId"`
	// BackupDir receives zip snapshots of the data folder (device-local:
	// each device may keep its own backups).
	BackupDir string `json:"backupDir,omitempty"`
	// Theme is the colour scheme of this device: "" follows the system,
	// "light" or "dark".
	Theme string `json:"theme,omitempty"`
	// Design is the look of this device: "" (the default, the new design),
	// "new" or "classic".
	Design string `json:"design,omitempty"`
	// DesignHintSeen: the one-time hint where to switch back to the classic
	// design was closed (or the device was set up with v0.4.0 or later).
	DesignHintSeen bool `json:"designHintSeen,omitempty"`
	// View is the invoice view the list starts with in the new design: ""
	// (Detail), "compact", "table" or "classic".
	View string `json:"view,omitempty"`
	// Table holds the options of the table view.
	Table TableOptions `json:"table,omitzero"`
}

// TableOptions are the options of the table view. The zero value is the
// default: normal rows, sum row shown, all columns.
type TableOptions struct {
	Dense  bool     `json:"dense,omitempty"`
	NoSum  bool     `json:"noSum,omitempty"`
	Hidden []string `json:"hidden,omitempty"` // column keys not shown
}

// Dir returns the folder for the device-local configuration.
func Dir() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "pkv-beihilfe-tracker"), nil
}

// Load reads the configuration from dir. A missing file yields an empty
// configuration; a device ID is created on first use and saved.
func Load(dir string) (*Config, error) {
	c := &Config{}
	b, err := os.ReadFile(filepath.Join(dir, fileName))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, c); err != nil {
			return nil, err
		}
	}
	if c.DeviceID == "" {
		var r [3]byte
		_, _ = rand.Read(r[:])
		c.DeviceID = hex.EncodeToString(r[:])
		if err := c.Save(dir); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// Save writes the configuration to dir.
func (c *Config) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, fileName+".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, fileName))
}
