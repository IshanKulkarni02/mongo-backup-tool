package cmd

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/dbhelm/internal/broker"
	"github.com/IshanKulkarni02/dbhelm/internal/mcp"
)

var mcpRunDir string

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Serve DBHelm to MCP clients (Cursor, Codex, Claude Desktop, ...) over stdio",
	Long: `mcp runs a Model Context Protocol server on stdin/stdout. It gives an MCP
client the same tools "dbhelm agent" gives Claude Code: read-only, bounded
reads, and change requests that only DBHelm can approve (in VS Code, or by
Autopilot). It never exposes a connection string or password.

Add it to a client's MCP configuration as:

  { "command": "dbhelm", "args": ["mcp"], "cwd": "<your project>" }

The broker for the project is found from the working directory, exactly like
"dbhelm agent"; if none is running, a headless one (reads only) is started.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		runDir := mcpRunDir
		if runDir == "" {
			cwd, _ := os.Getwd()
			runDir = broker.FindRunDir(cwd)
		}
		exe, _ := os.Executable()
		srv := &mcp.Server{Caller: &lazyBroker{runDir: runDir, exe: exe}, Version: version}
		return srv.Serve(cmd.Context(), os.Stdin, os.Stdout)
	},
}

// lazyBroker connects on the first tool call (so the initialize handshake
// never starts a process) and reconnects if the broker went away.
type lazyBroker struct {
	runDir, exe string
	mu          sync.Mutex
	c           *broker.Client
}

func (l *lazyBroker) client(ctx context.Context) (*broker.Client, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.c != nil && l.c.Alive(ctx) {
		return l.c, nil
	}
	c, err := broker.Connect(ctx, l.runDir, l.exe, true)
	if err != nil {
		return nil, err
	}
	l.c = c
	return c, nil
}

func (l *lazyBroker) Call(ctx context.Context, method string, params any) (broker.Envelope, error) {
	c, err := l.client(ctx)
	if err != nil {
		return broker.Envelope{}, fmt.Errorf("cannot reach the DBHelm broker: %w", err)
	}
	return c.Call(ctx, method, params)
}

func init() {
	mcpCmd.Flags().StringVar(&mcpRunDir, "run-dir", "", "Broker run directory (default: nearest .dbhelm/run above the working directory)")
	rootCmd.AddCommand(mcpCmd)
}
