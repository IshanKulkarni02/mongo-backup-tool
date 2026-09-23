package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/dbhelm/internal/broker"
)

var (
	serveRunDir      string
	serveHeadless    bool
	serveIdle        time.Duration
	serveNoSafety    bool
	serveExitOnStdin bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the local agent broker (started by the VS Code extension and by `dbhelm agent`)",
	Long: `serve runs the DBHelm broker: the local service AI agents reach databases
through. Agents connect with "dbhelm agent ..."; the VS Code extension attaches
as the operator, and is the only place changes are approved and Autopilot is
switched on.

Started by the extension, serve prints one JSON line on stdout with the port
and the operator token, then runs until stdin closes (--exit-on-stdin-close).
Started headless (--headless, which "dbhelm agent" does automatically when no
broker is running), no operator can attach: reads work, and any request to
change data is refused.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		runDir := serveRunDir
		if runDir == "" {
			cwd, _ := os.Getwd()
			runDir = broker.FindRunDir(cwd)
		}
		if err := handleExistingBroker(cmd.Context(), runDir, serveHeadless); err != nil {
			return err
		}
		if serveHeadless {
			if info, err := broker.LoadInfo(runDir); err == nil && broker.NewAgentClient(info).Alive(cmd.Context()) {
				return nil // another headless broker won the race; nothing to do
			}
		}

		b, err := broker.New(broker.Options{
			RunDir: runDir, LogPath: broker.LogPathFor(runDir), Headless: serveHeadless,
			SafetySnapshot: !serveNoSafety, IdleTimeout: serveIdle, Version: version,
		})
		if err != nil {
			return err
		}
		info, err := b.Start()
		if err != nil {
			return err
		}
		defer b.Close()
		json.NewEncoder(os.Stdout).Encode(info)

		stdinClosed := make(chan struct{})
		if serveExitOnStdin {
			go func() { io.Copy(io.Discard, os.Stdin); close(stdinClosed) }()
		}
		select {
		case <-cmd.Context().Done():
		case <-b.Done():
		case <-stdinClosed:
		}
		return nil
	},
}

// handleExistingBroker deals with a broker already serving runDir. An
// operator start replaces a headless one (nothing important lives in it); it
// never displaces another operator's broker, whose token it cannot have.
func handleExistingBroker(ctx context.Context, runDir string, wantHeadless bool) error {
	info, err := broker.LoadInfo(runDir)
	if err != nil {
		return nil
	}
	c := broker.NewAgentClient(info)
	if !c.Alive(ctx) {
		return nil // stale broker.json from a broker that is gone
	}
	if wantHeadless {
		return nil
	}
	if !info.Headless {
		return fmt.Errorf("another DBHelm operator (pid %d) is already running for this workspace; close the other VS Code window or use it", info.PID)
	}
	c.Call(ctx, "shutdown", nil)
	for i := 0; i < 50; i++ {
		if !c.Alive(ctx) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("the headless broker (pid %d) did not stop", info.PID)
}

func init() {
	serveCmd.Flags().StringVar(&serveRunDir, "run-dir", "", "Directory for broker.json (default: the nearest .dbhelm/run above the current directory)")
	serveCmd.Flags().BoolVar(&serveHeadless, "headless", false, "No operator: reads only, writes are refused")
	serveCmd.Flags().DurationVar(&serveIdle, "idle", 0, "Exit after this long with no requests (0 = never)")
	serveCmd.Flags().BoolVar(&serveNoSafety, "no-safety-snapshot", false, "Do not snapshot a database before applying an approved agent change")
	serveCmd.Flags().BoolVar(&serveExitOnStdin, "exit-on-stdin-close", false, "Exit when stdin closes (set by the extension, so the broker never outlives it)")
	rootCmd.AddCommand(serveCmd)
}
