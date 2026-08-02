package config

import (
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/secrets"
)

func TestSetAIAPIKeyUsesKeyringWhenAvailable(t *testing.T) {
	secrets.MockInit()
	cfg := &Config{}

	if err := SetAIAPIKey(cfg, "sk-test-123"); err != nil {
		t.Fatalf("SetAIAPIKey: %v", err)
	}
	if !cfg.AI.HasAPIKey {
		t.Fatal("expected HasAPIKey to be true")
	}
	if cfg.AI.APIKeyPlaintext != "" {
		t.Fatalf("expected no plaintext fallback when a keyring is available, got %q", cfg.AI.APIKeyPlaintext)
	}

	got, err := AIAPIKey(cfg)
	if err != nil {
		t.Fatalf("AIAPIKey: %v", err)
	}
	if got != "sk-test-123" {
		t.Fatalf("AIAPIKey = %q, want sk-test-123", got)
	}
}

// TestSetAIAPIKeyFallsBackToPlaintextWithoutKeyring is the regression
// test for #80: on a machine with no working OS keyring, saving an API
// key must not fail outright (as SetAIAPIKey used to, by calling
// secrets.Set unconditionally and propagating its error) — it should
// degrade to the same plaintext-in-config.json fallback connection
// credentials already use.
func TestSetAIAPIKeyFallsBackToPlaintextWithoutKeyring(t *testing.T) {
	secrets.MockUnavailable()
	t.Cleanup(secrets.ResetForTesting)
	cfg := &Config{}

	if err := SetAIAPIKey(cfg, "sk-test-456"); err != nil {
		t.Fatalf("SetAIAPIKey should succeed via the plaintext fallback, got: %v", err)
	}
	if !cfg.AI.HasAPIKey {
		t.Fatal("expected HasAPIKey to be true")
	}
	if cfg.AI.APIKeyPlaintext != "sk-test-456" {
		t.Fatalf("expected the key stored in the plaintext fallback field, got %q", cfg.AI.APIKeyPlaintext)
	}

	got, err := AIAPIKey(cfg)
	if err != nil {
		t.Fatalf("AIAPIKey: %v", err)
	}
	if got != "sk-test-456" {
		t.Fatalf("AIAPIKey = %q, want sk-test-456", got)
	}
}

func TestDeleteAIAPIKeyClearsPlaintextFallback(t *testing.T) {
	secrets.MockUnavailable()
	t.Cleanup(secrets.ResetForTesting)
	cfg := &Config{}
	if err := SetAIAPIKey(cfg, "sk-test-789"); err != nil {
		t.Fatalf("SetAIAPIKey: %v", err)
	}

	if err := DeleteAIAPIKey(cfg); err != nil {
		t.Fatalf("DeleteAIAPIKey: %v", err)
	}
	if cfg.AI.HasAPIKey {
		t.Fatal("expected HasAPIKey to be false after delete")
	}
	if cfg.AI.APIKeyPlaintext != "" {
		t.Fatalf("expected the plaintext fallback cleared, got %q", cfg.AI.APIKeyPlaintext)
	}
}

// TestSetAIAPIKeyPersistsAcrossSaveLoad confirms the plaintext fallback
// actually round-trips through the real Save/Load path (not just the
// in-memory Config), since that's how the desktop app's SaveAISettings
// actually uses it.
func TestSetAIAPIKeyPersistsAcrossSaveLoad(t *testing.T) {
	withTempConfigDir(t)
	secrets.MockUnavailable()
	t.Cleanup(secrets.ResetForTesting)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := SetAIAPIKey(cfg, "sk-persisted"); err != nil {
		t.Fatalf("SetAIAPIKey: %v", err)
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatalf("Load (after save): %v", err)
	}
	if !reloaded.AI.HasAPIKey {
		t.Fatal("expected HasAPIKey to survive a save/load round trip")
	}
	got, err := AIAPIKey(reloaded)
	if err != nil {
		t.Fatalf("AIAPIKey: %v", err)
	}
	if got != "sk-persisted" {
		t.Fatalf("AIAPIKey after reload = %q, want sk-persisted", got)
	}
}
