package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/remote"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/snapshot"
)

var remoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "Sync a database's snapshot history to a Git remote (e.g. GitHub), via Git LFS",
	Long: `Sync a database's snapshot history to a Git remote (e.g. GitHub).

This only works for a connection+database whose snapshot store uses the
"fs" backend (one file per document, rather than the default single-file
bbolt store) — that's what Git LFS needs to track content individually.
"remote init" switches a brand-new scope to that backend automatically;
it can't convert an existing bbolt-backed scope.`,
}

func init() {
	remoteCmd.PersistentFlags().StringVar(&snapConn, "connection", "", "Saved connection name (required)")
	remoteCmd.PersistentFlags().StringVar(&snapDB, "db", "", "Database name (required)")
	rootCmd.AddCommand(remoteCmd)
}

var remoteInitURL string
var remoteInitName string

var remoteInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Git + Git LFS for a database's snapshot store",
	Example: `  mongobak remote init --connection local --db myapp
  mongobak remote init --connection local --db myapp --url git@github.com:me/myapp-snapshots.git`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireConnAndDB(); err != nil {
			return err
		}
		scopeDir, err := snapshot.ScopeDir(snapConn, snapDB)
		if err != nil {
			return err
		}
		backend, err := snapshot.OpenBackendForRemote(scopeDir)
		if err != nil {
			return err
		}
		backend.Close()
		if err := remote.Init(scopeDir); err != nil {
			return err
		}
		fmt.Println("Git + Git LFS initialized for", snapConn+"/"+snapDB)
		if remoteInitURL != "" {
			if err := remote.AddRemote(scopeDir, remoteInitName, remoteInitURL); err != nil {
				return err
			}
			fmt.Printf("Remote %q set to %s\n", remoteInitName, remoteInitURL)
		}
		return nil
	},
}

var remotePushRemote string
var remotePushBranch string
var remotePushMessage string

var remotePushCmd = &cobra.Command{
	Use:   "push",
	Short: "Commit and push a database's snapshot history to its Git remote",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireConnAndDB(); err != nil {
			return err
		}
		scopeDir, err := snapshot.ScopeDir(snapConn, snapDB)
		if err != nil {
			return err
		}
		if !remote.IsInitialized(scopeDir) {
			return fmt.Errorf("not a git remote-sync scope yet — run: mongobak remote init --connection %s --db %s", snapConn, snapDB)
		}
		msg := remotePushMessage
		if msg == "" {
			msg = "mongobak sync"
		}
		if err := remote.Push(scopeDir, remotePushRemote, remotePushBranch, msg); err != nil {
			return err
		}
		fmt.Println("Pushed.")
		return nil
	},
}

var remotePullRemote string
var remotePullBranch string

var remotePullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull a database's snapshot history from its Git remote",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireConnAndDB(); err != nil {
			return err
		}
		scopeDir, err := snapshot.ScopeDir(snapConn, snapDB)
		if err != nil {
			return err
		}
		if !remote.IsInitialized(scopeDir) {
			return fmt.Errorf("not a git remote-sync scope yet — run: mongobak remote init --connection %s --db %s", snapConn, snapDB)
		}
		if err := remote.Pull(scopeDir, remotePullRemote, remotePullBranch); err != nil {
			return err
		}
		fmt.Println("Pulled.")
		return nil
	},
}

var remoteCloneBranch string

var remoteCloneCmd = &cobra.Command{
	Use:     "clone <git-url>",
	Short:   "Clone an existing remote snapshot history for a connection+database",
	Args:    cobra.ExactArgs(1),
	Example: `  mongobak remote clone git@github.com:me/myapp-snapshots.git --connection local --db myapp`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireConnAndDB(); err != nil {
			return err
		}
		scopeDir, err := snapshot.ScopeDir(snapConn, snapDB)
		if err != nil {
			return err
		}
		if err := remote.Clone(args[0], scopeDir, remoteCloneBranch); err != nil {
			return err
		}
		fmt.Println("Cloned into", scopeDir)
		return nil
	},
}

func init() {
	remoteInitCmd.Flags().StringVar(&remoteInitURL, "url", "", "Git remote URL to add (optional)")
	remoteInitCmd.Flags().StringVar(&remoteInitName, "name", "origin", "Name for the remote added via --url")

	remotePushCmd.Flags().StringVar(&remotePushRemote, "remote", "origin", "Remote name")
	remotePushCmd.Flags().StringVar(&remotePushBranch, "branch", "main", "Branch name")
	remotePushCmd.Flags().StringVarP(&remotePushMessage, "message", "m", "", "Commit message")

	remotePullCmd.Flags().StringVar(&remotePullRemote, "remote", "origin", "Remote name")
	remotePullCmd.Flags().StringVar(&remotePullBranch, "branch", "main", "Branch name")

	remoteCloneCmd.Flags().StringVar(&remoteCloneBranch, "branch", "main", "Branch to check out")

	remoteCmd.AddCommand(remoteInitCmd, remotePushCmd, remotePullCmd, remoteCloneCmd)
}
