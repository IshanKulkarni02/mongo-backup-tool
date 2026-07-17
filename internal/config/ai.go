package config

import "github.com/IshanKulkarni02/mongo-backup-tool/internal/secrets"

const aiAPIKeySecretKey = "ai:api-key"

// AISettings persists which AI provider/model the desktop app should use.
// A BYOK provider's API key is stored in the OS keychain (see
// SetAIAPIKey/AIAPIKey) — HasAPIKey only records whether one has been set,
// so config.json never holds it in plaintext.
type AISettings struct {
	ProviderID string `json:"providerId,omitempty"`
	Model      string `json:"model,omitempty"`
	OllamaHost string `json:"ollamaHost,omitempty"`
	HasAPIKey  bool   `json:"hasApiKey,omitempty"`
}

// SetAIAPIKey stores a BYOK provider's API key in the system keychain.
func SetAIAPIKey(key string) error {
	return secrets.Set(aiAPIKeySecretKey, key)
}

// AIAPIKey retrieves the stored API key, or secrets.ErrNotFound if none.
func AIAPIKey() (string, error) {
	return secrets.Get(aiAPIKeySecretKey)
}

// DeleteAIAPIKey removes the stored API key.
func DeleteAIAPIKey() error {
	return secrets.Delete(aiAPIKeySecretKey)
}
