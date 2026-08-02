package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
)

// withTempConfigDir points config.Dir() at a fresh temp directory for the
// duration of the test, so Load/Save never touch the real user config.
func withTempConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
}

// resetConnAddFlags clears the package-level flag vars connectionAddCmd's
// RunE reads, since they're shared state across table-driven test cases.
func resetConnAddFlags() {
	connAddURI = ""
	connAddEngine = ""
	connAddEnvironment = ""
	connAddReadOnly = false
	connAddSSHHost = ""
	connAddSSHUser = ""
	connAddSSHPassword = ""
	connAddSSHKey = ""
	connAddTenantSessionVar = ""
}

func TestConnectionAddRejectsBadEngine(t *testing.T) {
	withTempConfigDir(t)
	resetConnAddFlags()
	connAddURI = "mongodb://localhost:27017"
	connAddEngine = "not-a-real-engine"

	if err := connectionAddCmd.RunE(connectionAddCmd, []string{"test"}); err == nil {
		t.Fatal("expected an error for an unknown engine")
	}
}

func TestConnectionAddRejectsBadEnvironment(t *testing.T) {
	withTempConfigDir(t)
	resetConnAddFlags()
	connAddURI = "mongodb://localhost:27017"
	connAddEnvironment = "production" // must be dev/staging/prod, not this

	if err := connectionAddCmd.RunE(connectionAddCmd, []string{"test"}); err == nil {
		t.Fatal("expected an error for an invalid environment")
	}
}

func TestConnectionAddRejectsSSHHostWithoutCredential(t *testing.T) {
	withTempConfigDir(t)
	resetConnAddFlags()
	connAddURI = "postgres://localhost:5432/app"
	connAddEngine = "postgres"
	connAddSSHHost = "bastion.example.com"

	if err := connectionAddCmd.RunE(connectionAddCmd, []string{"test"}); err == nil {
		t.Fatal("expected an error for an SSH host with no password or key")
	}
}

func TestConnectionAddRejectsBadTenantVarName(t *testing.T) {
	withTempConfigDir(t)
	resetConnAddFlags()
	connAddURI = "postgres://localhost:5432/app"
	connAddEngine = "postgres"
	connAddTenantSessionVar = "app.current_tenant; DROP TABLE users"

	if err := connectionAddCmd.RunE(connectionAddCmd, []string{"test"}); err == nil {
		t.Fatal("expected an error for an invalid tenant session variable name")
	}
}

