package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/secrets"

	// Blank-imported so their init() registers each engine with the
	// registry engine.Lookup below resolves --engine against — same
	// registration desktop/app.go does for the GUI.
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/mongodb"
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/mysql"
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/postgres"
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/sqlite"
)

var connectionCmd = &cobra.Command{
	Use:   "connection",
	Short: "Manage saved database connections (MongoDB, Postgres, MySQL, SQLite)",
}

var (
	connAddURI              string
	connAddEngine           string
	connAddEnvironment      string
	connAddReadOnly         bool
	connAddSSHHost          string
	connAddSSHUser          string
	connAddSSHPassword      string
	connAddSSHKey           string
	connAddTenantSessionVar string
)

var connectionAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Save a connection under a name",
	Args:  cobra.ExactArgs(1),
	Example: `  dbhelm connection add local --uri "mongodb://localhost:27017"
  dbhelm connection add prod  --uri "mongodb+srv://user:pass@cluster0.mongodb.net"
  dbhelm connection add pg-dev --engine postgres --uri "postgres://user:pass@localhost:5432/app"
  dbhelm connection add pg-prod --engine postgres --uri "postgres://user@bastion-internal:5432/app" \
    --ssh-host bastion.example.com --ssh-user ops --ssh-key ~/.ssh/id_ed25519`,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if connAddURI == "" {
			return fmt.Errorf("--uri is required, e.g. mongodb://localhost:27017 or mongodb+srv://user:pass@cluster.mongodb.net")
		}
		engineID := connAddEngine
		if engineID == "" {
			engineID = "mongodb"
		}
		if _, err := engine.Lookup(engineID); err != nil {
			return err
		}
		switch connAddEnvironment {
		case "", "dev", "staging", "prod":
		default:
			return fmt.Errorf("invalid --environment %q (use dev, staging, or prod)", connAddEnvironment)
		}
		if connAddSSHHost != "" && connAddSSHPassword == "" && connAddSSHKey == "" {
			return fmt.Errorf("--ssh-host needs --ssh-password or --ssh-key")
		}
		// --ssh-key takes a path to a PEM-encoded private key file (the
		// common case for CLI use), or literal PEM content directly (for
		// scripted/embedded use). looksLikeSSHKeyPath tells the two apart:
		// literal PEM content always spans multiple lines or contains a
		// "-----BEGIN" marker, neither of which is valid in a filesystem
		// path. Only a value that looks like a path is actually read as
		// one — and if that read fails (typo, permissions, a directory),
		// the error is surfaced instead of silently falling back to
		// treating the path string itself as the key's PEM content, which
		// would save an unusable key with no indication anything went
		// wrong until the SSH tunnel later fails to parse it.
		sshPrivateKey := connAddSSHKey
		if connAddSSHKey != "" && looksLikeSSHKeyPath(connAddSSHKey) {
			data, err := os.ReadFile(connAddSSHKey)
			if err != nil {
				return fmt.Errorf("failed to read --ssh-key file %q: %w", connAddSSHKey, err)
			}
			sshPrivateKey = string(data)
		}
		if connAddTenantSessionVar != "" && !engine.ValidSessionVarName(connAddTenantSessionVar) {
			return fmt.Errorf("invalid --tenant-session-var %q", connAddTenantSessionVar)
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		// Preserve any tenant value already set for an existing connection of
		// the same name — enabling/renaming tenant mode here shouldn't reset
		// whichever tenant was last selected (see desktop's SwitchTenant).
		tenantValue := ""
		if existing, ok := cfg.Find(name); ok {
			tenantValue = existing.TenantValue
		}
		cfg.Upsert(config.Connection{
			Name:             name,
			URI:              connAddURI,
			Engine:           engineID,
			Environment:      connAddEnvironment,
			ReadOnly:         connAddReadOnly,
			SSHHost:          connAddSSHHost,
			SSHUser:          connAddSSHUser,
			SSHPassword:      connAddSSHPassword,
			SSHPrivateKey:    sshPrivateKey,
			TenantSessionVar: connAddTenantSessionVar,
			TenantValue:      tenantValue,
			CreatedAt:        time.Now().Format(time.RFC3339),
		})
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Printf("Saved connection %q\n", name)
		if !secrets.Available() {
			fmt.Fprintln(os.Stderr, "Warning:", secrets.UnavailableWarning)
		}
		return nil
	},
}

