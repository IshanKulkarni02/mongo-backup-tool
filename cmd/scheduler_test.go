package cmd

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/IshanKulkarni02/dbhelm/internal/scheduler"
	"github.com/IshanKulkarni02/dbhelm/internal/snapshot"
)

// TestRunDueDoesNotRefireAfterSimulatedCrash is the end-to-end regression
// test for #59: runDue's real fix is calling scheduler.BeginRun (which
// advances NextRun) before fireSchedule runs. This simulates a crash
// between BeginRun and the job actually completing (MarkRan never
// called, fireSchedule never called either — standing in for "the
// process died right after BeginRun persisted") and confirms the next
// runDue tick does not treat the schedule as due anymore, so it can't
// fire the same interval's job a second time.
func TestRunDueDoesNotRefireAfterSimulatedCrash(t *testing.T) {
	withTempConfigDir(t)
	resetConnAddFlags()

	dbPath := filepath.Join(t.TempDir(), "scheduler-cli-test.db")
	connAddURI = dbPath
	connAddEngine = "sqlite"
	if err := connectionAddCmd.RunE(connectionAddCmd, []string{"sched-cli-test"}); err != nil {
		t.Fatalf("connection add: %v", err)
	}

	conn, err := resolveConn("sched-cli-test")
	if err != nil {
		t.Fatalf("resolveConn: %v", err)
	}
	sess, release, err := openSQLSession(conn)
	if err != nil {
		t.Fatalf("openSQLSession: %v", err)
	}
	if _, err := sess.Execute(context.Background(), "main", `CREATE TABLE t (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	release()

	// Schedule.NextRun/LastRun are persisted with time.RFC3339, which has
	// only second-level precision — an interval shorter than a second
	// would round-trip through Load/save losing its sub-second advance
	// entirely, so use whole seconds here (same units the product always
	// uses: "1h", "24h", "15m") to keep the test meaningful.
	const interval = 2 * time.Second
	s, err := scheduler.Add(scheduler.Schedule{
		Connection: "sched-cli-test",
		Database:   "main",
		Action:     scheduler.ActionSnapshot,
		Interval:   interval.String(),
	})
	if err != nil {
		t.Fatalf("scheduler.Add: %v", err)
	}
	time.Sleep(interval + 500*time.Millisecond) // let the interval elapse, so it's now due

	fireTime := time.Now()
	if err := scheduler.BeginRun(s.ID, fireTime); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	// Simulate the crash here: fireSchedule/MarkRan never run for this cycle.

	runDue() // the next tick after a restart, well within `interval` of fireTime

	items, err := snapshot.Log("sched-cli-test", "main")
	if err != nil {
		t.Fatalf("listing snapshots: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no snapshot to have been created — runDue treated the schedule as due again after BeginRun already advanced NextRun, reproducing #59's double-fire bug (got %d snapshots)", len(items))
	}

	schedules, err := scheduler.Load()
	if err != nil {
		t.Fatalf("scheduler.Load: %v", err)
	}
	if schedules[0].Due(fireTime) {
		t.Fatal("schedule is still Due at fireTime after BeginRun — NextRun was not actually advanced")
	}
}