func TestConnectionAddAcceptsValidSQLConnection(t *testing.T) {
	withTempConfigDir(t)
	resetConnAddFlags()
	connAddURI = "postgres://user:pass@localhost:5432/app"
	connAddEngine = "postgres"
	connAddEnvironment = "staging"
	connAddTenantSessionVar = "app.current_tenant"

	if err := connectionAddCmd.RunE(connectionAddCmd, []string{"pg-test"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	conn, ok := cfg.Find("pg-test")
	if !ok {
		t.Fatal("connection not saved")
	}
	if conn.Engine != "postgres" || conn.Environment != "staging" || conn.TenantSessionVar != "app.current_tenant" {
		t.Fatalf("unexpected saved connection: %+v", conn)
	}
}

func TestConnectionAddPreservesTenantValueAcrossUpdate(t *testing.T) {
	withTempConfigDir(t)
	resetConnAddFlags()
	connAddURI = "postgres://localhost:5432/app"
	connAddEngine = "postgres"
	connAddTenantSessionVar = "app.current_tenant"
	if err := connectionAddCmd.RunE(connectionAddCmd, []string{"pg-test"}); err != nil {
		t.Fatalf("initial RunE: %v", err)
	}

	// Simulate SwitchTenant (desktop-side) setting a tenant value directly.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	conn, _ := cfg.Find("pg-test")
	conn.TenantValue = "acme"
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Re-running connection add (e.g. to change the URI) shouldn't clear it.
	resetConnAddFlags()
	connAddURI = "postgres://localhost:5432/app2"
	connAddEngine = "postgres"
	connAddTenantSessionVar = "app.current_tenant"
	if err := connectionAddCmd.RunE(connectionAddCmd, []string{"pg-test"}); err != nil {
		t.Fatalf("update RunE: %v", err)
	}

	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	conn, ok := cfg.Find("pg-test")
	if !ok {
		t.Fatal("connection missing after update")
	}
	if conn.TenantValue != "acme" {
		t.Fatalf("expected TenantValue to be preserved, got %q", conn.TenantValue)
	}
	if conn.URI != "postgres://localhost:5432/app2" {
		t.Fatalf("expected URI to be updated, got %q", conn.URI)
	}
}

func TestConnectionAddSSHKeyFromFile(t *testing.T) {
	withTempConfigDir(t)
	resetConnAddFlags()
	keyPath := t.TempDir() + "/id_test"
	pemContent := "-----BEGIN OPENSSH PRIVATE KEY-----\nfake-key-content\n-----END OPENSSH PRIVATE KEY-----\n"
	if err := os.WriteFile(keyPath, []byte(pemContent), 0o600); err != nil {
		t.Fatalf("seeding key file: %v", err)
	}

	connAddURI = "postgres://localhost:5432/app"
	connAddEngine = "postgres"
	connAddSSHHost = "bastion.example.com"
	connAddSSHKey = keyPath

	if err := connectionAddCmd.RunE(connectionAddCmd, []string{"pg-ssh"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	conn, ok := cfg.Find("pg-ssh")
	if !ok {
		t.Fatal("connection not saved")
	}
	if conn.SSHPrivateKey != pemContent {
		t.Fatalf("expected SSH private key file contents to be read in, got %q", conn.SSHPrivateKey)
	}
}

// TestConnectionTestWorksForNonMongoEngine is the regression test for
// #57: `connection test` unconditionally called mongotools.TestConnection,
// which dials with the Mongo driver's ApplyURI — and ApplyURI rejects any
// URI whose scheme isn't mongodb/mongodb+srv. Every non-Mongo connection
// (postgres, mysql, sqlite — all explicitly supported by `connection
// add --engine`) would fail `connection test` with a confusing
// driver-level error instead of actually being tested. SQLite is used
// here because it needs no external server: cfg.URI is just a file path,
// so this exercises the real engine.Lookup -> Open -> Ping ->
// ListDatabases path end-to-end with no mocking.
func TestConnectionTestWorksForNonMongoEngine(t *testing.T) {
	withTempConfigDir(t)
	resetConnAddFlags()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	connAddURI = dbPath
	connAddEngine = "sqlite"
	if err := connectionAddCmd.RunE(connectionAddCmd, []string{"sqlite-test"}); err != nil {
		t.Fatalf("connection add: %v", err)
	}

	if err := connectionTestCmd.RunE(connectionTestCmd, []string{"sqlite-test"}); err != nil {
		t.Fatalf("connection test failed for a non-Mongo (sqlite) engine: %v", err)
	}
}

// TestConnectionTestReportsUnknownConnection confirms `connection test`
// still reports a clear error for a name that was never saved, rather
// than reaching the engine-dispatch code with a nil connection.
func TestConnectionTestReportsUnknownConnection(t *testing.T) {
	withTempConfigDir(t)

	if err := connectionTestCmd.RunE(connectionTestCmd, []string{"does-not-exist"}); err == nil {
		t.Fatal("expected an error for a connection that was never saved")
	}
}

// TestConnectionAddSSHKeyFileReadErrorSurfaced is the regression test for
// #58: previously, if --ssh-key was a genuine file path but reading it
// failed for any reason (typo, permissions, a missing file), the error
// was silently swallowed and the literal path string was saved as if it
// were the key's PEM content — producing an unusable saved key with no
// indication anything went wrong until a later SSH tunnel attempt failed
// to parse it. A read failure on a path-shaped value must now surface as
// an error from connection add itself.
func TestConnectionAddSSHKeyFileReadErrorSurfaced(t *testing.T) {
	withTempConfigDir(t)
	resetConnAddFlags()
	missingPath := t.TempDir() + "/does-not-exist"

	connAddURI = "postgres://localhost:5432/app"
	connAddEngine = "postgres"
	connAddSSHHost = "bastion.example.com"
	connAddSSHKey = missingPath

	err := connectionAddCmd.RunE(connectionAddCmd, []string{"pg-ssh-missing"})
	if err == nil {
		t.Fatal("expected an error for an unreadable --ssh-key file path")
	}

	cfg, loadErr := config.Load()
	if loadErr != nil {
		t.Fatalf("Load: %v", loadErr)
	}
	if _, ok := cfg.Find("pg-ssh-missing"); ok {
		t.Fatal("expected the connection not to be saved when --ssh-key couldn't be read")
	}
}

// TestConnectionAddSSHKeyLiteralPEMContent confirms literal PEM content
// passed directly (not a file path) is saved as-is, with no attempt to
// read it as a file — the scripted/embedded use case this behavior is
// meant to support.
func TestConnectionAddSSHKeyLiteralPEMContent(t *testing.T) {
	withTempConfigDir(t)
	resetConnAddFlags()
	pemContent := "-----BEGIN OPENSSH PRIVATE KEY-----\nfake-key-content\n-----END OPENSSH PRIVATE KEY-----\n"

	connAddURI = "postgres://localhost:5432/app"
	connAddEngine = "postgres"
	connAddSSHHost = "bastion.example.com"
	connAddSSHKey = pemContent

	if err := connectionAddCmd.RunE(connectionAddCmd, []string{"pg-ssh-literal"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	conn, ok := cfg.Find("pg-ssh-literal")
	if !ok {
		t.Fatal("connection not saved")
	}
	if conn.SSHPrivateKey != pemContent {
		t.Fatalf("expected literal PEM content to be saved unchanged, got %q", conn.SSHPrivateKey)
	}
}