// looksLikeSSHKeyPath reports whether raw is a filesystem path rather
// than literal PEM key content. PEM content always spans multiple lines
// or contains a "-----BEGIN" marker, neither of which is valid in a path
// on any platform this tool supports, so their presence is a reliable
// signal that raw wasn't meant to be read as a file.
func looksLikeSSHKeyPath(raw string) bool {
	return !strings.Contains(raw, "\n") && !strings.Contains(raw, "-----BEGIN")
}

var connectionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved connections",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if len(cfg.Connections) == 0 {
			fmt.Println("No connections saved. Add one with: dbhelm connection add <name> --uri <uri>")
			return nil
		}
		for _, c := range cfg.Connections {
			fmt.Printf("%-20s %-10s %s\n", c.Name, c.EngineID(), config.RedactURI(c.URI))
		}
		if !secrets.Available() {
			fmt.Fprintln(os.Stderr, "\nWarning:", secrets.UnavailableWarning)
		}
		return nil
	},
}

var connectionRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a saved connection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if conn, ok := cfg.Find(args[0]); ok {
			config.DeleteCredential(*conn)
		}
		if !cfg.Remove(args[0]) {
			return fmt.Errorf("no connection named %q", args[0])
		}
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Printf("Removed connection %q\n", args[0])
		return nil
	},
}

var connectionTestCmd = &cobra.Command{
	Use:   "test <name>",
	Short: "Test a saved connection and list its databases",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		conn, ok := cfg.Find(args[0])
		if !ok {
			return fmt.Errorf("no connection named %q", args[0])
		}
		// Engine-agnostic: every engine.Session implements Ping and
		// ListDatabases, so this works the same for MongoDB, Postgres,
		// MySQL, and SQLite instead of unconditionally dialing with the
		// Mongo driver (which rejects any non-mongodb:// URI outright).
		sess, release, err := openEngineSession(conn)
		if err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}
		defer release()
		if err := sess.Ping(context.Background()); err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}
		dbs, err := sess.ListDatabases(context.Background())
		if err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}
		fmt.Println("Connected. Databases:")
		if len(dbs) == 0 {
			fmt.Println("  (none)")
		}
		for _, d := range dbs {
			fmt.Println(" -", d)
		}
		return nil
	},
}

func init() {
	connectionAddCmd.Flags().StringVar(&connAddURI, "uri", "", "Database connection URI")
	connectionAddCmd.Flags().StringVar(&connAddEngine, "engine", "", "Database engine: mongodb (default), postgres, mysql, or sqlite")
	connectionAddCmd.Flags().StringVar(&connAddEnvironment, "environment", "", "Tag the connection: dev, staging, or prod")
	connectionAddCmd.Flags().BoolVar(&connAddReadOnly, "readonly", false, "Refuse writes on this connection")
	connectionAddCmd.Flags().StringVar(&connAddSSHHost, "ssh-host", "", "SSH tunnel host, if the database isn't directly reachable")
	connectionAddCmd.Flags().StringVar(&connAddSSHUser, "ssh-user", "", "SSH tunnel username")
	connectionAddCmd.Flags().StringVar(&connAddSSHPassword, "ssh-password", "", "SSH tunnel password")
	connectionAddCmd.Flags().StringVar(&connAddSSHKey, "ssh-key", "", "SSH tunnel private key (PEM-encoded, or a path to one)")
	connectionAddCmd.Flags().StringVar(&connAddTenantSessionVar, "tenant-session-var", "", "Session variable for multi-tenant row-level security (e.g. app.current_tenant)")
	connectionCmd.AddCommand(connectionAddCmd, connectionListCmd, connectionRemoveCmd, connectionTestCmd)
	rootCmd.AddCommand(connectionCmd)
}
