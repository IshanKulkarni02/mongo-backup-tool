package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const defaultOpenAIBaseURL = "https://api.openai.com/v1"

// OpenAIProvider talks to OpenAI's chat completions API (BYOK). BaseURL can
// be overridden to point at any OpenAI-compatible endpoint.
type OpenAIProvider struct {
	APIKey  string
	Model   string
	BaseURL string // defaults to defaultOpenAIBaseURL
	Client  *http.Client
}

func (p *OpenAIProvider) ID() string { return "openai" }

func (p *OpenAIProvider) baseURL() string {
	if p.BaseURL != "" {
		return p.BaseURL
	}
	return defaultOpenAIBaseURL
}

func (p *OpenAIProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return http.DefaultClient
}

type openAIRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type openAIChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete streams a completion via OpenAI's SSE chat-completions API,
// terminated by a literal "data: [DONE]" line.
func (p *OpenAIProvider) Complete(ctx context.Context, messages []Message) (<-chan StreamChunk, error) {
	if p.APIKey == "" {
		return nil, fmt.Errorf("openai: no API key configured")
	}

	reqBody, err := json.Marshal(openAIRequest{Model: p.Model, Messages: messages, Stream: true})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL()+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai: %s: %s", resp.Status, string(b))
	}

	out := make(chan StreamChunk)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		err := scanSSE(resp.Body, func(payload string) (bool, error) {
			if payload == "[DONE]" {
				send(ctx, out, StreamChunk{Done: true})
				return true, nil
			}
			var chunk openAIChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				return false, err
			}
			if chunk.Error.Message != "" {
				send(ctx, out, StreamChunk{Err: fmt.Errorf("openai: %s", chunk.Error.Message)})
				return true, nil
			}
			if len(chunk.Choices) > 0 {
				if !send(ctx, out, StreamChunk{Delta: chunk.Choices[0].Delta.Content}) {
					return true, nil
				}
			}
			return false, nil
		})
		if err != nil {
			send(ctx, out, StreamChunk{Err: err})
		}
	}()
	return out, nil
}
