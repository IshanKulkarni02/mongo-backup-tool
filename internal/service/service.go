// Package service holds the connection-resolution and safety-gate logic
// that every DBHelm surface shares — the CLI, the TUI, the desktop app and
// the agent broker — so a rule like "a read-only connection refuses writes"
// or "a writable CTE is not a read" is enforced by one implementation
// instead of one copy per surface.
package service

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/engine/safeguard"
	"github.com/IshanKulkarni02/dbhelm/internal/engine/tunnel"
)

// ConnConfigFor builds the engine.ConnConfig for a saved connection,
// including its SSH tunnel settings if any are set. It is the single place
// this mapping happens, so no surface can drift out of sync with another.
func ConnConfigFor(conn *config.Connection) (engine.ConnConfig, error) {
	connCfg := engine.ConnConfig{
		Name: conn.Name, URI: conn.URI, ReadOnly: conn.ReadOnly,
		TenantSessionVar: conn.TenantSessionVar, TenantValue: conn.TenantValue,
	}
	if conn.SSHHost != "" {
		knownHosts, err := config.SSHKnownHostsPath()
		if err != nil {
			return engine.ConnConfig{}, err
		}
		connCfg.SSHTunnel = &tunnel.Config{
			Host:           conn.SSHHost,
			User:           conn.SSHUser,
			Password:       conn.SSHPassword,
			PrivateKeyPEM:  conn.SSHPrivateKey,
			KnownHostsPath: knownHosts,
		}
	}
	return connCfg, nil
}

// ResolveConn looks up a saved connection by name.
func ResolveConn(name string) (*config.Connection, error) {
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

// ResolveEngineConn maps a saved connection name to its engine and config.
// Its signature is engine.ResolveFunc, so it plugs straight into
// engine.NewManager.
func ResolveEngineConn(name string) (engine.ConnConfig, engine.Engine, error) {
	conn, err := ResolveConn(name)
	if err != nil {
		return engine.ConnConfig{}, nil, err
	}
	eng, err := engine.Lookup(conn.EngineID())
	if err != nil {
		return engine.ConnConfig{}, nil, err
	}
	connCfg, err := ConnConfigFor(conn)
	if err != nil {
		return engine.ConnConfig{}, nil, err
	}
	return connCfg, eng, nil
}

// RequireWritable returns engine.ErrReadOnly if the named connection is
// flagged Safe Mode / read-only. Every mutating operation must call this
// before dispatching to a session, so Safe Mode is enforced in Go,
// independent of whatever a frontend does or doesn't disable.
func RequireWritable(name string) error {
	conn, err := ResolveConn(name)
	if err != nil {
		return err
	}
	return engine.RequireWritable(engine.ConnConfig{ReadOnly: conn.ReadOnly})
}

// SQLSessionFrom acquires the cached SQL session for a connection from m.
// The caller must invoke the returned release func when done.
func SQLSessionFrom(ctx context.Context, m *engine.Manager, connectionName string) (engine.SQLSession, func(), error) {
	sess, release, err := m.Acquire(ctx, connectionName)
	if err != nil {
		return nil, nil, err
	}
	ss, ok := sess.(engine.SQLSession)
	if !ok {
		release()
		return nil, nil, fmt.Errorf("connection %q isn't a SQL database", connectionName)
	}
	return ss, release, nil
}

// CheckQueryStatement gates a statement arriving through a "query" RPC —
// a path meant only for reads, but which reaches the database driver
// directly with no write check of its own. A writable CTE (e.g. "WITH x AS
// (DELETE ...) SELECT * FROM x") looks like a read to a client's routing
// but isn't one, so read-only enforcement must not depend on which RPC the
// client happened to use. Statements that really are reads (per
// safeguard.IsRead) skip this entirely — a read-only connection must still
// be able to read.
func CheckQueryStatement(sqlText string, requireWritable func() error) error {
	if safeguard.IsRead(sqlText) {
		return nil
	}
	if err := requireWritable(); err != nil {
		return err
	}
	if class := safeguard.Classify(sqlText); class.Risk == safeguard.RiskDangerous {
		return fmt.Errorf("dangerous statement (%s) — run it via Execute instead, with confirmation", class.Reason)
	}
	return nil
}

// WritesData parses pipelineJSON the same way the mongodb engine's
// Aggregate does (a JSON array of stage documents, via
// bson.UnmarshalExtJSON) and reports whether any stage's actual top-level
// key is $out or $merge — the only aggregation stages that persist data.
// Checking the parsed pipeline rather than substring-matching the raw JSON
// text closes two gaps a text-based check can't handle: a stage key written
// with a unicode escape (which UnmarshalExtJSON resolves to the real key),
// and $out/$merge appearing only as an ordinary data value. An unparsable
// pipeline is treated as not a write here — Aggregate itself rejects it
// with a clear parse error right after.
func WritesData(pipelineJSON string) bool {
	var stages []bson.D
	if err := bson.UnmarshalExtJSON([]byte(pipelineJSON), true, &stages); err != nil {
		return false
	}
	for _, stage := range stages {
		for _, elem := range stage {
			if elem.Key == "$out" || elem.Key == "$merge" {
				return true
			}
		}
	}
	return false
}

// DatabaseNames returns what a database/schema picker should offer for an
// already-open session: an engine.SchemaLister's ListSchemas if the engine
// implements it (Postgres, whose "database" parameter actually means schema
// within the DSN's fixed database), otherwise the base ListDatabases.
func DatabaseNames(ctx context.Context, sess engine.Session) ([]string, error) {
	if sl, ok := sess.(engine.SchemaLister); ok {
		return sl.ListSchemas(ctx)
	}
	return sess.ListDatabases(ctx)
}
