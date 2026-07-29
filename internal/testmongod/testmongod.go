// Package testmongod starts throwaway local mongod instances for
// integration tests across this module (internal/snapshot, internal/remote,
// ...). It is only ever imported from _test.go files, but lives as a
// regular package (not itself a _test.go file) so it can be shared across
// package boundaries — Go doesn't allow importing another package's test
// files directly.
package testmongod

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Find locates a mongod binary for integration tests: PATH first, falling
// back to ~/.local/bin (where the manually-managed binary-download recovery
// path — the one internal/depmanager's fallback mirrors for
// mongodump/mongorestore — places it), so these tests run seamlessly in a
// dev environment that followed that same recovery path without requiring
// mongod to be permanently added to PATH.
func Find() string {
	if p, err := exec.LookPath("mongod"); err == nil {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidate := filepath.Join(home, ".local", "bin", "mongod")
	if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
		return candidate
	}
	return ""
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// Start starts a throwaway mongod instance for integration tests and
// returns its connection URI, skipping the test cleanly (not failing) if
// mongod isn't available anywhere Find looks — the release-gate requirement
// is that these tests degrade gracefully without the external dependency,
// not that they're unconditionally required to run everywhere.
//
// If replicaSetName is non-empty, the instance is started as a single-node
// replica set and initiated (via replSetInitiate), then this blocks until it
// reports itself as primary — the actual replica-set path Create() uses for
// readConcern:snapshot, not just the standalone fallback. The mongod process
// is managed directly via exec.Command (no --fork), and torn down in
// t.Cleanup by killing that exact *os.Process — never a broad pkill, per
// this project's hard rule on killing test MongoDB processes.
func Start(t *testing.T, replicaSetName string) (uri string) {
	t.Helper()
	bin := Find()
	if bin == "" {
		t.Skip("mongod not found on PATH or ~/.local/bin — skipping integration test")
	}

	dbpath := t.TempDir()
	port := freeTCPPort(t)
	logPath := filepath.Join(dbpath, "mongod.log")

	args := []string{
		"--dbpath", dbpath,
		"--port", fmt.Sprint(port),
		"--bind_ip", "127.0.0.1",
		"--logpath", logPath,
		"--noauth",
	}
	if replicaSetName != "" {
		args = append(args, "--replSet", replicaSetName)
	}

	cmd := exec.Command(bin, args...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting mongod: %v", err)
	}
	proc := cmd.Process
	t.Cleanup(func() {
		if proc == nil {
			return
		}
		proc.Kill() // exact PID only, never a name-matching pkill
		cmd.Wait()
	})

	uri = fmt.Sprintf("mongodb://127.0.0.1:%d/?directConnection=true&serverSelectionTimeoutMS=10000", port)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	waitForMongod(t, ctx, uri, logPath)

	if replicaSetName != "" {
		initiateReplicaSet(t, ctx, uri, replicaSetName, port)
		uri = fmt.Sprintf("mongodb://127.0.0.1:%d/?replicaSet=%s&serverSelectionTimeoutMS=10000", port, replicaSetName)
		waitForPrimary(t, ctx, uri)
	}

	return uri
}

func waitForMongod(t *testing.T, ctx context.Context, uri, logPath string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		func() {
			pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			client, err := mongo.Connect(options.Client().ApplyURI(uri))
			if err != nil {
				lastErr = err
				return
			}
			defer client.Disconnect(context.Background())
			lastErr = client.Ping(pingCtx, nil)
		}()
		if lastErr == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	logTail, _ := os.ReadFile(logPath)
	t.Fatalf("mongod never became reachable at %s: %v\nlog:\n%s", uri, lastErr, truncateLog(logTail))
}

func truncateLog(b []byte) []byte {
	const maxLen = 4000
	if len(b) <= maxLen {
		return b
	}
	return b[len(b)-maxLen:]
}

func initiateReplicaSet(t *testing.T, ctx context.Context, uri, name string, port int) {
	t.Helper()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connecting to initiate replica set: %v", err)
	}
	defer client.Disconnect(context.Background())

	cfg := bson.M{
		"_id": name,
		"members": bson.A{
			bson.M{"_id": 0, "host": fmt.Sprintf("127.0.0.1:%d", port)},
		},
	}
	cmd := bson.D{{Key: "replSetInitiate", Value: cfg}}
	if err := client.Database("admin").RunCommand(ctx, cmd).Err(); err != nil {
		t.Fatalf("replSetInitiate: %v", err)
	}
}

func waitForPrimary(t *testing.T, ctx context.Context, uri string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = func() error {
			pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			client, err := mongo.Connect(options.Client().ApplyURI(uri))
			if err != nil {
				return err
			}
			defer client.Disconnect(context.Background())
			var result bson.M
			if err := client.Database("admin").RunCommand(pingCtx, bson.D{{Key: "hello", Value: 1}}).Decode(&result); err != nil {
				return err
			}
			if ok, _ := result["isWritablePrimary"].(bool); ok {
				return nil
			}
			return fmt.Errorf("not yet primary: %+v", result)
		}()
		if lastErr == nil {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("replica set never reached primary at %s: %v", uri, lastErr)
}
