package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultAnthropicBaseURL = "https://api.anthropic.com/v1"

// AnthropicProvider talks to Anthropic's Messages API (BYOK — the user
// supplies their own API key).
type AnthropicProvider struct {
	APIKey    string
	Model     string
	BaseURL   string // defaults to defaultAnthropicBaseURL
	MaxTokens int    // defaults to 4096
	Client    *http.Client
}

func (p *AnthropicProvider) ID() string { return "anthropic" }

func (p *AnthropicProvider) baseURL() string {
	if p.BaseURL != "" {
		return p.BaseURL
	}
	return defaultAnthropicBaseURL
}

func (p *AnthropicProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return http.DefaultClient
}

type anthropicRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []Message `json:"messages"`
	Stream    bool      `json:"stream"`
}

// anthropicEvent covers the SSE event shapes this provider cares about:
// content_block_delta (text), message_stop (terminator), and error.
type anthropicEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete streams a completion via Anthropic's SSE Messages API. System
// messages are collected and sent via the top-level "system" field, since
// Anthropic doesn't accept a "system" role inside the messages array.
func (p *AnthropicProvider) Complete(ctx context.Context, messages []Message) (<-chan StreamChunk, error) {
	if p.APIKey == "" {
		return nil, fmt.Errorf("anthropic: no API key configured")
	}

	var system strings.Builder
	rest := make([]Message, 0, len(messages))
	for _, m := range messages {
		if m.Role == "system" {
			if system.Len() > 0 {
				system.WriteByte('\n')
			}
			system.WriteString(m.Content)
			continue
		}
		rest = append(rest, m)
	}

	maxTokens := p.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}
	reqBody, err := json.Marshal(anthropicRequest{
		Model: p.Model, MaxTokens: maxTokens, System: system.String(), Messages: rest, Stream: true,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL()+"/messages", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic: %s: %s", resp.Status, string(b))
	}

	out := make(chan StreamChunk)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		err := scanSSE(resp.Body, func(payload string) (bool, error) {
			var ev anthropicEvent
			if err := json.Unmarshal([]byte(payload), &ev); err != nil {
				return false, err
			}
			switch ev.Type {
			case "content_block_delta":
				if !send(ctx, out, StreamChunk{Delta: ev.Delta.Text}) {
					return true, nil
				}
			case "error":
				send(ctx, out, StreamChunk{Err: fmt.Errorf("anthropic: %s", ev.Error.Message)})
				return true, nil
			case "message_stop":
				send(ctx, out, StreamChunk{Done: true})
				return true, nil
			}
			return false, nil
		})
		if err != nil {
			send(ctx, out, StreamChunk{Err: err})
		}
	}()
	return out, nil
}
