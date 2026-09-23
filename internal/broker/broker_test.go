package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/snapshot"
)

type harness struct {
	t    *testing.T
	b    *Broker
	info Info
	db   string
	dir  string
}

// newHarness starts a real broker on a loopback port with a temp config dir
// holding three SQLite connections: lite (agent write), ro (agent read),
// hidden (agent off). Data lives in a real SQLite file.
func newHarness(t *testing.T, headless bool) *harness {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DBHELM_CONFIG_DIR", filepath.Join(dir, "cfg"))
	db := filepath.Join(dir, "app.db")
	if err := config.Update(func(c *config.Config) error {
		c.Connections = []config.Connection{
			{Name: "lite", Engine: "sqlite", URI: db, AgentAccess: "write"},
			{Name: "ro", Engine: "sqlite", URI: db, AgentAccess: "read"},
			{Name: "hidden", Engine: "sqlite", URI: db},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	eng, _ := engine.Lookup("sqlite")
	sess, err := eng.Open(context.Background(), engine.ConnConfig{URI: db})
	if err != nil {
		t.Fatal(err)
	}
	ss := sess.(engine.SQLSession)
	for _, q := range []string{
		"CREATE TABLE users(id INTEGER PRIMARY KEY, name TEXT)",
		"INSERT INTO users(name) VALUES ('ann'),('bob')",
	} {
		if _, err := ss.Execute(context.Background(), "main", q); err != nil {
			t.Fatal(err)
		}
	}
	sess.Close(context.Background())

	b, err := New(Options{
		RunDir: filepath.Join(dir, "run"), LogPath: filepath.Join(dir, "logbook.jsonl"),
		Headless: headless, SafetySnapshot: true, Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := b.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return &harness{t: t, b: b, info: info, db: db, dir: dir}
}

func (h *harness) call(token, method string, params any) (Envelope, int) {
	h.t.Helper()
	body, _ := json.Marshal(map[string]any{"method": method, "params": params})
	req, _ := http.NewRequest("POST", "http://127.0.0.1:"+itoa(h.info.Port)+"/rpc", bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var env Envelope
	json.NewDecoder(resp.Body).Decode(&env)
	return env, resp.StatusCode
}

func (h *harness) agent(method string, params any) Envelope {
	h.t.Helper()
	env, _ := h.call(h.info.AgentToken, method, params)
	return env
}

func (h *harness) operator(method string, params any) Envelope {
	h.t.Helper()
	env, _ := h.call(h.info.OperatorToken, method, params)
	return env
}

func itoa(n int) string { return strconv.Itoa(n) }

func (h *harness) userCount() int {
	h.t.Helper()
	eng, _ := engine.Lookup("sqlite")
	sess, err := eng.Open(context.Background(), engine.ConnConfig{URI: h.db})
	if err != nil {
		h.t.Fatal(err)
	}
	defer sess.Close(context.Background())
	res, err := sess.(engine.SQLSession).Query(context.Background(), "main", "SELECT COUNT(*) AS n FROM users")
	if err != nil {
		h.t.Fatal(err)
	}
	var n int
	json.Unmarshal([]byte(res.Rows[0]["n"].Display), &n)
	return n
}

func code(env Envelope) string {
	if env.Error == nil {
		return ""
	}
	return env.Error.Code
}

func TestAuthAndRoles(t *testing.T) {
	h := newHarness(t, false)
	if env, status := h.call("", "ping", nil); status != 401 || code(env) != CodeUnauthorized {
		t.Fatalf("no token: %d %s", status, code(env))
	}
	if _, status := h.call("nope", "ping", nil); status != 401 {
		t.Fatalf("bad token: %d", status)
	}
	if env := h.agent("ping", nil); !env.OK {
		t.Fatalf("agent ping: %+v", env.Error)
	}
	// An agent cannot use operator-only methods.
	for _, m := range []string{"approvals.list", "approvals.decide", "autopilot.set", "connections.setAccess", "logbook.tail",
		"connection.add", "connection.remove", "connection.test", "snapshots.restore", "backups.list", "backups.create", "backups.restore", "backups.delete"} {
		if env, status := h.call(h.info.AgentToken, m, nil); status != 403 || env.OK {
			t.Errorf("agent reached operator method %s (status %d)", m, status)
		}
	}
	if env := h.operator("approvals.list", nil); !env.OK {
		t.Fatalf("operator approvals.list: %+v", env.Error)
	}
}

func TestBrowserAndForeignHostRejected(t *testing.T) {
	h := newHarness(t, false)
	req, _ := http.NewRequest("POST", "http://127.0.0.1:"+itoa(h.info.Port)+"/rpc", strings.NewReader(`{"method":"ping"}`))
	req.Header.Set("Authorization", "Bearer "+h.info.AgentToken)
	req.Header.Set("Origin", "https://evil.example")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 403 {
		t.Errorf("Origin header accepted: %d", resp.StatusCode)
	}
	req2, _ := http.NewRequest("POST", "http://127.0.0.1:"+itoa(h.info.Port)+"/rpc", strings.NewReader(`{"method":"ping"}`))
	req2.Header.Set("Authorization", "Bearer "+h.info.AgentToken)
	req2.Host = "evil.example"
	resp2, _ := http.DefaultClient.Do(req2)
	if resp2.StatusCode != 403 {
		t.Errorf("foreign Host accepted: %d", resp2.StatusCode)
	}
}

func TestBrokerFileHoldsNoOperatorToken(t *testing.T) {
	h := newHarness(t, false)
	raw, err := os.ReadFile(InfoPath(filepath.Join(h.dir, "run")))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), h.info.OperatorToken) || h.info.OperatorToken == "" {
		t.Fatal("operator token must exist in memory only")
	}
	if !strings.Contains(string(raw), h.info.AgentToken) {
		t.Fatal("agent token missing from broker.json")
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(InfoPath(filepath.Join(h.dir, "run"))); st.Mode().Perm() != 0o600 {
			t.Fatalf("broker.json mode = %v, want 0600", st.Mode().Perm())
		}
	}
	hl := newHarness(t, true)
	if hl.info.OperatorToken != "" {
		t.Fatal("headless broker must have no operator token")
	}
	if env, status := hl.call("anything", "approvals.list", nil); status != 401 || env.OK {
		t.Fatal("headless broker accepted an operator call")
	}
}

func TestAgentSeesOnlyEnabledConnections(t *testing.T) {
	h := newHarness(t, false)
	env := h.agent("connections", nil)
	raw, _ := json.Marshal(env.Data)
	if strings.Contains(string(raw), "hidden") || !strings.Contains(string(raw), "lite") || !strings.Contains(string(raw), "ro") {
		t.Fatalf("connections = %s", raw)
	}
	if strings.Contains(string(raw), h.db) || strings.Contains(string(raw), "uri") {
		t.Fatalf("connection list leaks the URI: %s", raw)
	}
	// Off and nonexistent are indistinguishable.
	a := h.agent("schema", map[string]any{"connection": "hidden", "database": "main"})
	b := h.agent("schema", map[string]any{"connection": "does-not-exist", "database": "main"})
	if code(a) != CodeAccessOff || code(b) != CodeAccessOff {
		t.Fatalf("hidden=%s missing=%s", code(a), code(b))
	}
}

func TestReadsWorkAndWritesViaQueryAreRefused(t *testing.T) {
	h := newHarness(t, false)
	env := h.agent("query", map[string]any{"connection": "ro", "database": "main", "sql": "SELECT name FROM users ORDER BY id"})
	if !env.OK || env.Meta.Rows != 2 {
		t.Fatalf("read failed: %+v %+v", env.Error, env.Meta)
	}
	for _, q := range []string{"DELETE FROM users", "DROP TABLE users", "INSERT INTO users(name) VALUES ('x')"} {
		env := h.agent("query", map[string]any{"connection": "lite", "database": "main", "sql": q})
		if env.OK || code(env) != CodeReadOnly {
			t.Errorf("query(%q): ok=%v code=%s", q, env.OK, code(env))
		}
	}
	if n := h.userCount(); n != 2 {
		t.Fatalf("users changed to %d", n)
	}
}

func TestWriteWithoutOperatorFailsClosed(t *testing.T) {
	h := newHarness(t, false) // operator token exists but nobody is subscribed
	env := h.agent("write", map[string]any{"connection": "lite", "database": "main", "statement": "INSERT INTO users(name) VALUES ('eve')", "wait": 1})
	if code(env) != CodeNoOperator {
		t.Fatalf("code = %s", code(env))
	}
	if n := h.userCount(); n != 2 {
		t.Fatalf("write ran without an operator: %d users", n)
	}
	hl := newHarness(t, true)
	env = hl.agent("write", map[string]any{"connection": "lite", "database": "main", "statement": "INSERT INTO users(name) VALUES ('eve')"})
	if code(env) != CodeNoOperator {
		t.Fatalf("headless code = %s", code(env))
	}
}

func TestReadOnlyAgentAccessCannotWrite(t *testing.T) {
	h := newHarness(t, false)
	_, cancel := h.b.Subscribe()
	defer cancel()
	env := h.agent("write", map[string]any{"connection": "ro", "database": "main", "statement": "DELETE FROM users WHERE id = 1"})
	if code(env) != CodeAccessOff || h.userCount() != 2 {
		t.Fatalf("code=%s users=%d", code(env), h.userCount())
	}
}

// pendingID waits until the operator sees a pending request and returns it.
func (h *harness) pendingID() string {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		env := h.operator("approvals.list", nil)
		var list []Approval
		raw, _ := json.Marshal(env.Data)
		json.Unmarshal(raw, &list)
		for _, a := range list {
			if a.Status == StatusPending {
				return a.ID
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatal("no pending request appeared")
	return ""
}

func (h *harness) asyncWrite(params map[string]any) <-chan Envelope {
	ch := make(chan Envelope, 1)
	go func() { ch <- h.agent("write", params) }()
	return ch
}

func TestManualApprovalRunsTheChangeAfterASafetySnapshot(t *testing.T) {
	h := newHarness(t, false)
	_, cancel := h.b.Subscribe()
	defer cancel()

	res := h.asyncWrite(map[string]any{"connection": "lite", "database": "main", "statement": "INSERT INTO users(name) VALUES ('cy')", "wait": 20})
	id := h.pendingID()
	if n := h.userCount(); n != 2 {
		t.Fatalf("change ran before approval: %d users", n)
	}
	if env := h.operator("approvals.decide", map[string]any{"id": id, "approve": true}); !env.OK {
		t.Fatalf("decide: %+v", env.Error)
	}
	select {
	case env := <-res:
		if !env.OK {
			t.Fatalf("agent result: %+v", env.Error)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("agent never got a result")
	}
	if n := h.userCount(); n != 3 {
		t.Fatalf("users = %d, want 3", n)
	}
	snaps, _ := snapshot.Log("lite", "main")
	if len(snaps) == 0 || !strings.Contains(snaps[len(snaps)-1].Message, "safety snapshot") {
		t.Fatalf("no safety snapshot taken: %+v", snaps)
	}
	// Deciding twice is refused.
	if env := h.operator("approvals.decide", map[string]any{"id": id, "approve": false}); env.OK {
		t.Fatal("a finished request was decided again")
	}
}

func TestDenialLeavesDataAlone(t *testing.T) {
	h := newHarness(t, false)
	_, cancel := h.b.Subscribe()
	defer cancel()
	res := h.asyncWrite(map[string]any{"connection": "lite", "database": "main", "statement": "DELETE FROM users WHERE id = 1", "wait": 20})
	id := h.pendingID()
	h.operator("approvals.decide", map[string]any{"id": id, "approve": false})
	env := <-res
	if code(env) != CodeDenied || h.userCount() != 2 {
		t.Fatalf("code=%s users=%d", code(env), h.userCount())
	}
}

func TestPendingRequestOutlivesTheAgentsWait(t *testing.T) {
	h := newHarness(t, false)
	_, cancel := h.b.Subscribe()
	defer cancel()
	env := h.agent("write", map[string]any{"connection": "lite", "database": "main", "statement": "INSERT INTO users(name) VALUES ('di')", "wait": 1})
	if code(env) != CodePendingApproval || env.Meta.RequestID == "" {
		t.Fatalf("code=%s meta=%+v", code(env), env.Meta)
	}
	id := env.Meta.RequestID
	h.operator("approvals.decide", map[string]any{"id": id, "approve": true})
	got := h.agent("request.get", map[string]any{"id": id, "wait": 20})
	if !got.OK || h.userCount() != 3 {
		t.Fatalf("poll: %+v users=%d", got.Error, h.userCount())
	}
}

func TestAutopilot(t *testing.T) {
	h := newHarness(t, false)
	if env, status := h.call(h.info.AgentToken, "autopilot.set", map[string]any{"on": true}); status != 403 || env.OK {
		t.Fatal("agent switched Autopilot on")
	}
	// Autopilot cannot be enabled with nobody attached.
	if env := h.operator("autopilot.set", map[string]any{"on": true}); env.OK {
		t.Fatal("Autopilot enabled with no operator attached")
	}
	_, cancel := h.b.Subscribe()
	if env := h.operator("autopilot.set", map[string]any{"on": true}); !env.OK {
		t.Fatalf("autopilot.set: %+v", env.Error)
	}

	// Ordinary change: runs with no approval.
	env := h.agent("write", map[string]any{"connection": "lite", "database": "main", "statement": "INSERT INTO users(name) VALUES ('ed')", "wait": 20})
	if !env.OK || h.userCount() != 3 {
		t.Fatalf("autopilot write: %+v users=%d", env.Error, h.userCount())
	}
	if raw, _ := json.Marshal(env.Data); !strings.Contains(string(raw), `"decidedBy":"autopilot"`) {
		t.Fatalf("not marked as autopilot: %s", raw)
	}

	// Dangerous change still waits for a person.
	env = h.agent("write", map[string]any{"connection": "lite", "database": "main", "statement": "DELETE FROM users", "wait": 1})
	if code(env) != CodePendingApproval || h.userCount() != 3 {
		t.Fatalf("dangerous under autopilot: code=%s users=%d", code(env), h.userCount())
	}
	h.operator("approvals.decide", map[string]any{"id": env.Meta.RequestID, "approve": false})

	// Opting in lets Autopilot approve it.
	h.operator("autopilot.set", map[string]any{"on": true, "allowDangerous": true})
	env = h.agent("write", map[string]any{"connection": "lite", "database": "main", "statement": "DELETE FROM users WHERE id = 1", "wait": 20})
	if !env.OK || h.userCount() != 2 {
		t.Fatalf("allowDangerous write: %+v users=%d", env.Error, h.userCount())
	}

	// Detaching the operator switches Autopilot off and writes fail closed.
	cancel()
	if h.b.autopilotState().On {
		t.Fatal("Autopilot survived the operator detaching")
	}
	env = h.agent("write", map[string]any{"connection": "lite", "database": "main", "statement": "INSERT INTO users(name) VALUES ('fay')", "wait": 1})
	if code(env) != CodeNoOperator {
		t.Fatalf("after detach: %s", code(env))
	}
}

func TestSetAccessChangesWhatAgentsSee(t *testing.T) {
	h := newHarness(t, false)
	if env := h.operator("connections.setAccess", map[string]any{"name": "hidden", "access": "read"}); !env.OK {
		t.Fatalf("%+v", env.Error)
	}
	if env := h.agent("schema", map[string]any{"connection": "hidden", "database": "main"}); !env.OK {
		t.Fatalf("hidden still unavailable: %+v", env.Error)
	}
	h.operator("connections.setAccess", map[string]any{"name": "hidden", "access": "off"})
	if env := h.agent("schema", map[string]any{"connection": "hidden", "database": "main"}); env.OK {
		t.Fatal("access off did not take effect")
	}
	if env := h.operator("connections.setAccess", map[string]any{"name": "hidden", "access": "root"}); env.OK {
		t.Fatal("invalid access accepted")
	}
}

func TestLogbookRecordsActivityWithoutSecrets(t *testing.T) {
	h := newHarness(t, false)
	h.agent("query", map[string]any{"connection": "lite", "database": "main", "sql": "SELECT 1"})
	h.agent("query", map[string]any{"connection": "lite", "database": "main", "sql": "DELETE FROM users"})
	raw, err := os.ReadFile(filepath.Join(h.dir, "logbook.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"event":"query"`) || !strings.Contains(s, `"outcome":"refused"`) {
		t.Fatalf("logbook missing entries:\n%s", s)
	}
	if strings.Contains(s, h.info.AgentToken) || strings.Contains(s, h.info.OperatorToken) || strings.Contains(s, h.db) {
		t.Fatal("logbook leaked a token or connection URI")
	}
}

func TestHeadlessShutdownByAgentOnly(t *testing.T) {
	h := newHarness(t, false)
	if env := h.agent("shutdown", nil); env.OK {
		t.Fatal("agent stopped a broker that has an operator")
	}
	hl := newHarness(t, true)
	if env := hl.agent("shutdown", nil); !env.OK {
		t.Fatalf("headless shutdown refused: %+v", env.Error)
	}
	select {
	case <-hl.b.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("headless broker did not stop")
	}
}

func TestConcurrentDecisionsRunOnce(t *testing.T) {
	h := newHarness(t, false)
	_, cancel := h.b.Subscribe()
	defer cancel()
	res := h.asyncWrite(map[string]any{"connection": "lite", "database": "main", "statement": "INSERT INTO users(name) VALUES ('once')", "wait": 20})
	id := h.pendingID()
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.operator("approvals.decide", map[string]any{"id": id, "approve": true})
		}()
	}
	wg.Wait()
	<-res
	if n := h.userCount(); n != 3 {
		t.Fatalf("change ran %d times", n-2)
	}
}

func TestOperatorConnectionManagement(t *testing.T) {
	h := newHarness(t, false)
	secretURI := "postgres://bob:topsecretpw@db.internal:5432/app"
	if env := h.operator("connection.add", map[string]any{"name": "pg", "uri": secretURI, "engine": "postgres", "agentAccess": "read"}); !env.OK {
		t.Fatalf("add: %+v", env.Error)
	}
	if env := h.operator("connection.add", map[string]any{"name": "bad", "uri": "x", "engine": "oracle"}); env.OK {
		t.Fatal("unknown engine accepted")
	}
	// The agent sees the connection by name and never its URI.
	raw, _ := json.Marshal(h.agent("connections", nil))
	if !strings.Contains(string(raw), `"pg"`) || strings.Contains(string(raw), "topsecretpw") || strings.Contains(string(raw), "db.internal") {
		t.Fatalf("agent view: %s", raw)
	}
	// Re-saving without agentAccess keeps what the user granted.
	h.operator("connection.add", map[string]any{"name": "pg", "uri": secretURI, "engine": "postgres"})
	raw, _ = json.Marshal(h.agent("connections", nil))
	if !strings.Contains(string(raw), `"pg"`) {
		t.Fatal("re-saving a connection revoked agent access")
	}
	// Failed connection tests do not echo the URI.
	env := h.operator("connection.test", map[string]any{"name": "pg"})
	if env.OK || strings.Contains(env.Error.Message, "topsecretpw") {
		t.Fatalf("test result: %+v", env)
	}
	if log, _ := os.ReadFile(filepath.Join(h.dir, "logbook.jsonl")); strings.Contains(string(log), "topsecretpw") {
		t.Fatal("logbook contains the password")
	}
	if env := h.operator("connection.remove", map[string]any{"name": "pg"}); !env.OK {
		t.Fatalf("remove: %+v", env.Error)
	}
}

func TestConnectionUpdateKeepsCredentialsAndChangesSettings(t *testing.T) {
	h := newHarness(t, false)
	if env := h.operator("connection.update", map[string]any{"name": "hidden", "agentAccess": "read", "environment": "prod"}); !env.OK {
		t.Fatalf("update: %+v", env.Error)
	}
	cfg, _ := config.Load()
	c, _ := cfg.Find("hidden")
	if c.AgentAccess != "read" || c.Environment != "prod" || c.URI != h.db {
		t.Fatalf("after update: %+v", c)
	}
	// Omitted fields are left alone.
	h.operator("connection.update", map[string]any{"name": "hidden", "readOnly": true})
	cfg, _ = config.Load()
	c, _ = cfg.Find("hidden")
	if c.AgentAccess != "read" || !c.ReadOnly {
		t.Fatalf("partial update clobbered fields: %+v", c)
	}
	if env := h.operator("connection.update", map[string]any{"name": "hidden", "environment": "moon"}); env.OK {
		t.Fatal("invalid environment accepted")
	}
	if env, status := h.call(h.info.AgentToken, "connection.update", map[string]any{"name": "hidden", "agentAccess": "write"}); status != 403 || env.OK {
		t.Fatal("agent reached connection.update")
	}
}

func TestSnapshotDiffForAgents(t *testing.T) {
	h := newHarness(t, false)
	snap := func(msg string) string {
		env := h.agent("snapshot.create", map[string]any{"connection": "ro", "database": "main", "message": msg})
		if !env.OK {
			t.Fatalf("snapshot.create: %+v", env.Error)
		}
		raw, _ := json.Marshal(env.Data)
		var s struct{ ID string }
		json.Unmarshal(raw, &s)
		return s.ID
	}
	first := snap("one")
	eng, _ := engine.Lookup("sqlite")
	sess, _ := eng.Open(context.Background(), engine.ConnConfig{URI: h.db})
	sess.(engine.SQLSession).Execute(context.Background(), "main", "UPDATE users SET name='ANN' WHERE id=1")
	sess.(engine.SQLSession).Execute(context.Background(), "main", "INSERT INTO users(name) VALUES ('cy')")
	sess.Close(context.Background())
	second := snap("two")

	env := h.agent("snapshot.diff", map[string]any{"connection": "ro", "database": "main", "from": first, "to": second})
	if !env.OK {
		t.Fatalf("diff: %+v", env.Error)
	}
	raw, _ := json.Marshal(env.Data)
	if !strings.Contains(string(raw), `"name":"users"`) || !strings.Contains(string(raw), `"added":1`) || !strings.Contains(string(raw), `"modified":1`) {
		t.Fatalf("unexpected diff: %s", raw)
	}
	page := h.agent("snapshot.diff", map[string]any{"connection": "ro", "database": "main", "from": first, "to": second, "collection": "users", "change": "added"})
	raw, _ = json.Marshal(page.Data)
	if !page.OK || !strings.Contains(string(raw), `"total":1`) {
		t.Fatalf("page: %+v %s", page.Error, raw)
	}
	// A SQL database cannot be diffed against its live state.
	live := h.agent("snapshot.diff", map[string]any{"connection": "ro", "database": "main", "from": first})
	if live.OK || code(live) != CodeUnsupported {
		t.Fatalf("live diff on SQL: %+v", live)
	}
	if env := h.agent("snapshot.diff", map[string]any{"connection": "hidden", "database": "main", "from": first, "to": second}); code(env) != CodeAccessOff {
		t.Fatalf("hidden connection diff: %s", code(env))
	}
	if env := h.agent("snapshot.diff", map[string]any{"connection": "ro", "database": "main", "from": first, "to": second, "collection": "users", "change": "sideways"}); env.OK {
		t.Fatal("bad change type accepted")
	}
}
