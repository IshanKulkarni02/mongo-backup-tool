// Package config manages mongobak's persistent settings: saved connection
// profiles and the on-disk location where backup archives live.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Connection is a saved MongoDB connection profile (local or Atlas).
type Connection struct {
	Name      string `json:"name"`
	URI       string `json:"uri"`
	CreatedAt string `json:"createdAt"`
}

// Config is the root of the persisted settings file.
type Config struct {
	Connections []Connection `json:"connections"`
}

// Dir returns mongobak's per-user config directory, creating it if needed.
// It resolves to the OS-appropriate location (via os.UserConfigDir), e.g.
// ~/Library/Application Support/mongobak on macOS, %AppData%\mongobak on
// Windows, and ~/.config/mongobak on Linux.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("resolving config directory: %w", err)
		}
		base = home
	}
	dir := filepath.Join(base, "mongobak")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating config directory %s: %w", dir, err)
	}
	return dir, nil
}

// BackupsDir returns the directory where backup archives and the backup
// index are stored, creating it if needed.
func BackupsDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	b := filepath.Join(dir, "backups")
	if err := os.MkdirAll(b, 0o755); err != nil {
		return "", fmt.Errorf("creating backups directory %s: %w", b, err)
	}
	return b, nil
}

func filePath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads the config file, returning an empty Config if it doesn't exist yet.
func Load() (*Config, error) {
	path, err := filePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	return &cfg, nil
}

// Save writes the config file. Connection URIs may contain credentials, so
// the file is written with owner-only permissions.
func Save(cfg *Config) error {
	path, err := filePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Find looks up a saved connection by name.
func (c *Config) Find(name string) (*Connection, bool) {
	for i := range c.Connections {
		if c.Connections[i].Name == name {
			return &c.Connections[i], true
		}
	}
	return nil, false
}

// Upsert adds a connection or replaces the existing one with the same name.
func (c *Config) Upsert(conn Connection) {
	for i := range c.Connections {
		if c.Connections[i].Name == conn.Name {
			c.Connections[i] = conn
			return
		}
	}
	c.Connections = append(c.Connections, conn)
}

// Remove deletes a connection by name, reporting whether it existed.
func (c *Config) Remove(name string) bool {
	for i := range c.Connections {
		if c.Connections[i].Name == name {
			c.Connections = append(c.Connections[:i], c.Connections[i+1:]...)
			return true
		}
	}
	return false
}
