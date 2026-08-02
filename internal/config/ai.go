package config

import "github.com/IshanKulkarni02/dbhelm/internal/secrets"

const aiAPIKeySecretKey = "ai:api-key"

// AISettings persists which AI provider/model the desktop app should use.
// A BYOK provider's API key is stored in the OS keychain when available
// (see SetAIAPIKey/AIAPIKey) — HasAPIKey only records whether one has been
// set, so config.json never holds it in plaintext in that case. When no
// keyring is available, the key falls back to APIKeyPlaintext here in
// config.json instead (protected only by the file's owner-only
// permissions) — the same fallback connection credentials already use in
// stripCredentials, so the AI feature degrades gracefully instead of
// failing outright on a machine without a working keyring.
type AISettings struct {
	ProviderID string `json:"providerId,omitempty"`
	Model      string `json:"model,omitempty"`
	OllamaHost string `json:"ollamaHost,omitempty"`
	HasAPIKey  bool   `json:"hasApiKey,omitempty"`
	// APIKeyPlaintext holds the API key when SetAIAPIKey was called with
	// no keyring available. Empty whenever the key is in the keychain
	// instead.
	APIKeyPlaintext string `json:"apiKeyPlaintext,omitempty"`
}

// SetAIAPIKey stores a BYOK provider's API key: in the system keychain
// when available, or in cfg.AI.APIKeyPlaintext (persisted to config.json
// by the caller's subsequent Save) when it isn't. The caller must Save
// cfg afterward for a plaintext-fallback write to actually persist.
func SetAIAPIKey(cfg *Config, key string) error {
	if secrets.Available() {
		if err := secrets.Set(aiAPIKeySecretKey, key); err != nil {
			return err
		}
		cfg.AI.APIKeyPlaintext = ""
		cfg.AI.HasAPIKey = true
		return nil
	}
	cfg.AI.APIKeyPlaintext = key
	cfg.AI.HasAPIKey = true
	return nil
}

// AIAPIKey retrieves the stored API key: from cfg.AI.APIKeyPlaintext if
// that's how it was stored (no keyring available at Set time), otherwise
// from the system keychain. Returns secrets.ErrNotFound if neither holds
// one.
func AIAPIKey(cfg *Config) (string, error) {
	if cfg.AI.APIKeyPlaintext != "" {
		return cfg.AI.APIKeyPlaintext, nil
	}
	return secrets.Get(aiAPIKeySecretKey)
}

// DeleteAIAPIKey removes the stored API key, wherever it was held. The
// caller must Save cfg afterward to persist the plaintext-fallback field
// being cleared.
func DeleteAIAPIKey(cfg *Config) error {
	cfg.AI.APIKeyPlaintext = ""
	cfg.AI.HasAPIKey = false
	return secrets.Delete(aiAPIKeySecretKey)
}
