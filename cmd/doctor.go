package cmd

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/mongotools"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that mongodump/mongorestore are installed and reachable",
	RunE: func(cmd *cobra.Command, args []string) error {
		allOK := true
		for _, tool := range []string{"mongodump", "mongorestore"} {
			v, err := mongotools.Version(tool)
			if err != nil {
				allOK = false
				fmt.Printf("x %s: not found\n", tool)
				continue
			}
			fmt.Printf("OK %s: %s\n", tool, firstLine(v))
		}
		if allOK {
			fmt.Println("\nAll required tools are available.")
			return nil
		}

		fmt.Println("\nThe MongoDB Database Tools are required for backup/restore. Install them:")
		switch runtime.GOOS {
		case "darwin":
			fmt.Println("  brew tap mongodb/brew && brew install mongodb-database-tools")
			fmt.Println("  (if that fails to build from source, download a prebuilt binary from")
			fmt.Println("   https://www.mongodb.com/try/download/database-tools and put mongodump/")
			fmt.Println("   mongorestore on your PATH, e.g. in ~/.local/bin)")
		case "linux":
			fmt.Println("  See: https://www.mongodb.com/docs/database-tools/installation/installation-linux/")
		case "windows":
			fmt.Println("  See: https://www.mongodb.com/docs/database-tools/installation/installation-windows/")
		}
		fmt.Println("  Or download directly: https://www.mongodb.com/try/download/database-tools")
		return fmt.Errorf("missing required tools")
	},
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
