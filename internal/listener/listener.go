// Package listener runs a local HTTP server that logs every request it
// receives — for debugging device integrations that push data to a
// webhook (e.g. ZKTeco/eSSL biometric terminals speaking the ADMS push
// protocol): point the device at this listener instead of production, and
// watch the raw payloads arrive.
package listener

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
)

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
}

// bodyCap bounds how much of a request body is captured, so a device
// accidentally pushing a huge payload can't exhaust memory.
const bodyCap = 1 << 20 // 1 MiB

// Start binds a listener on port and begins forwarding every request it
// receives to onRequest (called synchronously per request, before a 200 OK
// is written back — keep it fast, or hand off to a goroutine internally).
func Start(port int, onRequest func(Request)) (*Listener, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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
	l := &Listener{server: srv, ln: ln}
	go srv.Serve(ln) //nolint:errcheck // Stop's Shutdown always returns http.ErrServerClosed here
	return l, nil
}

// Addr returns the listener's bound address (useful when Start was given
// port 0, for "pick any free port").
func (l *Listener) Addr() string {
	return l.ln.Addr().String()
}

// Stop gracefully shuts the server down.
func (l *Listener) Stop(ctx context.Context) error {
	err := l.server.Shutdown(ctx)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
