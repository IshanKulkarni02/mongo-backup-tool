package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/engine/tunnel"
)

var (
	snapConn string
	snapDB   string
)

var snapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Git-like version control for a database: snapshot, log, diff, restore, tag, gc",
}

// resolveConn looks up a saved connection or returns an error with the
// standard "see: dbhelm connection list" hint.
func resolveConn(name string) (*config.Connection, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	conn, ok := cfg.Find(name)
	if !ok {
		return nil, fmt.Errorf("no connection named %q (see: dbhelm connection list)", name)
	}
	return conn, nil
}

// connConfigFor builds the engine.ConnConfig for a saved connection,
// including its SSH tunnel settings if any are set — the one place this
// mapping happens for CLI-side session opening, so openSQLSession and
// openEngineSession can't drift out of sync with each other.
func connConfigFor(conn *config.Connection) engine.ConnConfig {
	connCfg := engine.ConnConfig{
		Name: conn.Name, URI: conn.URI, ReadOnly: conn.ReadOnly,
		TenantSessionVar: conn.TenantSessionVar, TenantValue: conn.TenantValue,
	}
	if conn.SSHHost != "" {
		connCfg.SSHTunnel = &tunnel.Config{
			Host:          conn.SSHHost,
			User:          conn.SSHUser,
			Password:      conn.SSHPassword,
			PrivateKeyPEM: conn.SSHPrivateKey,
		}
	}
	return connCfg
}

// openEngineSession opens a one-shot engine.Session for a saved
// connection, regardless of which surface its engine additionally
// implements (SQL, documents) — used where only the engine-agnostic
// Session methods (Ping, ListDatabases) are needed, e.g. `connection
// test`. The CLI has no long-lived session cache (unlike desktop's
// engine.Manager), so callers must invoke the returned release func when
// done.
func openEngineSession(conn *config.Connection) (engine.Session, func(), error) {
	eng, err := engine.Lookup(conn.EngineID())
	if err != nil {
		return nil, nil, err
	}
	sess, err := eng.Open(context.Background(), connConfigFor(conn))
	if err != nil {
		return nil, nil, err
	}
	return sess, func() { sess.Close(context.Background()) }, nil
}

// openSQLSession opens a one-shot engine.SQLSession for a saved connection —
// the CLI has no long-lived session cache (unlike desktop's engine.Manager),
// so callers must invoke the returned release func when done.
func openSQLSession(conn *config.Connection) (engine.SQLSession, func(), error) {
	eng, err := engine.Lookup(conn.EngineID())
	if err != nil {
		return nil, nil, err
	}
	sess, err := eng.Open(context.Background(), connConfigFor(conn))
	if err != nil {
		return nil, nil, err
	}
	ss, ok := sess.(engine.SQLSession)
	if !ok {
		sess.Close(context.Background())
		return nil, nil, fmt.Errorf("connection %q isn't a SQL database", conn.Name)
	}
	return ss, func() { ss.Close(context.Background()) }, nil
}

func init() {
	snapshotCmd.PersistentFlags().StringVar(&snapConn, "connection", "", "Saved connection name (required)")
	snapshotCmd.PersistentFlags().StringVar(&snapDB, "db", "", "Database name (required)")
	rootCmd.AddCommand(snapshotCmd)
}

func requireConnAndDB() error {
	if snapConn == "" || snapDB == "" {
		return fmt.Errorf("--connection and --db are required")
	}
	return nil
}
