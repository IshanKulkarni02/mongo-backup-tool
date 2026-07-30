// Package cmd implements DBHelm's CLI commands.
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/IshanKulkarni02/dbhelm/internal/tui"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "dbhelm",
	Short: "Back up, restore, and version-control MongoDB databases, local or Atlas",
	Long: `dbhelm is a cross-platform tool for backing up, restoring, and
version-controlling MongoDB databases — local deployments or Atlas clusters.
(For Postgres/MySQL/SQLite and the full SQL editor, AI assistant, and
dashboards, see the DBHelm desktop app — run "dbhelm launcher" to open it.)

Typical workflow:
  dbhelm connection add mydb --uri "mongodb://localhost:27017"
  dbhelm snapshot create --connection mydb --db myapp -m "checkpoint"
  dbhelm backup --connection mydb --db myapp
  dbhelm list

Run "dbhelm guide" for a full in-terminal usage walkthrough, or just run
"dbhelm" with no arguments for an interactive, arrow-key driven UI.`,
	SilenceUsage: true,
	Args:         cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return cmd.Help()
		}
		return tui.Run()
	},
}

// Execute runs the root command; it's the sole entry point called from
// main(). The command tree receives a context that's cancelled on
// SIGINT/SIGTERM, so long-running operations (a large snapshot, diff, or
// restore) can be interrupted with Ctrl-C instead of leaving the process to
// finish (or hang) regardless of the user's intent.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
