// Package scheduler runs recurring snapshot/backup jobs on a fixed
// interval, so mongobak doesn't depend on the OS's cron/Task Scheduler for
// routine use. Schedules use a plain duration ("1h", "24h", "15m") rather
// than cron-expression syntax — deliberately simpler than a full cron
// parser, and it covers the common cases (hourly/daily/weekly) without the
// edge-case risk of hand-rolling day-of-month/month/weekday matching.
package scheduler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/config"
)

// Action is what a schedule does when it fires.
type Action string

const (
	ActionSnapshot Action = "snapshot"
	ActionBackup   Action = "backup"
)

// Schedule is one recurring job.
type Schedule struct {
	ID         string `json:"id"`
	Connection string `json:"connection"`
	Database   string `json:"database"` // required for snapshot; empty means "all databases" for backup
	Action     Action `json:"action"`
	Message    string `json:"message,omitempty"` // snapshot message, if Action == snapshot
	Interval   string `json:"interval"`          // Go duration string, e.g. "1h", "24h"
	LastRun    string `json:"lastRun,omitempty"`
	NextRun    string `json:"nextRun,omitempty"`
}

func (s *Schedule) interval() (time.Duration, error) {
	d, err := time.ParseDuration(s.Interval)
	if err != nil {
		return 0, fmt.Errorf("invalid interval %q (expected e.g. \"1h\", \"24h\", \"15m\"): %w", s.Interval, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("interval must be positive, got %q", s.Interval)
	}
	return d, nil
}

// Due reports whether the schedule's next run time has passed.
func (s *Schedule) Due(now time.Time) bool {
	if s.NextRun == "" {
		return true
	}
	next, err := time.Parse(time.RFC3339, s.NextRun)
	if err != nil {
		return true
	}
	return !now.Before(next)
}

type scheduleFile struct {
	Schedules []Schedule `json:"schedules"`
}

func filePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "schedules.json"), nil
}

// Load returns every configured schedule.
func Load() ([]Schedule, error) {
	path, err := filePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f scheduleFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing schedules: %w", err)
	}
	return f.Schedules, nil
}

func save(schedules []Schedule) error {
	path, err := filePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(scheduleFile{Schedules: schedules}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Add creates a new schedule, validating its interval and computing its
// first NextRun (now + interval).
func Add(s Schedule) (*Schedule, error) {
	if s.Connection == "" {
		return nil, fmt.Errorf("a connection is required")
	}
	if s.Action != ActionSnapshot && s.Action != ActionBackup {
		return nil, fmt.Errorf("action must be %q or %q", ActionSnapshot, ActionBackup)
	}
	if s.Action == ActionSnapshot && s.Database == "" {
		return nil, fmt.Errorf("a database is required for snapshot schedules")
	}
	d, err := s.interval()
	if err != nil {
		return nil, err
	}

	s.ID = uuid.NewString()
	s.NextRun = time.Now().Add(d).Format(time.RFC3339)

	schedules, err := Load()
	if err != nil {
		return nil, err
	}
	schedules = append(schedules, s)
	if err := save(schedules); err != nil {
		return nil, err
	}
	return &s, nil
}

// Remove deletes a schedule by ID.
func Remove(id string) error {
	schedules, err := Load()
	if err != nil {
		return err
	}
	kept := schedules[:0]
	found := false
	for _, s := range schedules {
		if s.ID == id {
			found = true
			continue
		}
		kept = append(kept, s)
	}
	if !found {
		return fmt.Errorf("no schedule with id %q", id)
	}
	return save(kept)
}

// MarkRan records that a schedule fired at `at`, advancing its NextRun by
// one interval from `at` (not from "now"), so a delayed run doesn't cause
// runs to bunch up.
func MarkRan(id string, at time.Time) error {
	schedules, err := Load()
	if err != nil {
		return err
	}
	for i := range schedules {
		if schedules[i].ID != id {
			continue
		}
		d, err := schedules[i].interval()
		if err != nil {
			return err
		}
		schedules[i].LastRun = at.Format(time.RFC3339)
		schedules[i].NextRun = at.Add(d).Format(time.RFC3339)
		return save(schedules)
	}
	return fmt.Errorf("no schedule with id %q", id)
}
