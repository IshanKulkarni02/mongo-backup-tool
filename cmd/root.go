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
	Short: "Back up and restore MongoDB databases, local or Atlas",
	Long: `mongobak is a cross-platform CLI for backing up and restoring MongoDB
databases — local deployments or Atlas clusters — using the official
mongodump/mongorestore tools under the hood.

Typical workflow:
  mongobak connection add mydb --uri "mongodb://localhost:27017"
  mongobak backup --connection mydb --db myapp
  mongobak list
  mongobak restore --backup <id> --connection mydb

Run "mongobak ui" for a local web interface instead of the CLI.`,
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
