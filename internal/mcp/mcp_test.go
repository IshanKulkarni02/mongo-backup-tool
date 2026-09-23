package mcp

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IshanKulkarni02/dbhelm/internal/broker"
	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

type fakeCaller struct {
	mu    sync.Mutex
	calls []struct {
		method string
		params map[string]any
	}
	env broker.Envelope
}

func (f *fakeCaller) Call(ctx context.Context, method string, params any) (broker.Envelope, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		method string
		params map[string]any
	}{method, params.(map[string]any)})
	return f.env, nil
}

// session runs the server over pipes and returns a send/recv pair.
func session(t *testing.T, c Caller) (send func(string), recv func() map[string]any) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	srv := &Server{Caller: c, Version: "test"}
	go func() { srv.Serve(context.Background(), inR, outW); outW.Close() }()
	t.Cleanup(func() { inW.Close() })
	dec := json.NewDecoder(outR)
	return func(s string) { inW.Write([]byte(s + "\n")) }, func() map[string]any {
		var m map[string]any
		done := make(chan error, 1)
		go func() { done <- dec.Decode(&m) }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("recv: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for a response")
		}
		return m
	}
}

func TestHandshakeAndToolList(t *testing.T) {
	send, recv := session(t, &fakeCaller{})
	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	r := recv()
	res := r["result"].(map[string]any)
	if res["protocolVersion"] != "2024-11-05" || res["serverInfo"].(map[string]any)["name"] != "dbhelm" {
		t.Fatalf("initialize: %+v", r)
	}
	send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`) // no response expected
	send(`{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	if r := recv(); r["id"].(float64) != 2 {
		t.Fatalf("notification produced a response or ping failed: %+v", r)
	}
	send(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	list := recv()["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, x := range list {
		tl := x.(map[string]any)
		names[tl["name"].(string)] = true
		if tl["inputSchema"].(map[string]any)["type"] != "object" {
			t.Errorf("%v has no object schema", tl["name"])
		}
	}
	for _, want := range []string{"dbhelm_query", "dbhelm_write", "dbhelm_snapshot_restore", "dbhelm_request", "dbhelm_connections"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
	// Nothing in the tool list can carry a credential or connection string.
	raw, _ := json.Marshal(list)
	for _, bad := range []string{"password", "uri", "token"} {
		if strings.Contains(strings.ToLower(string(raw)), `"`+bad+`"`) {
			t.Errorf("tool schema exposes %q", bad)
		}
	}
	send(`{"jsonrpc":"2.0","id":4,"method":"resources/list"}`)
	if e := recv()["error"].(map[string]any); e["code"].(float64) != -32601 {
		t.Fatalf("unknown method: %+v", e)
	}
	send(`not json`)
	if e := recv()["error"].(map[string]any); e["code"].(float64) != -32700 {
		t.Fatalf("parse error: %+v", e)
	}
}

func TestToolCallMapsToBrokerAndReportsErrors(t *testing.T) {
	fc := &fakeCaller{env: broker.Envelope{OK: true, Data: map[string]any{"x": 1}}}
	send, recv := session(t, fc)
	send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"dbhelm_write","arguments":{"connection":"c","database":"d","statement":"DELETE FROM t WHERE id=1"}}}`)
	res := recv()["result"].(map[string]any)
	if res["isError"] != false {
		t.Fatalf("result: %+v", res)
	}
	if fc.calls[0].method != "write" || fc.calls[0].params["wait"].(int) != defaultWaitSeconds {
		t.Fatalf("call: %+v", fc.calls[0])
	}
	fc.env = broker.Envelope{Error: &broker.ErrorBody{Code: "DENIED", Message: "refused", Hint: "ask the user"}}
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"dbhelm_query","arguments":{"connection":"c","database":"d","sql":"select 1"}}}`)
	res = recv()["result"].(map[string]any)
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if res["isError"] != true || !strings.Contains(text, "DENIED") || !strings.Contains(text, "ask the user") {
		t.Fatalf("error result: %+v", res)
	}
	send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"drop_everything","arguments":{}}}`)
	if e := recv()["error"].(map[string]any); e["code"].(float64) != -32602 {
		t.Fatalf("unknown tool: %+v", e)
	}
}

// Against a real broker: reads work, and a change with no operator is refused.
func TestAgainstRealBroker(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DBHELM_CONFIG_DIR", filepath.Join(dir, "cfg"))
	db := filepath.Join(dir, "app.db")
	config.Update(func(c *config.Config) error {
		c.Connections = []config.Connection{{Name: "lite", Engine: "sqlite", URI: db, AgentAccess: "write"}}
		return nil
	})
	eng, _ := engine.Lookup("sqlite")
	sess, _ := eng.Open(context.Background(), engine.ConnConfig{URI: db})
	sess.(engine.SQLSession).Execute(context.Background(), "main", "CREATE TABLE t(id INTEGER PRIMARY KEY, v TEXT)")
	sess.(engine.SQLSession).Execute(context.Background(), "main", "INSERT INTO t(v) VALUES ('a')")
	sess.Close(context.Background())

	b, err := broker.New(broker.Options{RunDir: filepath.Join(dir, "run"), Headless: true, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := b.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	send, recv := session(t, broker.NewAgentClient(info))
	send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"dbhelm_query","arguments":{"connection":"lite","database":"main","sql":"select v from t"}}}`)
	res := recv()["result"].(map[string]any)
	if res["isError"] != false || !strings.Contains(res["content"].([]any)[0].(map[string]any)["text"].(string), `"display": "a"`) {
		t.Fatalf("query: %+v", res)
	}
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"dbhelm_write","arguments":{"connection":"lite","database":"main","statement":"DELETE FROM t WHERE id=1","wait":1}}}`)
	res = recv()["result"].(map[string]any)
	if res["isError"] != true || !strings.Contains(res["content"].([]any)[0].(map[string]any)["text"].(string), "NO_OPERATOR") {
		t.Fatalf("write without operator: %+v", res)
	}
}
