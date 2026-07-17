// Package config manages mongobak's persistent settings: saved connection
// profiles and the on-disk location where backup archives live.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Connection is a saved database connection profile.
type Connection struct {
	Name string `json:"name"`
	// URI is the full connection string in memory. On disk, when a system
	// keyring is available, the password is held in the keyring (see
	// CredentialRef) and the stored URI is credential-stripped; Load
	// re-injects it transparently.
	URI string `json:"uri"`
	// Engine identifies the database technology ("mongodb", "postgres",
	// "mysql", "sqlite"). Empty means "mongodb" — profiles saved before
	// multi-engine support have no engine field.
	Engine string `json:"engine,omitempty"`
	// Environment tags the connection ("dev", "staging", "prod") so UIs
	// can color-code it and treat production with extra caution.
	Environment string `json:"environment,omitempty"`
	// ReadOnly marks a connection whose sessions must refuse writes.
	ReadOnly bool `json:"readOnly,omitempty"`
	// CredentialRef is the secrets-store key holding this connection's
	// password, when it has been moved out of the URI.
	CredentialRef string `json:"credentialRef,omitempty"`

	// SSH tunnel settings, for reaching a database that isn't directly
	// network-reachable. SSHHost being non-empty enables the tunnel.
	// SSHPassword/SSHPrivateKey are the in-memory, resolved values (see
	// resolveCredentials); on disk they're held in the keychain when
	// available (SSHPasswordRef/SSHPrivateKeyRef), same as the URI
	// password.
	SSHHost          string `json:"sshHost,omitempty"`
	SSHUser          string `json:"sshUser,omitempty"`
	SSHPassword      string `json:"sshPassword,omitempty"`
	SSHPrivateKey    string `json:"sshPrivateKey,omitempty"`
	SSHPasswordRef   string `json:"sshPasswordRef,omitempty"`
	SSHPrivateKeyRef string `json:"sshPrivateKeyRef,omitempty"`

	// TenantSessionVar names the session variable a query's row-level
	// security policy checks (e.g. Postgres' "app.current_tenant"). Set,
	// it enables multi-tenant mode: every session for this connection runs
	// a SET statement for it on connect, using TenantValue. Switching
	// tenants recreates the pooled session (see engine.Manager.Invalidate)
	// rather than issuing a per-statement SET — safer, since a
	// per-statement SET on a pooled connection can leak across requests
	// that reuse it.
	TenantSessionVar string `json:"tenantSessionVar,omitempty"`
	TenantValue      string `json:"tenantValue,omitempty"`

	CreatedAt string `json:"createdAt"`
}

// EngineID returns the connection's engine, defaulting legacy profiles
// (saved before multi-engine support) to "mongodb".
func (c *Connection) EngineID() string {
	if c.Engine == "" {
		return "mongodb"
	}
	return c.Engine
}

// Config is the root of the persisted settings file.
type Config struct {
	Connections []Connection `json:"connections"`
	AI          AISettings   `json:"ai,omitempty"`
}

// Dir returns mongobak's per-user config directory, creating it if needed.
// It resolves to the OS-appropriate location (via os.UserConfigDir), e.g.
// ~/Library/Application Support/mongobak on macOS, %AppData%\mongobak on
// Windows, and ~/.config/mongobak on Linux.
func Dir() (string, error) {
	if override := os.Getenv("MONGOBAK_CONFIG_DIR"); override != "" {
		if err := os.MkdirAll(override, 0o755); err != nil {
			return "", fmt.Errorf("creating config directory %s: %w", override, err)
		}
		return override, nil
	}
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
	resolveCredentials(&cfg)
	return &cfg, nil
}

// Save writes the config file. When a system keyring is available,
// passwords are moved into it and the file keeps credential-stripped URIs;
// otherwise full URIs are stored, protected only by the file's owner-only
// permissions.
func Save(cfg *Config) error {
	path, err := filePath()
	if err != nil {
		return err
	}
	persisted := stripCredentials(cfg)
	data, err := json.MarshalIndent(persisted, "", "  ")
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
