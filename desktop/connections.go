package main

import (
	"fmt"
	"time"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/config"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/mongotools"
)

// ConnectionInfo is a saved connection as shown to the frontend — its URI
// is always redacted, since the real one may contain credentials.
type ConnectionInfo struct {
	Name        string `json:"name"`
	RedactedURI string `json:"redactedUri"`
	CreatedAt   string `json:"createdAt"`
}

// ListConnections returns every saved connection.
func (a *App) ListConnections() ([]ConnectionInfo, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	out := make([]ConnectionInfo, len(cfg.Connections))
	for i, c := range cfg.Connections {
		out[i] = ConnectionInfo{Name: c.Name, RedactedURI: redactURI(c.URI), CreatedAt: c.CreatedAt}
	}
	return out, nil
}

// AddConnection saves a new connection (or replaces one with the same name).
func (a *App) AddConnection(name, uri string) error {
	if name == "" || uri == "" {
		return fmt.Errorf("both a name and a URI are required")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.Upsert(config.Connection{Name: name, URI: uri, CreatedAt: time.Now().Format(time.RFC3339)})
	return config.Save(cfg)
}

// RemoveConnection deletes a saved connection.
func (a *App) RemoveConnection(name string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !cfg.Remove(name) {
		return fmt.Errorf("no connection named %q", name)
	}
	return config.Save(cfg)
}

// TestConnection pings a saved connection and returns its database names.
func (a *App) TestConnection(name string) ([]string, error) {
	conn, err := a.resolveConn(name)
	if err != nil {
		return nil, err
	}
	return mongotools.TestConnection(conn.URI)
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
