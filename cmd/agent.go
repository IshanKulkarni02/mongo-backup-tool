package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/dbhelm/internal/broker"
)

// exitError makes a command exit with a specific code without Cobra printing
// an "Error:" line — agent commands report through their JSON envelope.
type exitError struct{ code int }

func (e *exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

var (
	agentConn string
	agentDB   string
	agentWait int
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Database access for AI agents, through DBHelm (JSON in, JSON out)",
	Long: `agent is how a coding agent reads databases, and asks to change them, through
DBHelm. Every command prints one JSON envelope:

  {"ok":true,"data":...,"meta":{"rows":3,"truncated":false}}
  {"ok":false,"error":{"code":"DENIED","message":"...","hint":"..."}}

Reads run immediately, inside a database-enforced read-only transaction, with
row, size and time limits. Changes ("write", "snapshot restore") are requests:
DBHelm asks the user to approve them in VS Code, or approves them itself when
the user has switched Autopilot on. The agent never approves anything.

Exit codes: 0 ok, 1 error, 3 waiting for the user's approval, 4 refused
(DENIED, NO_OPERATOR, TIMEOUT).`,
	SilenceUsage: true,
}

func agentLeaf(use, short string, run func(ctx context.Context, cmd *cobra.Command, args []string) (string, map[string]any, error)) *cobra.Command {
	return &cobra.Command{
		Use: use, Short: short, SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			method, params, err := run(cmd.Context(), cmd, args)
			if err != nil {
				return emit(broker.Envelope{Error: &broker.ErrorBody{Code: broker.CodeBadRequest, Message: err.Error()}})
			}
			cwd, _ := os.Getwd()
			exe, _ := os.Executable()
			c, err := broker.Connect(cmd.Context(), broker.FindRunDir(cwd), exe, true)
			if err != nil {
				return emit(broker.Envelope{Error: &broker.ErrorBody{Code: "BROKER_UNAVAILABLE", Message: err.Error()}})
			}
			env, err := c.Call(cmd.Context(), method, params)
			if err != nil {
				return emit(broker.Envelope{Error: &broker.ErrorBody{Code: "BROKER_UNAVAILABLE", Message: err.Error()}})
			}
			return emit(env)
		},
	}
}

// emit prints the envelope and returns the exit code the outcome maps to.
func emit(env broker.Envelope) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(env)
	if env.OK {
		return nil
	}
	switch env.Error.Code {
	case broker.CodePendingApproval:
		return &exitError{3}
	case broker.CodeDenied, broker.CodeNoOperator, broker.CodeTimeout:
		return &exitError{4}
	}
	return &exitError{1}
}

func target() map[string]any { return map[string]any{"connection": agentConn, "database": agentDB} }

// readText returns v, or all of stdin when v is "-" (so multi-line SQL and
// JSON can be piped in without shell quoting).
func readText(v string) (string, error) {
	if v != "-" {
		return v, nil
	}
	b, err := io.ReadAll(os.Stdin)
	return string(b), err
}

func setIf(m map[string]any, k string, v any) {
	switch t := v.(type) {
	case string:
		if t == "" {
			return
		}
	case int:
		if t == 0 {
			return
		}
	}
	m[k] = v
}

