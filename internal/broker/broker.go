package broker

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/service"
)

// Options configures a Broker.
type Options struct {
	// RunDir is where broker.json is published. Required.
	RunDir string
	// LogPath is the ship's log file. Empty disables logging.
	LogPath string
	// Headless means no operator (the extension) will ever attach: reads
	// work, writes are refused with NO_OPERATOR, and there is no operator
	// token at all.
	Headless bool
	// SafetySnapshot takes a snapshot of the target database before each
	// approved agent write, so the change can be undone.
	SafetySnapshot bool
	// ApprovalTTL is how long a request waits for a decision before it
	// expires. Default 30 minutes.
	ApprovalTTL time.Duration
	// IdleTimeout, if set, closes a broker that has served no request for
	// that long (used for headless brokers autostarted by the CLI).
	IdleTimeout time.Duration
	Version     string
}

// Event is pushed to attached operators over /events.
type Event struct {
	Type string `json:"type"` // approval.pending | approval.updated | activity | autopilot
	Data any    `json:"data"`
}

// Broker is the local agent-access service. Construct with New, then Start.
type Broker struct {
	opts    Options
	engines *engine.Manager
	queue   *Queue
	log     *Logbook

	agentToken    string
	operatorToken string

	autoMu sync.Mutex
	auto   Autopilot

	subMu sync.Mutex
	subs  map[chan Event]struct{}

	agentMethods    map[string]handler
	operatorMethods map[string]handler

	ln       net.Listener
	srv      *http.Server
	info     Info
	last     atomic.Int64
	done     chan struct{}
	closeOne sync.Once
}

type handler func(ctx context.Context, role Role, params json.RawMessage) (any, Meta, *ErrorBody)

// New builds a broker. It opens nothing until Start.
func New(opts Options) (*Broker, error) {
	if opts.RunDir == "" {
		return nil, fmt.Errorf("broker: RunDir is required")
	}
	if opts.ApprovalTTL <= 0 {
		opts.ApprovalTTL = 30 * time.Minute
	}
	b := &Broker{
		opts:       opts,
		engines:    engine.NewManager(service.ResolveEngineConn),
		queue:      NewQueue(),
		log:        NewLogbook(opts.LogPath),
		agentToken: randToken(),
		subs:       map[chan Event]struct{}{},
		done:       make(chan struct{}),
	}
	if !opts.Headless {
		b.operatorToken = randToken()
	}
	b.registerMethods()
	b.touch()
	return b, nil
}

func randToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic("broker: no randomness: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

func (b *Broker) touch() { b.last.Store(time.Now().UnixNano()) }

// Handler exposes the HTTP surface (for Start and for tests).
func (b *Broker) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc", b.serveRPC)
	mux.HandleFunc("/events", b.serveEvents)
	return mux
}

// Start listens on a random loopback port and publishes broker.json (0600,
// agent token only). The returned Info additionally carries the operator
// token, which the caller must hand only to the operator and never persist.
func (b *Broker) Start() (Info, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Info{}, err
	}
	b.ln = ln
	port := ln.Addr().(*net.TCPAddr).Port
	b.info = Info{
		Port: port, PID: os.Getpid(), AgentToken: b.agentToken, Headless: b.opts.Headless,
		StartedAt: now().Format(time.RFC3339), Version: b.opts.Version,
	}
	if err := b.writeInfoFile(); err != nil {
		ln.Close()
		return Info{}, err
	}
	b.srv = &http.Server{Handler: b.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go b.srv.Serve(ln)
	go b.housekeeping()
	full := b.info
	full.OperatorToken = b.operatorToken
	return full, nil
}

// InfoPath is where broker.json lives for a given run directory.
func InfoPath(runDir string) string { return filepath.Join(runDir, "broker.json") }

func (b *Broker) writeInfoFile() error {
	if err := os.MkdirAll(b.opts.RunDir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b.info, "", "  ")
	if err != nil {
		return err
	}
	tmp := InfoPath(b.opts.RunDir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, InfoPath(b.opts.RunDir))
}

// Done is closed when the broker has shut down (Close, idle timeout, or an
// authorized shutdown request).
func (b *Broker) Done() <-chan struct{} { return b.done }

// Close stops the server, cancels unanswered requests and removes
// broker.json.
func (b *Broker) Close() {
	b.closeOne.Do(func() {
		b.queue.cancelAll()
		if b.srv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			b.srv.Shutdown(ctx)
			cancel()
		}
		if b.info.Port != 0 {
			if cur, err := os.ReadFile(InfoPath(b.opts.RunDir)); err == nil && strings.Contains(string(cur), b.agentToken) {
				os.Remove(InfoPath(b.opts.RunDir))
			}
		}
		b.engines.Close()
		close(b.done)
	})
}

func (b *Broker) housekeeping() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-b.done:
			return
		case <-t.C:
			for _, id := range b.queue.expireStale(b.opts.ApprovalTTL) {
				if a, ok := b.queue.Get(id); ok {
					b.publish(Event{Type: "approval.updated", Data: a})
				}
			}
			if b.opts.IdleTimeout > 0 && time.Since(time.Unix(0, b.last.Load())) > b.opts.IdleTimeout {
				b.Close()
				return
			}
		}
	}
}

// --- auth + dispatch ---

func (b *Broker) roleFor(r *http.Request) (Role, bool) {
	h := r.Header.Get("Authorization")
	tok, ok := strings.CutPrefix(h, "Bearer ")
	if !ok || tok == "" {
		return "", false
	}
	if b.operatorToken != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(b.operatorToken)) == 1 {
		return RoleOperator, true
	}
	if subtle.ConstantTimeCompare([]byte(tok), []byte(b.agentToken)) == 1 {
		return RoleAgent, true
	}
	return "", false
}

