package listener

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestListenerCapturesRequest(t *testing.T) {
	var mu sync.Mutex
	var captured []Request
	l, err := Start(0, func(r Request) {
		mu.Lock()
		captured = append(captured, r)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer l.Stop(context.Background())

	resp, err := http.Post("http://"+l.Addr()+"/iclock/cdata?SN=12345", "application/json", strings.NewReader(`{"punch":"data"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "OK" {
		t.Fatalf("expected OK response, got %q", body)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(captured)
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for captured request")
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	req := captured[0]
	mu.Unlock()

	if req.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", req.Method)
	}
	if req.Path != "/iclock/cdata" {
		t.Errorf("expected path /iclock/cdata, got %s", req.Path)
	}
	if req.Query != "SN=12345" {
		t.Errorf("expected query SN=12345, got %s", req.Query)
	}
	if req.Body != `{"punch":"data"}` {
		t.Errorf("expected body to be captured, got %q", req.Body)
	}
	if req.ID == "" {
		t.Error("expected a non-empty request ID")
	}
}

func TestListenerStopClosesServer(t *testing.T) {
	l, err := Start(0, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	addr := l.Addr()

	if err := l.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	client := http.Client{Timeout: 500 * time.Millisecond}
	_, err = client.Get("http://" + addr + "/")
	if err == nil {
		t.Fatal("expected requests to fail after Stop")
	}
}

func TestListenerBodyCapEnforced(t *testing.T) {
	var mu sync.Mutex
	var captured *Request
	l, err := Start(0, func(r Request) {
		mu.Lock()
		captured = &r
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer l.Stop(context.Background())

	huge := strings.Repeat("x", bodyCap+1000)
	resp, err := http.Post("http://"+l.Addr()+"/", "text/plain", strings.NewReader(huge))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		c := captured
		mu.Unlock()
		if c != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for captured request")
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	bodyLen := len(captured.Body)
	mu.Unlock()
	if bodyLen > bodyCap {
		t.Fatalf("expected captured body to be capped at %d bytes, got %d", bodyCap, bodyLen)
	}
}
