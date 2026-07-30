package mongotools

import (
	"strings"
	"testing"
)

// TestRestoreRejectsTargetDBWithoutSourceDB guards against #13: an
// all-databases backup (SourceDB == "", the value stored for a backup
// taken with no --db) has no single source namespace to remap --nsFrom/
// --nsTo away from, so Restore must refuse a TargetDB in that case rather
// than silently ignoring it and restoring (optionally --drop-ing) the
// original databases instead of the caller's intended isolated copy. This
// calls Restore itself (not just the validation helper) since the guard
// fires before mongorestore is ever located or invoked, so it stays fast
// regardless of the test environment.
func TestRestoreRejectsTargetDBWithoutSourceDB(t *testing.T) {
	_, err := Restore(RestoreOptions{
		URI:         "mongodb://localhost:27017",
		ArchivePath: "/tmp/does-not-exist.gz",
		SourceDB:    "",
		TargetDB:    "scratch",
	})
	if err == nil {
		t.Fatal("expected an error restoring an all-databases backup into a single target database")
	}
	if !strings.Contains(err.Error(), "all-databases backup") {
		t.Fatalf("error = %q, want it to mention the all-databases restriction", err.Error())
	}
}

func TestValidateRestoreTargetDB(t *testing.T) {
	cases := []struct {
		name    string
		opts    RestoreOptions
		wantErr bool
	}{
		{"all-databases backup with target db", RestoreOptions{SourceDB: "", TargetDB: "scratch"}, true},
		{"all-databases backup without target db", RestoreOptions{SourceDB: "", TargetDB: ""}, false},
		{"single-database backup with target db", RestoreOptions{SourceDB: "myapp", TargetDB: "myapp_restored"}, false},
		{"single-database backup without target db", RestoreOptions{SourceDB: "myapp", TargetDB: ""}, false},
		{"target db equal to source db", RestoreOptions{SourceDB: "myapp", TargetDB: "myapp"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateRestoreTargetDB(c.opts)
			if (err != nil) != c.wantErr {
				t.Errorf("validateRestoreTargetDB(%+v) = %v, wantErr %v", c.opts, err, c.wantErr)
			}
		})
	}
}
