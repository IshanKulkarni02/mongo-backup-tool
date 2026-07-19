package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print dbhelm's version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("dbhelm", version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
