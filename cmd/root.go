// Package cmd implements mongobak's CLI commands.
package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/tui"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "mongobak",
	Short: "Back up, restore, and version-control MongoDB databases, local or Atlas",
	Long: `mongobak is a cross-platform tool for backing up, restoring, and
version-controlling MongoDB databases — local deployments or Atlas clusters.

Typical workflow:
  mongobak connection add mydb --uri "mongodb://localhost:27017"
  mongobak snapshot create --connection mydb --db myapp -m "checkpoint"
  mongobak backup --connection mydb --db myapp
  mongobak list

Run "mongobak guide" for a full in-terminal usage walkthrough, or just run
"mongobak" with no arguments for an interactive, arrow-key driven UI.`,
	SilenceUsage: true,
	Args:         cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
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

// redactURI masks a URI's password for safe display in list/log output.
func redactURI(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if _, hasPass := u.User.Password(); hasPass {
		u.User = url.UserPassword(u.User.Username(), "****")
	}
	return u.String()
}
