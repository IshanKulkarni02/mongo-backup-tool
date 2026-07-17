// Package ai talks to language model backends — a local Ollama instance or
// a bring-your-own-key cloud provider (Anthropic, OpenAI) — through raw
// REST calls (no vendor SDKs), so the desktop app's AI features work
// against whichever the user has configured. Every provider streams
// incremental text through the same Provider interface.
package ai

import (
	"bufio"
	"context"
	"io"
	"strings"
)

// Message is one turn in a chat-style completion request.
type Message struct {
	Role    string `json:"role"` // "system" | "user" | "assistant"
	Content string `json:"content"`
}

// StreamChunk is one piece of a streaming completion. A chunk with Err set
// is always the last one sent; the channel is closed immediately after.
type StreamChunk struct {
	Delta string
	Done  bool
	Err   error
}

// Provider generates a completion from a list of messages, streaming
// incremental text via the returned channel. The channel is always closed
// when the stream ends, whether by completion, error, or context
// cancellation.
type Provider interface {
	ID() string
	Complete(ctx context.Context, messages []Message) (<-chan StreamChunk, error)
}

// CollectText drains a Provider's stream into a single string — for
// callers (NL→SQL, error-fix, mocking) that want the final text rather
// than incremental tokens.
func CollectText(ch <-chan StreamChunk) (string, error) {
	var sb strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			return "", chunk.Err
		}
		sb.WriteString(chunk.Delta)
	}
	return sb.String(), nil
}

// send delivers a chunk, respecting context cancellation so a stalled
// consumer (or a canceled request) can't leak the producing goroutine.
func send(ctx context.Context, out chan<- StreamChunk, chunk StreamChunk) bool {
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}

// scanSSE reads a Server-Sent-Events body (Anthropic, OpenAI) and invokes
// onData with each event's payload (the text after "data:", trimmed).
// Non-data lines (event:, id:, blank keep-alives) are skipped. onData
// returns done=true to stop early (e.g. on a stream terminator).
func scanSSE(body io.Reader, onData func(payload string) (done bool, err error)) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		done, err := onData(payload)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return scanner.Err()
}
