package ai

import "fmt"

// Config selects and configures one provider.
type Config struct {
	ProviderID string // "ollama" (default) | "anthropic" | "openai"
	Model      string
	APIKey     string // BYOK providers only
	OllamaHost string // optional override, defaults to DefaultOllamaHost
}

// NewProvider builds the Provider a Config describes.
func NewProvider(cfg Config) (Provider, error) {
	switch cfg.ProviderID {
	case "", "ollama":
		if cfg.Model == "" {
			return nil, fmt.Errorf("ollama: no model selected")
		}
		return &OllamaProvider{Host: cfg.OllamaHost, Model: cfg.Model}, nil
	case "anthropic":
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("anthropic: no API key configured")
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("anthropic: no model selected")
		}
		return &AnthropicProvider{APIKey: cfg.APIKey, Model: cfg.Model}, nil
	case "openai":
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("openai: no API key configured")
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("openai: no model selected")
		}
		return &OpenAIProvider{APIKey: cfg.APIKey, Model: cfg.Model}, nil
	default:
		return nil, fmt.Errorf("unknown AI provider %q", cfg.ProviderID)
	}
}
