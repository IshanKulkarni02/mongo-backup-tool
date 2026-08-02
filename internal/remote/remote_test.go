package remote

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/IshanKulkarni02/dbhelm/internal/snapshot"
	"github.com/IshanKulkarni02/dbhelm/internal/testmongod"
)

func requireGitAndLFS(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found on PATH — skipping remote sync test")
	}
	if _, err := exec.LookPath("git-lfs"); err != nil {
		t.Skip("git-lfs not found on PATH — skipping remote sync test")
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// TestPushCloneRoundTripsContent exercises the actual push→clone path
// against a real local bare git repo (standing in for GitHub — the git
// protocol doesn't distinguish) and a real mongod snapshot (so object
// content goes through the genuine hash/compress pipeline, not synthetic
// bytes), using real Git LFS. It verifies the cloned copy reproduces both
// the document content — read back through the snapshot engine end to end
// (restore into a fresh database), not just "a file with this name exists"
// — and the doc-ref list.
func TestPushCloneRoundTripsContent(t *testing.T) {
	requireGitAndLFS(t)
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())

	uri := testmongod.Start(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	docs := []interface{}{
		bson.D{{Key: "n", Value: 1}, {Key: "label", Value: "widget-a"}},
		bson.D{{Key: "n", Value: 2}, {Key: "label", Value: "widget-b"}},
	}
	if _, err := client.Database("remotedb").Collection("widgets").InsertMany(ctx, docs); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	client.Disconnect(context.Background())

	const connName = "remote-test"
	const dbName = "remotedb"

	res, err := snapshot.Create(snapshot.CreateOptions{
		Connection: connName,
		URI:        uri,
		Database:   dbName,
		Message:    "for remote sync test",
		Backend:    snapshot.BackendFS,
	})
	if err != nil {
		t.Fatalf("snapshot.Create: %v", err)
	}

	sourceScope, err := snapshot.ScopeDir(connName, dbName)
	if err != nil {
		t.Fatalf("ScopeDir: %v", err)
	}

	bareDir := t.TempDir()
	runGit(t, bareDir, "init", "--bare", "--initial-branch=main")

	if err := Init(sourceScope); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := AddRemote(sourceScope, "origin", bareDir); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	if err := Push(sourceScope, "origin", "main", "snapshot push"); err != nil {
		t.Fatalf("Push: %v", err)
	}

	// Confirm the pushed content actually went through LFS, not plain git —
	// otherwise this test would "pass" even if .gitattributes/LFS tracking
	// silently broke, since git itself round-trips any file correctly.
	lsFiles := runGit(t, sourceScope, "lfs", "ls-files")
	if lsFiles == "" {
		t.Fatalf("git lfs ls-files reported no LFS-tracked files — objects were not pushed through LFS")
	}

	// Clone into a scope directory under a *different* connection name, so
	// restoring from it exercises Get/Restore against genuinely
	// clone-sourced files rather than the original in-process scope.
	clonedScope, err := snapshot.ScopeDir("remote-test-clone", dbName)
	if err != nil {
		t.Fatalf("ScopeDir (clone target): %v", err)
	}
	// ScopeDir's mere lookup already created the directory (and an
	// identity.json in it) as a side effect — Clone requires an empty
	// target, so remove that side effect before cloning into it.
	if err := os.RemoveAll(clonedScope); err != nil {
		t.Fatal(err)
	}
	if err := Clone(bareDir, clonedScope, "main"); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	restoreResult, err := snapshot.Restore(snapshot.RestoreOptions{
		SourceConnection: "remote-test-clone",
		SourceDatabase:   dbName,
		SnapshotID:       res.Summary.ID,
		TargetURI:        uri,
		TargetDatabase:   "remotedb_restored",
		Drop:             true,
	})
	if err != nil {
		t.Fatalf("Restore from cloned scope: %v", err)
	}
	if restoreResult.DocsWritten != 2 {
		t.Fatalf("restored %d docs from the cloned scope, want 2", restoreResult.DocsWritten)
	}

	verifyClient, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer verifyClient.Disconnect(context.Background())
	var doc bson.M
	if err := verifyClient.Database("remotedb_restored").Collection("widgets").FindOne(ctx, bson.D{{Key: "n", Value: 1}}).Decode(&doc); err != nil {
		t.Fatalf("finding restored doc: %v", err)
	}
	if doc["label"] != "widget-a" {
		t.Errorf("restored doc label = %v, want widget-a", doc["label"])
	}
}

func TestAddRemoteFallsBackToSetURL(t *testing.T) {
	requireGitAndLFS(t)
	scope := t.TempDir()
	if err := Init(scope); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := AddRemote(scope, "origin", "https://example.invalid/first.git"); err != nil {
		t.Fatalf("AddRemote (first): %v", err)
	}
	// Adding again under the same name must update the URL via `remote
	// set-url` rather than failing with "remote origin already exists".
	if err := AddRemote(scope, "origin", "https://example.invalid/second.git"); err != nil {
		t.Fatalf("AddRemote (second, same name): %v", err)
	}
	got := strings.TrimSpace(runGit(t, scope, "remote", "get-url", "origin"))
	if got != "https://example.invalid/second.git" {
		t.Fatalf("expected remote URL updated via set-url fallback, got %q", got)
	}
}

func TestPushToleratesNothingToCommit(t *testing.T) {
	requireGitAndLFS(t)
	scope := t.TempDir()
	if err := Init(scope); err != nil {
		t.Fatalf("Init: %v", err)
	}
	bareDir := t.TempDir()
	runGit(t, bareDir, "init", "--bare", "--initial-branch=main")
	if err := AddRemote(scope, "origin", bareDir); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scope, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Push(scope, "origin", "main", "first commit"); err != nil {
		t.Fatalf("Push (first, has changes): %v", err)
	}
	// Nothing changed since the last commit — Push must tolerate git's
	// "nothing to commit" instead of treating it as a failure.
	if err := Push(scope, "origin", "main", "second commit, no changes"); err != nil {
		t.Fatalf("Push (second, no changes): expected nothing-to-commit to be tolerated, got: %v", err)
	}
}

func TestPullMergesRemoteChanges(t *testing.T) {
	requireGitAndLFS(t)
	sourceScope := t.TempDir()
	if err := Init(sourceScope); err != nil {
		t.Fatalf("Init (source): %v", err)
	}
	bareDir := t.TempDir()
	runGit(t, bareDir, "init", "--bare", "--initial-branch=main")
	if err := AddRemote(sourceScope, "origin", bareDir); err != nil {
		t.Fatalf("AddRemote (source): %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceScope, "file.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Push(sourceScope, "origin", "main", "v1"); err != nil {
		t.Fatalf("Push (v1): %v", err)
	}

	// A second clone of the same remote, standing in for a different
	// machine that will later pull the source's subsequent change.
	pullerScope := t.TempDir()
	if err := os.RemoveAll(pullerScope); err != nil {
		t.Fatal(err)
	}
	if err := Clone(bareDir, pullerScope, "main"); err != nil {
		t.Fatalf("Clone (puller): %v", err)
	}

	if err := os.WriteFile(filepath.Join(sourceScope, "file.txt"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Push(sourceScope, "origin", "main", "v2"); err != nil {
		t.Fatalf("Push (v2): %v", err)
	}

	if err := Pull(pullerScope, "origin", "main"); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(pullerScope, "file.txt"))
	if err != nil {
		t.Fatalf("reading pulled file: %v", err)
	}
	if string(got) != "v2" {
		t.Fatalf("expected Pull to bring in the source's v2 content, got %q", got)
	}
}

func TestCloneRejectsNonEmptyDir(t *testing.T) {
	requireGitAndLFS(t)
	sourceScope := t.TempDir()
	if err := Init(sourceScope); err != nil {
		t.Fatalf("Init: %v", err)
	}
	bareDir := t.TempDir()
	runGit(t, bareDir, "init", "--bare", "--initial-branch=main")
	if err := AddRemote(sourceScope, "origin", bareDir); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceScope, "file.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Push(sourceScope, "origin", "main", "v1"); err != nil {
		t.Fatalf("Push: %v", err)
	}

	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "unexpected.txt"), []byte("pre-existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Clone(bareDir, target, "main"); err == nil {
		t.Fatal("expected Clone to reject a target directory with pre-existing content")
	}
}
