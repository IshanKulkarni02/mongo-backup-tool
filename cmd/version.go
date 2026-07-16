package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print mongobak's version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("mongobak", version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
