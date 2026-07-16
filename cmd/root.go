// Package cmd implements mongobak's CLI commands.
package cmd

import (
	"fmt"
	"net/url"
	"os"

	"github.com/spf13/cobra"
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

Run "mongobak guide" for a full in-terminal usage walkthrough.`,
	SilenceUsage: true,
}

// Execute runs the root command; it's the sole entry point called from main().
func Execute() {
	if err := rootCmd.Execute(); err != nil {
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