// loopbackOnly rejects anything that isn't a direct local client: a browser
// page (Origin header) or a DNS-rebinding request (foreign Host header).
func (b *Broker) loopbackOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") != "" {
		writeJSON(w, http.StatusForbidden, Envelope{Error: errBody(CodeUnauthorized, "", "browser requests are not accepted")})
		return false
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	if host != "127.0.0.1" && host != "localhost" && host != "[::1]" && host != "::1" {
		writeJSON(w, http.StatusForbidden, Envelope{Error: errBody(CodeUnauthorized, "", "unexpected host")})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (b *Broker) serveRPC(w http.ResponseWriter, r *http.Request) {
	if !b.loopbackOnly(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, Envelope{Error: errBody(CodeBadRequest, "", "POST only")})
		return
	}
	role, ok := b.roleFor(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, Envelope{Error: errBody(CodeUnauthorized, "read broker.json for the agent token", "missing or invalid token")})
		return
	}
	b.touch()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	var req Request
	if err != nil || json.Unmarshal(body, &req) != nil || req.Method == "" {
		writeJSON(w, http.StatusBadRequest, Envelope{Error: errBody(CodeBadRequest, `send {"method": "...", "params": {...}}`, "invalid request body")})
		return
	}
	h := b.agentMethods[req.Method]
	if role == RoleOperator {
		if oh, ok := b.operatorMethods[req.Method]; ok {
			h = oh
		}
	} else if _, isOp := b.operatorMethods[req.Method]; isOp && h == nil {
		writeJSON(w, http.StatusForbidden, Envelope{Error: errBody(CodeUnauthorized, "only the VS Code operator can do this", "method %q is not available to agents", req.Method)})
		return
	}
	if h == nil {
		writeJSON(w, http.StatusNotFound, Envelope{Error: errBody(CodeUnknownMethod, "", "unknown method %q", req.Method)})
		return
	}
	start := time.Now()
	data, meta, eb := h(r.Context(), role, req.Params)
	meta.MS = time.Since(start).Milliseconds()
	env := Envelope{OK: eb == nil, Data: data, Error: eb, Meta: &meta}
	writeJSON(w, http.StatusOK, env)
}

// --- operator presence + events ---

// Subscribe registers an operator event stream. The broker treats "has at
// least one subscriber" as "an operator is attached"; when the last one
// leaves, Autopilot is switched off.
func (b *Broker) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	b.subMu.Lock()
	b.subs[ch] = struct{}{}
	b.subMu.Unlock()
	b.log.Add(LogEntry{Actor: "broker", Event: "operator.attached"})
	return ch, func() {
		b.subMu.Lock()
		delete(b.subs, ch)
		remaining := len(b.subs)
		b.subMu.Unlock()
		if remaining == 0 {
			b.setAutopilot(Autopilot{}, "operator detached")
			b.log.Add(LogEntry{Actor: "broker", Event: "operator.detached"})
		}
	}
}

func (b *Broker) operatorAttached() bool {
	b.subMu.Lock()
	defer b.subMu.Unlock()
	return len(b.subs) > 0
}

func (b *Broker) publish(e Event) {
	b.subMu.Lock()
	defer b.subMu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default: // a stalled operator must never block the broker
		}
	}
}

func (b *Broker) serveEvents(w http.ResponseWriter, r *http.Request) {
	if !b.loopbackOnly(w, r) {
		return
	}
	role, ok := b.roleFor(r)
	if !ok || role != RoleOperator {
		writeJSON(w, http.StatusUnauthorized, Envelope{Error: errBody(CodeUnauthorized, "", "operator token required")})
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch, cancel := b.Subscribe()
	defer cancel()
	fmt.Fprint(w, ": attached\n\n")
	fl.Flush()
	hb := time.NewTicker(15 * time.Second)
	defer hb.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-b.done:
			return
		case e := <-ch:
			data, _ := json.Marshal(e.Data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data)
			fl.Flush()
		case <-hb.C:
			fmt.Fprint(w, ": hb\n\n")
			fl.Flush()
		}
	}
}

// --- autopilot ---

// Autopilot is the "DBHelm approves on the user's behalf" mode.
type Autopilot struct {
	On bool `json:"on"`
	// AllowDangerous lets Autopilot also approve DROP/TRUNCATE/ALTER and
	// unqualified DELETE/UPDATE. Off by default: those still wait for the user.
	AllowDangerous bool   `json:"allowDangerous"`
	Until          string `json:"until,omitempty"` // RFC3339; empty = until the operator detaches
}

func (b *Broker) setAutopilot(a Autopilot, why string) {
	b.autoMu.Lock()
	changed := b.auto != a
	b.auto = a
	b.autoMu.Unlock()
	if changed {
		b.publish(Event{Type: "autopilot", Data: a})
		b.log.Add(LogEntry{Actor: "operator", Event: "autopilot", Outcome: strconv.FormatBool(a.On), Detail: why})
	}
}

// autopilotState returns the current mode, treating an expired one as off.
func (b *Broker) autopilotState() Autopilot {
	b.autoMu.Lock()
	a := b.auto
	b.autoMu.Unlock()
	if a.On && a.Until != "" {
		if t, err := time.Parse(time.RFC3339, a.Until); err == nil && time.Now().After(t) {
			b.setAutopilot(Autopilot{}, "expired")
			return Autopilot{}
		}
	}
	return a
}
