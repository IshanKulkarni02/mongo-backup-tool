package scheduler

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
)

func withTempConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
}

func TestAddLoadRemoveRoundTrip(t *testing.T) {
	withTempConfigDir(t)

	s, err := Add(Schedule{Connection: "local", Database: "app", Action: ActionBackup, Interval: "1h"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if s.ID == "" {
		t.Fatal("expected a generated ID")
	}
	if s.NextRun == "" {
		t.Fatal("expected NextRun to be set")
	}

	schedules, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(schedules) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(schedules))
	}

	if err := Remove(s.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	schedules, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(schedules) != 0 {
		t.Fatalf("expected 0 schedules after Remove, got %d", len(schedules))
	}
}

func TestRemoveMissingReturnsError(t *testing.T) {
	withTempConfigDir(t)
	if err := Remove("nope"); err == nil {
		t.Fatal("expected an error removing a nonexistent schedule")
	}
}

// TestBeginRunAdvancesNextRunOnly guards against #59: BeginRun must
// advance NextRun without touching LastRun (LastRun is MarkRan's job,
// called separately after the job completes).
func TestBeginRunAdvancesNextRunOnly(t *testing.T) {
	withTempConfigDir(t)
	s, err := Add(Schedule{Connection: "local", Database: "app", Action: ActionBackup, Interval: "1h"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	oldNextRun := s.NextRun

	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := BeginRun(s.ID, at); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}

	schedules, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if schedules[0].LastRun != "" {
		t.Fatalf("expected LastRun to still be empty after BeginRun alone, got %q", schedules[0].LastRun)
	}
	wantNext := at.Add(time.Hour).Format(time.RFC3339)
	if schedules[0].NextRun != wantNext {
		t.Fatalf("NextRun = %q, want %q", schedules[0].NextRun, wantNext)
	}
	if schedules[0].NextRun == oldNextRun {
		t.Fatal("expected NextRun to have advanced from its initial value")
	}
}

// TestMarkRanSetsLastRunOnlyNotNextRun guards against #59: MarkRan (called
// after the job completes) must not re-advance NextRun — BeginRun already
// did that before the job ran. If MarkRan advanced it again, an interval
// would effectively be skipped every time.
func TestMarkRanSetsLastRunOnlyNotNextRun(t *testing.T) {
	withTempConfigDir(t)
	s, err := Add(Schedule{Connection: "local", Database: "app", Action: ActionBackup, Interval: "1h"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	beginAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := BeginRun(s.ID, beginAt); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	nextRunAfterBegin, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	wantNextRun := nextRunAfterBegin[0].NextRun

	// MarkRan is called after the job finishes, potentially somewhat
	// later than beginAt — that later timestamp must not feed into a
	// second NextRun computation.
	markAt := beginAt.Add(5 * time.Second)
	if err := MarkRan(s.ID, markAt); err != nil {
		t.Fatalf("MarkRan: %v", err)
	}

	schedules, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if schedules[0].LastRun != markAt.Format(time.RFC3339) {
		t.Fatalf("LastRun = %q, want %q", schedules[0].LastRun, markAt.Format(time.RFC3339))
	}
	if schedules[0].NextRun != wantNextRun {
		t.Fatalf("NextRun changed by MarkRan: got %q, want unchanged %q", schedules[0].NextRun, wantNextRun)
	}
}

func TestMarkRanMissingReturnsError(t *testing.T) {
	withTempConfigDir(t)
	if err := MarkRan("nope", time.Now()); err == nil {
		t.Fatal("expected an error marking a nonexistent schedule as ran")
	}
}

func TestBeginRunMissingReturnsError(t *testing.T) {
	withTempConfigDir(t)
	if err := BeginRun("nope", time.Now()); err == nil {
		t.Fatal("expected an error beginning a run for a nonexistent schedule")
	}
}

// TestCrashBetweenBeginRunAndMarkRanDoesNotRefire is the direct
// regression test for #59: simulate a process that calls BeginRun, then
// crashes before ever calling MarkRan (never happens — the "crash" is
// just not calling it). A fresh process loading schedules afterward must
// see the schedule as no longer due for the interval that already
// "fired", proving the double-fire gap is closed.
func TestCrashBetweenBeginRunAndMarkRanDoesNotRefire(t *testing.T) {
	withTempConfigDir(t)
	s, err := Add(Schedule{Connection: "local", Database: "app", Action: ActionBackup, Interval: "1h"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Simulate enough time passing for the schedule to become due — Add
	// always sets NextRun to now+interval, so push it into the past
	// directly rather than waiting an hour.
	schedules, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	schedules[0].NextRun = time.Now().Add(-time.Minute).Format(time.RFC3339)
	if err := save(schedules); err != nil {
		t.Fatal(err)
	}

	fireTime := time.Now()
	before, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !before[0].Due(fireTime) {
		t.Fatal("expected the schedule to be due after its NextRun was set into the past")
	}

	if err := BeginRun(s.ID, fireTime); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	// Simulate a crash: MarkRan is never called.

	after, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if after[0].Due(fireTime) {
		t.Fatal("schedule is still reported Due at the same fireTime after BeginRun — the next scheduler run would fire it again, reproducing #59's double-fire bug")
	}
}

// TestAddConcurrentCallsDontLoseWrites guards against #60: Add/Remove/
// MarkRan each used to do their own Load->mutate->save with no locking,
// so two calls hitting schedules.json close together raced — whichever
// save landed second silently discarded the other's change. This is
// specifically a cross-process race ("dbhelm scheduler run" in one
// terminal, "dbhelm scheduler add" in another), not just concurrent
// goroutines in one process, which is exactly what a file-backed lock
// (rather than an in-memory mutex) is needed to close — and since the
// lock lives on disk, not in process memory, these goroutines exercise
// the same code path two separate OS processes would.
func TestAddConcurrentCallsDontLoseWrites(t *testing.T) {
	withTempConfigDir(t)

	const n = 30
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := Add(Schedule{
				Connection: fmt.Sprintf("conn-%d", i),
				Database:   "app",
				Action:     ActionBackup,
				Interval:   "1h",
			})
			if err != nil {
				t.Errorf("Add: %v", err)
			}
		}(i)
	}
	wg.Wait()

	schedules, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(schedules) != n {
		t.Fatalf("expected %d schedules after %d concurrent Add calls, got %d", n, n, len(schedules))
	}
	seen := map[string]bool{}
	for _, s := range schedules {
		seen[s.Connection] = true
	}
	for i := 0; i < n; i++ {
		if !seen[fmt.Sprintf("conn-%d", i)] {
			t.Errorf("conn-%d is missing — its Add was lost to a race", i)
		}
	}
}

// TestMarkRanConcurrentWithAddDontLoseWrites simulates the issue's exact
// scenario: a scheduler-run loop repeatedly calling MarkRan on one
// schedule while other schedules are concurrently Added — the MarkRan
// update (LastRun) and every concurrent Add must all survive.
func TestMarkRanConcurrentWithAddDontLoseWrites(t *testing.T) {
	withTempConfigDir(t)

	running, err := Add(Schedule{Connection: "running", Database: "app", Action: ActionBackup, Interval: "1h"})
	if err != nil {
		t.Fatalf("seeding Add: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := MarkRan(running.ID, time.Now()); err != nil {
				t.Errorf("MarkRan: %v", err)
			}
		}(i)
	}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := Add(Schedule{Connection: fmt.Sprintf("added-%d", i), Database: "app", Action: ActionBackup, Interval: "1h"})
			if err != nil {
				t.Errorf("Add: %v", err)
			}
		}(i)
	}
	wg.Wait()

	schedules, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(schedules) != n+1 {
		t.Fatalf("expected %d schedules (1 seeded + %d added), got %d", n+1, n, len(schedules))
	}
	for i := 0; i < n; i++ {
		found := false
		for _, s := range schedules {
			if s.Connection == fmt.Sprintf("added-%d", i) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("added-%d is missing — its Add was lost to a race with concurrent MarkRan calls", i)
		}
	}
}

// TestSaveWriteIsAtomic guards against #60's other half: the original
// save used a direct os.WriteFile, so a crash mid-write could corrupt
// schedules.json into something the next Load can't parse. save must go
// through a temp file + rename, leaving no stray temp file behind.
func TestSaveWriteIsAtomic(t *testing.T) {
	withTempConfigDir(t)
	if _, err := Add(Schedule{Connection: "local", Database: "app", Action: ActionBackup, Interval: "1h"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	dir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("stray temp file left behind after save: %s", e.Name())
		}
	}
}
