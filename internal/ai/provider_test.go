package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOllamaProviderStreamsAndAssemblesText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		lines := []string{
			`{"message":{"role":"assistant","content":"Hello"},"done":false}`,
			`{"message":{"role":"assistant","content":", world"},"done":false}`,
			`{"message":{"role":"assistant","content":""},"done":true}`,
		}
		for _, line := range lines {
			fmt.Fprintln(w, line)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p := &OllamaProvider{Host: srv.URL, Model: "test-model"}
	ch, err := p.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	text, err := CollectText(ch)
	if err != nil {
		t.Fatalf("CollectText: %v", err)
	}
	if text != "Hello, world" {
		t.Fatalf("expected assembled text %q, got %q", "Hello, world", text)
	}
}

func TestOllamaProviderPropagatesErrorChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"error":"model not found"}`)
	}))
	defer srv.Close()

	p := &OllamaProvider{Host: srv.URL, Model: "missing"}
	ch, err := p.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	_, err = CollectText(ch)
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("expected model-not-found error, got %v", err)
	}
}

func TestOllamaProviderNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	}))
	defer srv.Close()

	p := &OllamaProvider{Host: srv.URL, Model: "x"}
	_, err := p.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err == nil {
		t.Fatal("expected an error for a non-200 response")
	}
}

func TestOllamaProviderRespectsContextCancellation(t *testing.T) {
	blockUntil := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		fmt.Fprintln(w, `{"message":{"content":"partial"},"done":false}`)
		flusher.Flush()
		<-blockUntil // simulate a slow/stalled upstream
	}))
	defer srv.Close()
	defer close(blockUntil)

	ctx, cancel := context.WithCancel(context.Background())
	p := &OllamaProvider{Host: srv.URL, Model: "x"}
	ch, err := p.Complete(ctx, []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	<-ch // read the first chunk
	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			// draining until closed is fine either way
			for range ch {
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not close promptly after context cancellation")
	}
}

func TestAnthropicProviderStreamsSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key header to be sent")
		}
		flusher := w.(http.Flusher)
		events := []string{
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hel"}}`,
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"lo"}}`,
			`data: {"type":"message_stop"}`,
		}
		for _, ev := range events {
			fmt.Fprintln(w, ev)
			fmt.Fprintln(w)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p := &AnthropicProvider{APIKey: "test-key", Model: "claude-x", BaseURL: srv.URL}
	ch, err := p.Complete(context.Background(), []Message{
		{Role: "system", Content: "be terse"},
		{Role: "user", Content: "hi"},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	text, err := CollectText(ch)
	if err != nil {
		t.Fatalf("CollectText: %v", err)
	}
	if text != "Hello" {
		t.Fatalf("expected %q, got %q", "Hello", text)
	}
}

func TestAnthropicProviderPropagatesErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `data: {"type":"error","error":{"message":"rate limited"}}`)
	}))
	defer srv.Close()

	p := &AnthropicProvider{APIKey: "k", Model: "m", BaseURL: srv.URL}
	ch, err := p.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	_, err = CollectText(ch)
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("expected rate-limited error, got %v", err)
	}
}

func TestAnthropicProviderRequiresAPIKey(t *testing.T) {
	p := &AnthropicProvider{Model: "m"}
	if _, err := p.Complete(context.Background(), nil); err == nil {
		t.Fatal("expected an error when no API key is set")
	}
}

func TestOpenAIProviderStreamsSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Authorization header to be sent")
		}
		flusher := w.(http.Flusher)
		events := []string{
			`data: {"choices":[{"delta":{"content":"Hi "}}]}`,
			`data: {"choices":[{"delta":{"content":"there"}}]}`,
			`data: [DONE]`,
		}
		for _, ev := range events {
			fmt.Fprintln(w, ev)
			fmt.Fprintln(w)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p := &OpenAIProvider{APIKey: "test-key", Model: "gpt-x", BaseURL: srv.URL}
	ch, err := p.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	text, err := CollectText(ch)
	if err != nil {
		t.Fatalf("CollectText: %v", err)
	}
	if text != "Hi there" {
		t.Fatalf("expected %q, got %q", "Hi there", text)
	}
}

func TestOpenAIProviderPropagatesErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `data: {"error":{"message":"invalid api key"}}`)
	}))
	defer srv.Close()

	p := &OpenAIProvider{APIKey: "bad", Model: "m", BaseURL: srv.URL}
	ch, err := p.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	_, err = CollectText(ch)
	if err == nil || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("expected invalid-api-key error, got %v", err)
	}
}

func TestOpenAIProviderRequiresAPIKey(t *testing.T) {
	p := &OpenAIProvider{Model: "m"}
	if _, err := p.Complete(context.Background(), nil); err == nil {
		t.Fatal("expected an error when no API key is set")
	}
}

func TestNewProviderDispatch(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
		wantID  string
	}{
		{"default is ollama", Config{Model: "llama"}, false, "ollama"},
		{"ollama needs a model", Config{ProviderID: "ollama"}, true, ""},
		{"anthropic needs a key", Config{ProviderID: "anthropic", Model: "claude"}, true, ""},
		{"anthropic ok", Config{ProviderID: "anthropic", Model: "claude", APIKey: "k"}, false, "anthropic"},
		{"openai needs a key", Config{ProviderID: "openai", Model: "gpt"}, true, ""},
		{"openai ok", Config{ProviderID: "openai", Model: "gpt", APIKey: "k"}, false, "openai"},
		{"unknown provider", Config{ProviderID: "bogus"}, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := NewProvider(c.cfg)
			if c.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.ID() != c.wantID {
				t.Fatalf("expected provider ID %q, got %q", c.wantID, p.ID())
			}
		})
	}
}

func TestPromptBuildersIncludeContext(t *testing.T) {
	msgs := BuildNLToSQLPrompt("postgres", []string{"CREATE TABLE users (id INT)"}, "show all users")
	if len(msgs) != 2 || !strings.Contains(msgs[0].Content, "CREATE TABLE users") {
		t.Fatalf("expected schema in system prompt, got %+v", msgs)
	}
	if msgs[1].Content != "show all users" {
		t.Fatalf("expected user request preserved verbatim, got %q", msgs[1].Content)
	}

	aggMsgs := BuildNLToAggregationPrompt("orders", `{"userId": "ObjectId", "total": 12.5}`, "total sales by user")
	if !strings.Contains(aggMsgs[0].Content, "orders") {
		t.Fatalf("expected collection name in system prompt, got %+v", aggMsgs)
	}

	fixMsgs := BuildErrorFixPrompt("mysql", "SELECT * FROM usres", "table usres doesn't exist", []string{"CREATE TABLE users (id INT)"})
	if !strings.Contains(fixMsgs[1].Content, "doesn't exist") {
		t.Fatalf("expected error message included, got %+v", fixMsgs)
	}

	mockMsgs := BuildMockDataPrompt("sqlite", "users", "CREATE TABLE users (id INT, email TEXT)", 5)
	if !strings.Contains(mockMsgs[0].Content, "5") {
		t.Fatalf("expected row count in prompt, got %+v", mockMsgs)
	}
}
