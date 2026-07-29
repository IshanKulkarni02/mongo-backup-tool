package listener

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func authedPost(l *Listener, url, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set(TokenHeader, l.Token())
	return http.DefaultClient.Do(req)
}

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

	resp, err := authedPost(l, "http://"+l.Addr()+"/iclock/cdata?SN=12345", "application/json", strings.NewReader(`{"punch":"data"}`))
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

// TestListenerBindsLoopbackOnly guards against #43: the listener must
// never be reachable from another host, regardless of the machine's
// firewall/NAT configuration.
func TestListenerBindsLoopbackOnly(t *testing.T) {
	l, err := Start(0, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer l.Stop(context.Background())

	host, _, err := net.SplitHostPort(l.Addr())
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", l.Addr(), err)
	}
	if !net.ParseIP(host).IsLoopback() {
		t.Fatalf("listener bound to %q, want a loopback address", l.Addr())
	}
}

// TestListenerRejectsRequestWithoutToken guards against #43: an
// unauthenticated caller must not be able to get a payload captured (and,
// via the UI's "map to database" feature, inserted into a real database).
func TestListenerRejectsRequestWithoutToken(t *testing.T) {
	var mu sync.Mutex
	captured := false
	l, err := Start(0, func(r Request) {
		mu.Lock()
		captured = true
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer l.Stop(context.Background())

	resp, err := http.Post("http://"+l.Addr()+"/", "application/json", strings.NewReader(`{"evil":"payload"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	time.Sleep(50 * time.Millisecond) // give onRequest a chance to fire, if it wrongly would
	mu.Lock()
	defer mu.Unlock()
	if captured {
		t.Fatal("request without a valid token was still forwarded to onRequest")
	}
}

// TestListenerRejectsWrongToken guards against #43: a stale or guessed
// token must not be accepted.
func TestListenerRejectsWrongToken(t *testing.T) {
	l, err := Start(0, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer l.Stop(context.Background())

	req, err := http.NewRequest(http.MethodPost, "http://"+l.Addr()+"/", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(TokenHeader, "not-the-real-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
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
	resp, err := authedPost(l, "http://"+l.Addr()+"/", "text/plain", strings.NewReader(huge))
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
