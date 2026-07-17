package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/config"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/secrets"
)

// ConnectionInfo is a saved connection as shown to the frontend — its URI
// is always redacted, since the real one may contain credentials.
type ConnectionInfo struct {
	Name             string      `json:"name"`
	RedactedURI      string      `json:"redactedUri"`
	Engine           string      `json:"engine"`
	Capabilities     engine.Caps `json:"capabilities"`
	Environment      string      `json:"environment"`
	ReadOnly         bool        `json:"readOnly"`
	TenantSessionVar string      `json:"tenantSessionVar"`
	TenantValue      string      `json:"tenantValue"`
	CreatedAt        string      `json:"createdAt"`
}

// EngineIDs returns every database engine this build supports.
func (a *App) EngineIDs() []string {
	return engine.IDs()
}

// SecureCredentialStorageAvailable lets the UI warn when connection
// passwords must fall back to the owner-only config file.
func (a *App) SecureCredentialStorageAvailable() bool {
	return secrets.Available()
}

// ListConnections returns every saved connection.
func (a *App) ListConnections() ([]ConnectionInfo, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	out := make([]ConnectionInfo, len(cfg.Connections))
	for i, c := range cfg.Connections {
		var caps engine.Caps
		if eng, err := engine.Lookup(c.EngineID()); err == nil {
			caps = eng.Capabilities()
		}
		out[i] = ConnectionInfo{
			Name:             c.Name,
			RedactedURI:      redactURI(c.URI),
			Engine:           c.EngineID(),
			Capabilities:     caps,
			Environment:      c.Environment,
			ReadOnly:         c.ReadOnly,
			TenantSessionVar: c.TenantSessionVar,
			TenantValue:      c.TenantValue,
			CreatedAt:        c.CreatedAt,
		}
	}
	return out, nil
}

// ConnectionInput is the payload for AddConnection. SSHHost being non-empty
// enables an SSH tunnel; SSHPrivateKey (a PEM-encoded key) takes precedence
// over SSHPassword if both are set.
type ConnectionInput struct {
	Name             string `json:"name"`
	URI              string `json:"uri"`
	Engine           string `json:"engine"`
	Environment      string `json:"environment"`
	ReadOnly         bool   `json:"readOnly"`
	SSHHost          string `json:"sshHost"`
	SSHUser          string `json:"sshUser"`
	SSHPassword      string `json:"sshPassword"`
	SSHPrivateKey    string `json:"sshPrivateKey"`
	TenantSessionVar string `json:"tenantSessionVar"`
}

// AddConnection saves a new connection (or replaces one with the same
// name). input.Engine may be empty for the default (mongodb).
func (a *App) AddConnection(input ConnectionInput) error {
	if input.Name == "" || input.URI == "" {
		return fmt.Errorf("both a name and a URI are required")
	}
	engineID := input.Engine
	if engineID == "" {
		engineID = "mongodb"
	}
	if _, err := engine.Lookup(engineID); err != nil {
		return err
	}
	switch input.Environment {
	case "", "dev", "staging", "prod":
	default:
		return fmt.Errorf("invalid environment %q (use dev, staging, or prod)", input.Environment)
	}
	if input.SSHHost != "" && input.SSHPassword == "" && input.SSHPrivateKey == "" {
		return fmt.Errorf("an SSH tunnel needs a password or private key")
	}
	if input.TenantSessionVar != "" && !engine.ValidSessionVarName(input.TenantSessionVar) {
		return fmt.Errorf("invalid tenant session variable name %q", input.TenantSessionVar)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	// Preserve any tenant value already set for an existing connection of
	// the same name — enabling/renaming tenant mode here shouldn't reset
	// whichever tenant SwitchTenant last selected.
	tenantValue := ""
	if existing, ok := cfg.Find(input.Name); ok {
		tenantValue = existing.TenantValue
	}
	cfg.Upsert(config.Connection{
		Name:             input.Name,
		URI:              input.URI,
		Engine:           engineID,
		Environment:      input.Environment,
		ReadOnly:         input.ReadOnly,
		SSHHost:          input.SSHHost,
		SSHUser:          input.SSHUser,
		SSHPassword:      input.SSHPassword,
		SSHPrivateKey:    input.SSHPrivateKey,
		TenantSessionVar: input.TenantSessionVar,
		TenantValue:      tenantValue,
		CreatedAt:        time.Now().Format(time.RFC3339),
	})
	if err := config.Save(cfg); err != nil {
		return err
	}
	// A replaced profile may have a cached session against the old URI.
	a.engines.Invalidate(input.Name)
	return nil
}

// PickSQLiteFile opens a native file picker for choosing a SQLite database
// file, returning "" if the user cancels.
func (a *App) PickSQLiteFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose a SQLite database file",
		Filters: []runtime.FileFilter{
			{DisplayName: "SQLite databases (*.db, *.sqlite, *.sqlite3)", Pattern: "*.db;*.sqlite;*.sqlite3"},
			{DisplayName: "All files", Pattern: "*.*"},
		},
	})
}

// RemoveConnection deletes a saved connection, its cached session, and its
// keychain entry.
func (a *App) RemoveConnection(name string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if conn, ok := cfg.Find(name); ok {
		config.DeleteCredential(*conn)
	}
	if !cfg.Remove(name) {
		return fmt.Errorf("no connection named %q", name)
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	a.engines.Invalidate(name)
	return nil
}

// SwitchTenant changes a connection's current tenant value. Requires the
// connection to already have TenantSessionVar configured (set it via
// AddConnection). The cached session is invalidated so the next query
// reconnects and re-runs SET/set_config under the new tenant, rather than
// mutating a pooled connection's session state in place.
func (a *App) SwitchTenant(connectionName, tenantValue string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	conn, ok := cfg.Find(connectionName)
	if !ok {
		return fmt.Errorf("no connection named %q", connectionName)
	}
	if conn.TenantSessionVar == "" {
		return fmt.Errorf("connection %q isn't configured for multi-tenant mode (no tenant session variable set)", connectionName)
	}
	conn.TenantValue = tenantValue
	cfg.Upsert(*conn)
	if err := config.Save(cfg); err != nil {
		return err
	}
	a.engines.Invalidate(connectionName)
	return nil
}

// TestConnection pings a saved connection and returns its database names.
func (a *App) TestConnection(name string) ([]string, error) {
	sess, release, err := a.engines.Acquire(context.Background(), name)
	if err != nil {
		return nil, err
	}
	defer release()
	if err := sess.Ping(context.Background()); err != nil {
		// Don't keep serving a session that just failed its health check.
		a.engines.Invalidate(name)
		return nil, err
	}
	return sess.ListDatabases(context.Background())
}

// requireWritable returns engine.ErrReadOnly if the named connection is
// flagged Safe Mode / read-only. Every mutating binding (document CRUD,
// index/namespace changes, SQL Execute) must call this before dispatching
// to a session, so Safe Mode is enforced in Go — independent of whatever
// the frontend does or doesn't disable.
func (a *App) requireWritable(name string) error {
	conn, err := a.resolveConn(name)
	if err != nil {
		return err
	}
	return engine.RequireWritable(engine.ConnConfig{ReadOnly: conn.ReadOnly})
}

func (a *App) resolveConn(name string) (*config.Connection, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	conn, ok := cfg.Find(name)
	if !ok {
		return nil, fmt.Errorf("no connection named %q", name)
	}
	return conn, nil
}
