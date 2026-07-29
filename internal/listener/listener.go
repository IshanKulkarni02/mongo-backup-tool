// Package listener runs a local HTTP server that logs every request it
// receives — for debugging device integrations that push data to a
// webhook (e.g. ZKTeco/eSSL biometric terminals speaking the ADMS push
// protocol): point the device at this listener instead of production, and
// watch the raw payloads arrive.
package listener

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// TokenHeader is the HTTP header a caller must send, set to the listener's
// Token(), for a request to be accepted. Point a device at this listener
// (see the package doc) by configuring it to send this header — devices
// that can't be configured with a custom header can't use this listener,
// which is the intended trade-off: without it, anyone who can reach the
// port could inject arbitrary "webhook" payloads (see Start's doc comment).
const TokenHeader = "X-Dbhelm-Webhook-Token"

// Request is one captured HTTP request.
type Request struct {
	ID        string              `json:"id"`
	Method    string              `json:"method"`
	Path      string              `json:"path"`
	Query     string              `json:"query"`
	Headers   map[string][]string `json:"headers"`
	Body      string              `json:"body"`
	Timestamp string              `json:"timestamp"`
}

// Listener is one running debug HTTP server.
type Listener struct {
	server *http.Server
	ln     net.Listener
	token  string
}

// bodyCap bounds how much of a request body is captured, so a device
// accidentally pushing a huge payload can't exhaust memory.
const bodyCap = 1 << 20 // 1 MiB

// Start binds a listener on 127.0.0.1:port — loopback only, so nothing off
// this machine can reach it regardless of LAN/firewall configuration — and
// begins forwarding every request that presents the listener's auth token
// (see TokenHeader) to onRequest (called synchronously per request, before
// a 200 OK is written back — keep it fast, or hand off to a goroutine
// internally). A request without a valid token gets 401 and is never
// forwarded to onRequest, so an unauthenticated caller can't get arbitrary
// data captured (and, via the UI's "map to database" insert feature, into
// a real database) even from another process on the same machine.
func Start(port int, onRequest func(Request)) (*Listener, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}

	token, err := newToken()
	if err != nil {
		ln.Close()
		return nil, fmt.Errorf("generating webhook auth token: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get(TokenHeader)), []byte(token)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("unauthorized: missing or incorrect " + TokenHeader + " header"))
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, bodyCap))
		headers := map[string][]string{}
		for k, v := range r.Header {
			headers[k] = v
		}
		if onRequest != nil {
			onRequest(Request{
				ID:        uuid.NewString(),
				Method:    r.Method,
				Path:      r.URL.Path,
				Query:     r.URL.RawQuery,
				Headers:   headers,
				Body:      string(body),
				Timestamp: time.Now().Format(time.RFC3339Nano),
			})
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	srv := &http.Server{Handler: mux}
	l := &Listener{server: srv, ln: ln, token: token}
	go srv.Serve(ln) //nolint:errcheck // Stop's Shutdown always returns http.ErrServerClosed here
	return l, nil
}

// newToken generates a random hex-encoded auth token for one listener
// instance (regenerated on every Start, so it can't be guessed or reused
// across runs).
func newToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Addr returns the listener's bound address (useful when Start was given
// port 0, for "pick any free port").
func (l *Listener) Addr() string {
	return l.ln.Addr().String()
}

// Token returns the auth token callers must send via TokenHeader.
func (l *Listener) Token() string {
	return l.token
}

// Stop gracefully shuts the server down.
func (l *Listener) Stop(ctx context.Context) error {
	err := l.server.Shutdown(ctx)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
