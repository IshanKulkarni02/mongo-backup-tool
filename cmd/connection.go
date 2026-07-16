package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/config"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/mongotools"
)

var connectionCmd = &cobra.Command{
	Use:   "connection",
	Short: "Manage saved MongoDB connections (local or Atlas)",
}

var connAddURI string

var connectionAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Save a connection under a name",
	Args:  cobra.ExactArgs(1),
	Example: `  mongobak connection add local --uri "mongodb://localhost:27017"
  mongobak connection add prod  --uri "mongodb+srv://user:pass@cluster0.mongodb.net"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if connAddURI == "" {
			return fmt.Errorf("--uri is required, e.g. mongodb://localhost:27017 or mongodb+srv://user:pass@cluster.mongodb.net")
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		cfg.Upsert(config.Connection{Name: name, URI: connAddURI, CreatedAt: time.Now().Format(time.RFC3339)})
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Printf("Saved connection %q\n", name)
		return nil
	},
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
			fmt.Println("No connections saved. Add one with: mongobak connection add <name> --uri <uri>")
			return nil
		}
		for _, c := range cfg.Connections {
			fmt.Printf("%-20s %s\n", c.Name, redactURI(c.URI))
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
		dbs, err := mongotools.TestConnection(conn.URI)
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
	connectionAddCmd.Flags().StringVar(&connAddURI, "uri", "", "MongoDB connection URI")
	connectionCmd.AddCommand(connectionAddCmd, connectionListCmd, connectionRemoveCmd, connectionTestCmd)
	rootCmd.AddCommand(connectionCmd)
}
