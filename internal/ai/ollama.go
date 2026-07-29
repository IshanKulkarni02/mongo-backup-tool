package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultOllamaHost is the local Ollama REST API's default address.
const DefaultOllamaHost = "http://localhost:11434"

// OllamaProvider talks to a local Ollama instance's /api/chat endpoint.
type OllamaProvider struct {
	Host   string // defaults to DefaultOllamaHost
	Model  string
	Client *http.Client
}

func (p *OllamaProvider) ID() string { return "ollama" }

func (p *OllamaProvider) host() string {
	if p.Host != "" {
		return p.Host
	}
	return DefaultOllamaHost
}

func (p *OllamaProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return http.DefaultClient
}

type ollamaChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type ollamaChatChunk struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done  bool   `json:"done"`
	Error string `json:"error"`
}

// Complete streams a chat completion. Ollama's /api/chat streams
// newline-delimited JSON objects (not SSE) — one per token/batch, the last
// with "done": true.
func (p *OllamaProvider) Complete(ctx context.Context, messages []Message) (<-chan StreamChunk, error) {
	body, err := json.Marshal(ollamaChatRequest{Model: p.Model, Messages: messages, Stream: true})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.host()+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("connecting to Ollama at %s: %w (is it running?)", p.host(), err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama: %s: %s", resp.Status, string(b))
	}

	out := make(chan StreamChunk)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			var chunk ollamaChatChunk
			if err := json.Unmarshal(line, &chunk); err != nil {
				send(ctx, out, StreamChunk{Err: err})
				return
			}
			if chunk.Error != "" {
				send(ctx, out, StreamChunk{Err: fmt.Errorf("ollama: %s", chunk.Error)})
				return
			}
			if !send(ctx, out, StreamChunk{Delta: chunk.Message.Content, Done: chunk.Done}) {
				return
			}
			if chunk.Done {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			send(ctx, out, StreamChunk{Err: err})
		}
	}()
	return out, nil
}

// IsRunning reports whether the local Ollama API answers.
func IsRunning(ctx context.Context, host string) bool {
	if host == "" {
		host = DefaultOllamaHost
	}
	client := http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, host+"/api/version", nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// OllamaModel is one locally-pulled model, as reported by /api/tags.
type OllamaModel struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// ListModels returns every model already pulled into the local Ollama
// instance.
func ListModels(ctx context.Context, host string) ([]OllamaModel, error) {
	if host == "" {
		host = DefaultOllamaHost
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, host+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama: %s: %s", resp.Status, string(b))
	}
	var out struct {
		Models []OllamaModel `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Models, nil
}

// PullProgress is one line of a model download's streamed status.
type PullProgress struct {
	Status    string
	Completed int64
	Total     int64
}

// PullModel downloads a model into the local Ollama instance, streaming
// progress lines to onProgress as they arrive.
func PullModel(ctx context.Context, host, model string, onProgress func(PullProgress)) error {
	if host == "" {
		host = DefaultOllamaHost
	}
	body, err := json.Marshal(map[string]string{"model": model})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to Ollama at %s: %w (is it running?)", host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama: %s: %s", resp.Status, string(b))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var chunk struct {
			Status    string `json:"status"`
			Error     string `json:"error"`
			Completed int64  `json:"completed"`
			Total     int64  `json:"total"`
		}
		if err := json.Unmarshal(line, &chunk); err != nil {
			return err
		}
		if chunk.Error != "" {
			return fmt.Errorf("ollama: %s", chunk.Error)
		}
		if onProgress != nil {
			onProgress(PullProgress{Status: chunk.Status, Completed: chunk.Completed, Total: chunk.Total})
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return scanner.Err()
}
