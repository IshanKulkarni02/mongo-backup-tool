// Package mcp exposes the DBHelm agent broker as a Model Context Protocol
// server over stdio, so MCP clients (Cursor, Codex, Claude Desktop, and
// others) get exactly what `dbhelm agent` gives Claude Code: read-only reads
// and change requests only DBHelm can approve. It adds no capability of its
// own; every tool is one broker agent-method.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/IshanKulkarni02/dbhelm/internal/broker"
)

// Caller is the broker as the server sees it.
type Caller interface {
	Call(ctx context.Context, method string, params any) (broker.Envelope, error)
}

var supportedVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// defaultWaitSeconds keeps a change request under the ~60s tool-call timeout
// most clients apply; a still-pending request is fetched with dbhelm_request.
const defaultWaitSeconds = 45

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Server serves one MCP session.
type Server struct {
	Caller  Caller
	Version string

	mu sync.Mutex // guards writes to out
}

// Serve reads newline-delimited JSON-RPC messages from in until it closes,
// writing responses to out. Requests are handled concurrently so a change
// waiting for approval never blocks a ping.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	var wg sync.WaitGroup
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		if len(line) == 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handle(ctx, line, out)
		}()
	}
	wg.Wait()
	return sc.Err()
}

func (s *Server) write(out io.Writer, r rpcResponse) {
	r.JSONRPC = "2.0"
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out.Write(append(b, '\n'))
}

func (s *Server) handle(ctx context.Context, line []byte, out io.Writer) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		s.write(out, rpcResponse{ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
		return
	}
	isNotification := len(req.ID) == 0
	reply := func(result any) {
		if !isNotification {
			s.write(out, rpcResponse{ID: req.ID, Result: result})
		}
	}
	fail := func(code int, msg string) {
		if !isNotification {
			s.write(out, rpcResponse{ID: req.ID, Error: &rpcError{code, msg}})
		}
	}

	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		version := supportedVersions[0]
		for _, v := range supportedVersions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		reply(map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "dbhelm", "version": s.Version},
			"instructions": "Use these tools instead of raw database clients or saved credentials. Reads are read-only and bounded. " +
				"dbhelm_write and dbhelm_snapshot_restore are requests: a person approves them in VS Code (or Autopilot does). " +
				"A PENDING_APPROVAL result means a person is deciding: poll with dbhelm_request, never resend. DENIED means stop and ask the user.",
		})
	case "notifications/initialized", "notifications/cancelled":
		// No response to notifications.
	case "ping":
		reply(map[string]any{})
	case "tools/list":
		reply(map[string]any{"tools": toolList()})
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			fail(-32602, "invalid params")
			return
		}
		t, ok := toolByName(p.Name)
		if !ok {
			fail(-32602, fmt.Sprintf("unknown tool %q", p.Name))
			return
		}
		reply(s.callTool(ctx, t, p.Arguments))
	default:
		fail(-32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

// callTool runs one tool and wraps the broker's envelope as MCP content. A
// failed envelope is a tool error (isError), not a protocol error, so the
// model sees the code and hint and can react.
func (s *Server) callTool(ctx context.Context, t tool, args map[string]any) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	if t.defaultWait {
		if _, ok := args["wait"]; !ok {
			args["wait"] = defaultWaitSeconds
		}
	}
	env, err := s.Caller.Call(ctx, t.method, args)
	if err != nil {
		env = broker.Envelope{Error: &broker.ErrorBody{Code: "BROKER_UNAVAILABLE", Message: err.Error()}}
	}
	text, _ := json.MarshalIndent(env, "", "  ")
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(text)}},
		"isError": !env.OK,
	}
}
