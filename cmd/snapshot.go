package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/config"
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
// standard "see: mongobak connection list" hint.
func resolveConn(name string) (*config.Connection, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	conn, ok := cfg.Find(name)
	if !ok {
		return nil, fmt.Errorf("no connection named %q (see: mongobak connection list)", name)
	}
	return conn, nil
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
