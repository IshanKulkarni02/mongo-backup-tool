package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/depmanager"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that mongodump/mongorestore are installed and reachable",
	RunE: func(cmd *cobra.Command, args []string) error {
		statuses := depmanager.Check()
		for _, s := range statuses {
			if s.Installed {
				fmt.Printf("OK %s: %s\n", s.Dependency.Name, s.Version)
			} else {
				fmt.Printf("x %s: not found (%s)\n", s.Dependency.Name, s.Dependency.Description)
			}
		}
		if depmanager.AllInstalled(statuses) {
			fmt.Println("\nAll required tools are available.")
			return nil
		}

		fmt.Println("\nThe MongoDB Database Tools are required for backup/restore (snapshots don't need them). Install them:")
		for _, line := range depmanager.ManualInstructions() {
			fmt.Println(" ", line)
		}
		if depmanager.AutoInstallAvailable() {
			fmt.Println("\nOr let mongobak install them for you: mongobak doctor install")
		}
		return fmt.Errorf("missing required tools")
	},
}

var doctorInstallYes bool

var doctorInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Automatically install missing dependencies (mongodump/mongorestore)",
	RunE: func(cmd *cobra.Command, args []string) error {
		statuses := depmanager.Check()
		missing := depmanager.Missing(statuses)
		if len(missing) == 0 {
			fmt.Println("Everything is already installed.")
			return nil
		}
		if !depmanager.AutoInstallAvailable() {
			fmt.Println("Automatic install isn't available on this OS. Manual instructions:")
			for _, line := range depmanager.ManualInstructions() {
				fmt.Println(" ", line)
			}
			return fmt.Errorf("automatic install unavailable")
		}

		if !doctorInstallYes {
			fmt.Println("This will run your OS's package manager to install the MongoDB Database Tools:")
			for _, line := range depmanager.ManualInstructions() {
				fmt.Println(" ", line)
			}
			fmt.Print("\nProceed? [y/N] ")
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Cancelled. Install manually with the commands above, or re-run with --yes.")
				return nil
			}
		}

		fmt.Println("Installing...")
		err := depmanager.AutoInstall(context.Background(), func(line string) {
			fmt.Println(" ", line)
		})
		if err != nil {
			fmt.Println("\nAutomatic install failed:", err)
			fmt.Println("Manual instructions:")
			for _, line := range depmanager.ManualInstructions() {
				fmt.Println(" ", line)
			}
			return err
		}

		fmt.Println("\nDone. Verifying...")
		after := depmanager.Check()
		if depmanager.AllInstalled(after) {
			fmt.Println("All required tools are now available.")
			return nil
		}
		return fmt.Errorf("install completed but some tools are still missing — run `mongobak doctor` for details")
	},
}

func init() {
	doctorInstallCmd.Flags().BoolVar(&doctorInstallYes, "yes", false, "Skip the confirmation prompt")
	doctorCmd.AddCommand(doctorInstallCmd)
	rootCmd.AddCommand(doctorCmd)
}
