package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/IshanKulkarni02/dbhelm/internal/tui"
)

var launcherCmd = &cobra.Command{
	Use:   "launcher",
	Short: "Reopen the terminal-vs-desktop-app chooser",
	Long: `launcher reopens the chooser you saw the first time you ran
"dbhelm" — pick "Continue in terminal" to stay here, or "Get the full app"
to launch (or download) the DBHelm desktop app.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return cmd.Help()
		}
		return tui.RunChooser()
	},
}

func init() {
	rootCmd.AddCommand(launcherCmd)
}
