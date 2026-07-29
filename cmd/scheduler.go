package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/scheduler"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/snapshot"
)

var schedulerCmd = &cobra.Command{
	Use:   "scheduler",
	Short: "Run recurring snapshot/backup jobs without needing external cron",
}

func init() {
	rootCmd.AddCommand(schedulerCmd)
}

var (
	schedAddConn     string
	schedAddDB       string
	schedAddAction   string
	schedAddInterval string
	schedAddMessage  string
)

var schedulerAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a recurring schedule",
	Example: `  mongobak scheduler add --connection local --db myapp --action snapshot --interval 1h
  mongobak scheduler add --connection local --action backup --interval 24h`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := scheduler.Add(scheduler.Schedule{
			Connection: schedAddConn,
			Database:   schedAddDB,
			Action:     scheduler.Action(schedAddAction),
			Message:    schedAddMessage,
			Interval:   schedAddInterval,
		})
		if err != nil {
			return err
		}
		fmt.Printf("Schedule %s added: %s every %s, next run %s\n", s.ID, s.Action, s.Interval, s.NextRun)
		return nil
	},
}

var schedulerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured schedules",
	RunE: func(cmd *cobra.Command, args []string) error {
		schedules, err := scheduler.Load()
		if err != nil {
			return err
		}
		if len(schedules) == 0 {
			fmt.Println("No schedules yet. Add one with: mongobak scheduler add --connection <name> --action snapshot|backup --interval 1h")
			return nil
		}
		for _, s := range schedules {
			db := s.Database
			if db == "" {
				db = "(all)"
			}
			fmt.Printf("%s  %-10s %-15s %-15s every %-6s next %s\n", s.ID, s.Action, s.Connection, db, s.Interval, s.NextRun)
		}
		return nil
	},
}

var schedulerRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Remove a schedule",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := scheduler.Remove(args[0]); err != nil {
			return err
		}
		fmt.Println("Removed schedule", args[0])
		return nil
	},
}

var schedulerRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run in the foreground, firing due schedules until interrupted (Ctrl+C)",
	Long: `Run in the foreground, checking every 30 seconds for due schedules and
firing them. This is mongobak's built-in alternative to external cron —
start it once (directly, under a process supervisor, or as a launchd/
systemd service) and leave it running.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Scheduler running. Press Ctrl+C to stop.")
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()

		runDue() // check immediately on startup, then on each tick
		for {
			select {
			case <-ctx.Done():
				fmt.Println("Stopped.")
				return nil
			case <-tick.C:
				runDue()
			}
		}
	},
}

func runDue() {
	schedules, err := scheduler.Load()
	if err != nil {
		fmt.Println("Error loading schedules:", err)
		return
	}
	now := time.Now()
	for _, s := range schedules {
		if !s.Due(now) {
			continue
		}
		fmt.Printf("[%s] running schedule %s (%s %s/%s)\n", now.Format(time.RFC3339), s.ID, s.Action, s.Connection, s.Database)
		if err := fireSchedule(s); err != nil {
			fmt.Printf("[%s] schedule %s failed: %v\n", time.Now().Format(time.RFC3339), s.ID, err)
			// Still advance NextRun on failure — a persistently broken
			// schedule (e.g. unreachable database) shouldn't fire every
			// tick forever; it'll retry at the next normal interval.
		}
		if err := scheduler.MarkRan(s.ID, now); err != nil {
			fmt.Println("Error updating schedule:", err)
		}
	}
}

func fireSchedule(s scheduler.Schedule) error {
	switch s.Action {
	case scheduler.ActionSnapshot:
		conn, err := resolveConn(s.Connection)
		if err != nil {
			return err
		}
		_, err = snapshot.Create(snapshot.CreateOptions{
			Connection: s.Connection,
			URI:        conn.URI,
			Database:   s.Database,
			Message:    s.Message,
		})
		return err
	case scheduler.ActionBackup:
		_, err := RunBackup(s.Connection, s.Database)
		return err
	default:
		return fmt.Errorf("unknown action %q", s.Action)
	}
}

func init() {
	schedulerAddCmd.Flags().StringVar(&schedAddConn, "connection", "", "Saved connection name (required)")
	schedulerAddCmd.Flags().StringVar(&schedAddDB, "db", "", "Database name (required for snapshot; optional for backup)")
	schedulerAddCmd.Flags().StringVar(&schedAddAction, "action", "", "\"snapshot\" or \"backup\" (required)")
	schedulerAddCmd.Flags().StringVar(&schedAddInterval, "interval", "", "How often to run, e.g. \"1h\", \"24h\", \"15m\" (required)")
	schedulerAddCmd.Flags().StringVarP(&schedAddMessage, "message", "m", "", "Snapshot message (snapshot schedules only)")
	schedulerAddCmd.MarkFlagRequired("connection")
	schedulerAddCmd.MarkFlagRequired("action")
	schedulerAddCmd.MarkFlagRequired("interval")

	schedulerCmd.AddCommand(schedulerAddCmd, schedulerListCmd, schedulerRemoveCmd, schedulerRunCmd)
}
