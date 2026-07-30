package cmd

import (
	"os"
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
