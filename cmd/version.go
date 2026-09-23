package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/dbhelm/internal/broker"
)

var versionJSON bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print dbhelm's version",
	Run: func(cmd *cobra.Command, args []string) {
		if versionJSON {
			json.NewEncoder(os.Stdout).Encode(map[string]any{"name": "dbhelm", "version": version, "protocolVersion": broker.ProtocolVersion})
			return
		}
		fmt.Println("dbhelm", version)
	},
}

func init() {
	versionCmd.Flags().BoolVar(&versionJSON, "json", false, "Print machine-readable version and broker protocol version")
	rootCmd.AddCommand(versionCmd)
}