func init() {
	agentCmd.PersistentFlags().StringVar(&agentConn, "connection", "", "Connection name (see: dbhelm agent connections)")
	agentCmd.PersistentFlags().StringVar(&agentDB, "db", "", "Database (Postgres: schema; SQLite: main)")

	agentCmd.AddCommand(
		agentLeaf("status", "Is the broker up, and is a person attached to approve changes?", func(ctx context.Context, cmd *cobra.Command, args []string) (string, map[string]any, error) {
			return "ping", nil, nil
		}),
		agentLeaf("connections", "List the connections agents may use (names and capabilities only, never credentials)", func(ctx context.Context, cmd *cobra.Command, args []string) (string, map[string]any, error) {
			return "connections", nil, nil
		}),
		agentLeaf("databases", "List a connection's databases (Postgres: schemas)", func(ctx context.Context, cmd *cobra.Command, args []string) (string, map[string]any, error) {
			return "databases", target(), nil
		}),
		agentLeaf("schema", "List a database's tables or collections", func(ctx context.Context, cmd *cobra.Command, args []string) (string, map[string]any, error) {
			return "schema", target(), nil
		}),
	)

	var describeTable, describeColl string
	describe := agentLeaf("describe", "Describe one table (columns, keys, indexes) or collection (indexes, a sample document)", func(ctx context.Context, cmd *cobra.Command, args []string) (string, map[string]any, error) {
		p := target()
		setIf(p, "table", describeTable)
		setIf(p, "collection", describeColl)
		return "describe", p, nil
	})
	describe.Flags().StringVar(&describeTable, "table", "", "SQL table")
	describe.Flags().StringVar(&describeColl, "collection", "", "Document collection")
	agentCmd.AddCommand(describe)

	var qSQL, qColl, qFilter, qSort, qPipeline string
	var qSkip, qLimit, qMax int
	query := agentLeaf("query", "Run a read-only query (SQL, document find, or aggregation pipeline)", func(ctx context.Context, cmd *cobra.Command, args []string) (string, map[string]any, error) {
		sqlText, err := readText(qSQL)
		if err != nil {
			return "", nil, err
		}
		pipeline, err := readText(qPipeline)
		if err != nil {
			return "", nil, err
		}
		p := target()
		setIf(p, "sql", sqlText)
		setIf(p, "collection", qColl)
		setIf(p, "filter", qFilter)
		setIf(p, "sort", qSort)
		setIf(p, "pipeline", pipeline)
		setIf(p, "skip", qSkip)
		setIf(p, "limit", qLimit)
		setIf(p, "maxRows", qMax)
		return "query", p, nil
	})
	query.Flags().StringVar(&qSQL, "sql", "", `SQL to run, or "-" to read it from stdin`)
	query.Flags().StringVar(&qColl, "collection", "", "Document collection to query")
	query.Flags().StringVar(&qFilter, "filter", "", "Document filter (Extended JSON)")
	query.Flags().StringVar(&qSort, "sort", "", "Document sort (Extended JSON)")
	query.Flags().StringVar(&qPipeline, "pipeline", "", `Aggregation pipeline (JSON array), or "-" for stdin`)
	query.Flags().IntVar(&qSkip, "skip", 0, "Documents to skip")
	query.Flags().IntVar(&qLimit, "limit", 0, "Documents to return")
	query.Flags().IntVar(&qMax, "max-rows", 0, "Row cap for this call (default 100, at most 1000)")
	agentCmd.AddCommand(query)

	var eSQL string
	explain := agentLeaf("explain", "Show a query plan without running the query", func(ctx context.Context, cmd *cobra.Command, args []string) (string, map[string]any, error) {
		sqlText, err := readText(eSQL)
		if err != nil {
			return "", nil, err
		}
		p := target()
		setIf(p, "sql", sqlText)
		return "explain", p, nil
	})
	explain.Flags().StringVar(&eSQL, "sql", "", `SQL to explain, or "-" for stdin`)
	agentCmd.AddCommand(explain)

	// snapshot
	snap := &cobra.Command{Use: "snapshot", Short: "Snapshots: list, create, and request a restore", SilenceUsage: true}
	var snapMsg, snapID, snapReason string
	snapList := agentLeaf("list", "List a database's snapshots, newest first", func(ctx context.Context, cmd *cobra.Command, args []string) (string, map[string]any, error) {
		return "snapshot.list", target(), nil
	})
	snapCreate := agentLeaf("create", "Take a snapshot now (do this before risky work)", func(ctx context.Context, cmd *cobra.Command, args []string) (string, map[string]any, error) {
		p := target()
		setIf(p, "message", snapMsg)
		return "snapshot.create", p, nil
	})
	snapCreate.Flags().StringVarP(&snapMsg, "message", "m", "", "Snapshot message")
	snapRestore := agentLeaf("restore", "Request a restore of a snapshot (the user must approve it)", func(ctx context.Context, cmd *cobra.Command, args []string) (string, map[string]any, error) {
		p := target()
		setIf(p, "snapshotId", snapID)
		setIf(p, "reason", snapReason)
		setIf(p, "wait", agentWait)
		return "snapshot.restore", p, nil
	})
	snapRestore.Flags().StringVar(&snapID, "snapshot", "", "Snapshot id (from: snapshot list)")
	snapRestore.Flags().StringVar(&snapReason, "reason", "", "Why, shown to the user")
	snapRestore.Flags().IntVar(&agentWait, "wait", 120, "Seconds to wait for the user's decision")
	var dFrom, dTo, dColl, dChange string
	var dOffset int
	snapDiff := agentLeaf("diff", "Compare two snapshots (per-collection change counts, or one page of changed ids)", func(ctx context.Context, cmd *cobra.Command, args []string) (string, map[string]any, error) {
		p := target()
		setIf(p, "from", dFrom)
		setIf(p, "to", dTo)
		setIf(p, "collection", dColl)
		setIf(p, "change", dChange)
		setIf(p, "offset", dOffset)
		return "snapshot.diff", p, nil
	})
	snapDiff.Flags().StringVar(&dFrom, "from", "", "Older snapshot id")
	snapDiff.Flags().StringVar(&dTo, "to", "", "Newer snapshot id (empty = the live database, MongoDB only)")
	snapDiff.Flags().StringVar(&dColl, "collection", "", "Table or collection: return its changed ids instead of counts")
	snapDiff.Flags().StringVar(&dChange, "change", "", "With --collection: added, modified or removed")
	snapDiff.Flags().IntVar(&dOffset, "offset", 0, "With --collection: page offset")
	snap.AddCommand(snapList, snapCreate, snapDiff, snapRestore)
	agentCmd.AddCommand(snap)

	// write
	var wSQL, wColl, wOp, wDoc, wID, wReason string
	var wWait int
	write := agentLeaf("write", "Request a change (the user approves it in VS Code, or Autopilot does)", func(ctx context.Context, cmd *cobra.Command, args []string) (string, map[string]any, error) {
		sqlText, err := readText(wSQL)
		if err != nil {
			return "", nil, err
		}
		doc, err := readText(wDoc)
		if err != nil {
			return "", nil, err
		}
		p := target()
		setIf(p, "statement", sqlText)
		setIf(p, "collection", wColl)
		setIf(p, "op", wOp)
		setIf(p, "document", doc)
		setIf(p, "id", wID)
		setIf(p, "reason", wReason)
		setIf(p, "wait", wWait)
		return "write", p, nil
	})
	write.Flags().StringVar(&wSQL, "sql", "", `SQL statement to run, or "-" for stdin`)
	write.Flags().StringVar(&wColl, "collection", "", "Document collection")
	write.Flags().StringVar(&wOp, "op", "", "Document operation: insert, update or delete")
	write.Flags().StringVar(&wDoc, "document", "", `Document (Extended JSON), or "-" for stdin`)
	write.Flags().StringVar(&wID, "id", "", "Document _id as JSON (update, delete)")
	write.Flags().StringVar(&wReason, "reason", "", "Why, shown to the user")
	write.Flags().IntVar(&wWait, "wait", 120, "Seconds to wait for the user's decision")
	agentCmd.AddCommand(write)

	var rWait int
	request := agentLeaf("request <id>", "Check (or wait for) a pending request", func(ctx context.Context, cmd *cobra.Command, args []string) (string, map[string]any, error) {
		if len(args) != 1 {
			return "", nil, errors.New("pass the request id")
		}
		p := map[string]any{"id": args[0]}
		setIf(p, "wait", rWait)
		return "request.get", p, nil
	})
	request.Flags().IntVar(&rWait, "wait", 0, "Seconds to wait for a decision")
	agentCmd.AddCommand(request)

	rootCmd.AddCommand(agentCmd)
}
