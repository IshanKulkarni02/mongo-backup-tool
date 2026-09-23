package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/engine/safeguard"
	"github.com/IshanKulkarni02/dbhelm/internal/guard"
	"github.com/IshanKulkarni02/dbhelm/internal/service"
	"github.com/IshanKulkarni02/dbhelm/internal/snapshot"
)

// Access levels for agents on one connection (config.Connection.AgentAccess).
const (
	AccessOff   = "off"
	AccessRead  = "read"
	AccessWrite = "write"
)

func normalizeAccess(s string) string {
	switch s {
	case AccessRead, AccessWrite:
		return s
	}
	return AccessOff
}

func (b *Broker) registerMethods() {
	b.agentMethods = map[string]handler{
		"ping":             b.mPing,
		"shutdown":         b.mShutdown,
		"connections":      b.mConnections,
		"databases":        b.mDatabases,
		"schema":           b.mSchema,
		"describe":         b.mDescribe,
		"query":            b.mQuery,
		"explain":          b.mExplain,
		"snapshot.list":    b.mSnapshotList,
		"snapshot.create":  b.mSnapshotCreate,
		"snapshot.diff":    b.mSnapshotDiff,
		"snapshot.restore": b.mSnapshotRestore,
		"write":            b.mWrite,
		"request.get":      b.mRequestGet,
	}
	b.operatorMethods = map[string]handler{
		"approvals.list":         b.mApprovalsList,
		"approvals.decide":       b.mApprovalsDecide,
		"autopilot.get":          b.mAutopilotGet,
		"autopilot.set":          b.mAutopilotSet,
		"connections.list":       b.mConnectionsAll,
		"connections.setAccess":  b.mSetAccess,
		"logbook.tail":           b.mLogbookTail,
		"connection.add":         b.mConnAdd,
		"connection.update":      b.mConnUpdate,
		"connection.setPassword": b.mConnSetPassword,
		"connection.remove":      b.mConnRemove,
		"connection.test":        b.mConnTest,
		"snapshots.restore":      b.mSnapRestoreNow,
		"backups.list":           b.mBackupsList,
		"backups.create":         b.mBackupsCreate,
		"backups.restore":        b.mBackupsRestore,
		"backups.delete":         b.mBackupsDelete,
	}
}

func decode(raw json.RawMessage, v any) *ErrorBody {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return errBody(CodeBadRequest, "check the parameter names and types", "invalid params: %v", err)
	}
	return nil
}

// fromErr converts any error into an envelope error, scrubbing it first.
func fromErr(err error) *ErrorBody {
	var ge *guard.Error
	if errors.As(err, &ge) {
		return &ErrorBody{Code: string(ge.Code), Message: ge.Message, Hint: ge.Hint}
	}
	var eb *ErrorBody
	if errors.As(err, &eb) {
		return eb
	}
	if errors.Is(err, engine.ErrReadOnly) {
		return errBody(CodeReadOnly, "this connection is marked read-only in DBHelm", "connection is read-only")
	}
	return errBody(CodeFailed, "", "%s", guard.Scrub(err.Error()))
}

// connAccess resolves a connection for a caller. Agents only get connections
// whose AgentAccess allows the requested level; a connection that is off and
// one that does not exist look identical so names can't be enumerated.
func (b *Broker) connAccess(role Role, name string, need string) (*config.Connection, *ErrorBody) {
	conn, err := service.ResolveConn(name)
	if err != nil {
		if role == RoleAgent {
			return nil, errBody(CodeAccessOff, "run `dbhelm agent connections` to see what is available", "connection %q is not available to agents", name)
		}
		return nil, errBody(CodeNotFound, "", "%v", err)
	}
	if role == RoleOperator {
		return conn, nil
	}
	level := normalizeAccess(conn.AgentAccess)
	if level == AccessOff || (need == AccessWrite && level != AccessWrite) {
		hint := "the user enables agent access per connection in the DBHelm extension"
		if level == AccessRead {
			hint = "this connection is read-only for agents; the user can allow write requests in the DBHelm extension"
		}
		return nil, errBody(CodeAccessOff, hint, "connection %q is not available to agents for %s", name, need)
	}
	return conn, nil
}

func (b *Broker) audit(role Role, event, conn, db, stmt, id, outcome string, eb *ErrorBody) {
	e := LogEntry{Actor: string(role), Event: event, Connection: conn, Database: db, Statement: stmt, RequestID: id, Outcome: outcome}
	if eb != nil {
		e.Code, e.Detail = eb.Code, eb.Message
	}
	b.log.Add(e)
	b.publish(Event{Type: "activity", Data: e})
}

// --- simple methods ---

func (b *Broker) mPing(ctx context.Context, role Role, _ json.RawMessage) (any, Meta, *ErrorBody) {
	auto := b.autopilotState()
	return map[string]any{
		"protocol": ProtocolVersion, "version": b.opts.Version, "headless": b.opts.Headless,
		"operatorAttached": b.operatorAttached(), "autopilot": auto.On, "role": role,
	}, Meta{}, nil
}

func (b *Broker) mShutdown(ctx context.Context, role Role, _ json.RawMessage) (any, Meta, *ErrorBody) {
	if role == RoleAgent && !b.opts.Headless {
		return nil, Meta{}, errBody(CodeUnauthorized, "", "an agent cannot stop an operator's broker")
	}
	go func() { time.Sleep(50 * time.Millisecond); b.Close() }()
	return map[string]any{"stopping": true}, Meta{}, nil
}

type connView struct {
	Name         string      `json:"name"`
	Engine       string      `json:"engine"`
	Capabilities engine.Caps `json:"capabilities"`
	Access       string      `json:"access"`
	Environment  string      `json:"environment,omitempty"`
	ReadOnly     bool        `json:"readOnly,omitempty"`
}

func (b *Broker) listConns(all bool) ([]connView, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	out := []connView{}
	for _, c := range cfg.Connections {
		access := normalizeAccess(c.AgentAccess)
		if !all && access == AccessOff {
			continue
		}
		var caps engine.Caps
		if eng, err := engine.Lookup(c.EngineID()); err == nil {
			caps = eng.Capabilities()
		}
		out = append(out, connView{Name: c.Name, Engine: c.EngineID(), Capabilities: caps, Access: access, Environment: c.Environment, ReadOnly: c.ReadOnly})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (b *Broker) mConnections(ctx context.Context, role Role, _ json.RawMessage) (any, Meta, *ErrorBody) {
	list, err := b.listConns(role == RoleOperator)
	if err != nil {
		return nil, Meta{}, fromErr(err)
	}
	b.audit(role, "connections", "", "", "", "", "ok", nil)
	return list, Meta{Rows: len(list)}, nil
}

type target struct {
	Connection string `json:"connection"`
	Database   string `json:"database"`
}

func (b *Broker) session(ctx context.Context, name string) (engine.Session, func(), *ErrorBody) {
	sess, release, err := b.engines.Acquire(ctx, name)
	if err != nil {
		return nil, nil, errBody(CodeConnectionFailed, "check the connection in DBHelm", "%s", guard.Scrub(err.Error()))
	}
	return sess, release, nil
}

func (b *Broker) mDatabases(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p target
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if _, eb := b.connAccess(role, p.Connection, AccessRead); eb != nil {
		return nil, Meta{}, eb
	}
	sess, release, eb := b.session(ctx, p.Connection)
	if eb != nil {
		return nil, Meta{}, eb
	}
	defer release()
	names, err := service.DatabaseNames(ctx, sess)
	if err != nil {
		return nil, Meta{}, fromErr(err)
	}
	return names, Meta{Rows: len(names)}, nil
}

func (b *Broker) mSchema(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p target
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if p.Database == "" {
		return nil, Meta{}, errBody(CodeBadRequest, "run `databases` to list them", "database is required")
	}
	if _, eb := b.connAccess(role, p.Connection, AccessRead); eb != nil {
		return nil, Meta{}, eb
	}
	sess, release, eb := b.session(ctx, p.Connection)
	if eb != nil {
		return nil, Meta{}, eb
	}
	defer release()
	ns, err := sess.ListNamespaces(ctx, p.Database)
	if err != nil {
		return nil, Meta{}, fromErr(err)
	}
	b.audit(role, "schema", p.Connection, p.Database, "", "", "ok", nil)
	return ns, Meta{Rows: len(ns)}, nil
}

func (b *Broker) mDescribe(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		target
		Table      string `json:"table"`
		Collection string `json:"collection"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	name := p.Table
	if name == "" {
		name = p.Collection
	}
	if p.Database == "" || name == "" {
		return nil, Meta{}, errBody(CodeBadRequest, "pass database and table (or collection)", "database and table are required")
	}
	if _, eb := b.connAccess(role, p.Connection, AccessRead); eb != nil {
		return nil, Meta{}, eb
	}
	sess, release, eb := b.session(ctx, p.Connection)
	if eb != nil {
		return nil, Meta{}, eb
	}
	defer release()
	switch s := sess.(type) {
	case engine.SQLSession:
		schema, err := s.TableSchema(ctx, p.Database, name)
		if err != nil {
			return nil, Meta{}, fromErr(err)
		}
		idx, _ := s.ListTableIndexes(ctx, p.Database, name)
		b.audit(role, "describe", p.Connection, p.Database, name, "", "ok", nil)
		return map[string]any{"schema": schema, "indexes": idx}, Meta{}, nil
	case engine.DocumentSession:
		idx, err := s.ListIndexes(ctx, p.Database, name)
		if err != nil {
			return nil, Meta{}, fromErr(err)
		}
		page, _, err := guard.ReadDocuments(ctx, s, engine.DocQuery{Database: p.Database, Namespace: name, Limit: 1}, guard.Limits{})
		if err != nil {
			return nil, Meta{}, fromErr(err)
		}
		b.audit(role, "describe", p.Connection, p.Database, name, "", "ok", nil)
		return map[string]any{"indexes": idx, "sample": page.Documents, "approxCount": page.Total}, Meta{}, nil
	}
	return nil, Meta{}, errBody(CodeUnsupported, "", "engine does not support describe")
}

// --- reads ---

type queryParams struct {
	target
	SQL        string `json:"sql"`
	Collection string `json:"collection"`
	Filter     string `json:"filter"`
	Sort       string `json:"sort"`
	Skip       int    `json:"skip"`
	Limit      int    `json:"limit"`
	Pipeline   string `json:"pipeline"`
	MaxRows    int    `json:"maxRows"`
}

const maxAgentRows = 1000

func (b *Broker) mQuery(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p queryParams
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	conn, eb := b.connAccess(role, p.Connection, AccessRead)
	if eb != nil {
		return nil, Meta{}, eb
	}
	lim := guard.Limits{MaxRows: min(p.MaxRows, maxAgentRows)}
	sess, release, eb := b.session(ctx, conn.Name)
	if eb != nil {
		return nil, Meta{}, eb
	}
	defer release()

	stmt := p.SQL
	switch s := sess.(type) {
	case engine.SQLSession:
		if p.Database == "" || strings.TrimSpace(p.SQL) == "" {
			return nil, Meta{}, errBody(CodeBadRequest, "pass database and sql", "database and sql are required")
		}
		res, err := guard.ReadSQL(ctx, s, p.Database, p.SQL, lim)
		if err != nil {
			eb := fromErr(err)
			b.audit(role, "query", conn.Name, p.Database, stmt, "", "refused", eb)
			return nil, Meta{}, eb
		}
		b.audit(role, "query", conn.Name, p.Database, stmt, "", "ok", nil)
		return res, Meta{Rows: len(res.Rows), Truncated: res.Truncated}, nil
	case engine.DocumentSession:
		if p.Database == "" || p.Collection == "" {
			return nil, Meta{}, errBody(CodeBadRequest, "pass database and collection", "database and collection are required")
		}
		if p.Pipeline != "" {
			as, ok := sess.(engine.AggregateSession)
			if !ok {
				return nil, Meta{}, errBody(CodeUnsupported, "", "engine has no aggregation pipelines")
			}
			stmt = p.Pipeline
			docs, trunc, err := guard.Aggregate(ctx, as, p.Database, p.Collection, p.Pipeline, lim)
			if err != nil {
				eb := fromErr(err)
				b.audit(role, "query", conn.Name, p.Database, stmt, "", "refused", eb)
				return nil, Meta{}, eb
			}
			b.audit(role, "query", conn.Name, p.Database, stmt, "", "ok", nil)
			return map[string]any{"documents": docs}, Meta{Rows: len(docs), Truncated: trunc}, nil
		}
		stmt = p.Filter
		page, trunc, err := guard.ReadDocuments(ctx, s, engine.DocQuery{
			Database: p.Database, Namespace: p.Collection, FilterJSON: p.Filter, SortJSON: p.Sort, Skip: p.Skip, Limit: p.Limit,
		}, lim)
		if err != nil {
			eb := fromErr(err)
			b.audit(role, "query", conn.Name, p.Database, stmt, "", "refused", eb)
			return nil, Meta{}, eb
		}
		b.audit(role, "query", conn.Name, p.Database, stmt, "", "ok", nil)
		return page, Meta{Rows: len(page.Documents), Truncated: trunc}, nil
	}
	return nil, Meta{}, errBody(CodeUnsupported, "", "engine does not support queries")
}

func (b *Broker) mExplain(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p queryParams
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if _, eb := b.connAccess(role, p.Connection, AccessRead); eb != nil {
		return nil, Meta{}, eb
	}
	sess, release, eb := b.session(ctx, p.Connection)
	if eb != nil {
		return nil, Meta{}, eb
	}
	defer release()
	ss, ok := sess.(engine.SQLSession)
	if !ok {
		return nil, Meta{}, errBody(CodeUnsupported, "", "explain is only available for SQL connections")
	}
	plan, err := guard.Explain(ctx, ss, p.Database, p.SQL)
	if err != nil {
		return nil, Meta{}, fromErr(err)
	}
	return map[string]any{"plan": plan}, Meta{}, nil
}

// --- snapshots ---

func (b *Broker) mSnapshotList(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p target
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if _, eb := b.connAccess(role, p.Connection, AccessRead); eb != nil {
		return nil, Meta{}, eb
	}
	items, err := snapshot.Log(p.Connection, p.Database)
	if err != nil {
		return nil, Meta{}, fromErr(err)
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 { // newest first
		items[i], items[j] = items[j], items[i]
	}
	return items, Meta{Rows: len(items)}, nil
}

// mSnapshotCreate is allowed at read access: a snapshot only writes DBHelm's
// own store and reads the database, and it is what makes later changes undoable.
func (b *Broker) mSnapshotCreate(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		target
		Message string `json:"message"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if p.Database == "" {
		return nil, Meta{}, errBody(CodeBadRequest, "", "database is required")
	}
	if _, eb := b.connAccess(role, p.Connection, AccessRead); eb != nil {
		return nil, Meta{}, eb
	}
	msg := strings.TrimSpace(p.Message)
	if msg == "" {
		msg = "snapshot requested by agent"
	}
	res, err := service.CreateSnapshot(ctx, b.engines, p.Connection, p.Database, msg)
	if err != nil {
		eb := fromErr(err)
		b.audit(role, "snapshot.create", p.Connection, p.Database, msg, "", "failed", eb)
		return nil, Meta{}, eb
	}
	b.audit(role, "snapshot.create", p.Connection, p.Database, msg, "", "ok", nil)
	return res.Summary, Meta{}, nil
}

// --- writes (approval flow) ---

type writeParams struct {
	target
	Kind       string `json:"kind"` // sql | doc; inferred from the engine when empty
	Statement  string `json:"statement"`
	Collection string `json:"collection"`
	Op         string `json:"op"` // insert | update | delete
	Document   string `json:"document"`
	ID         string `json:"id"`
	Reason     string `json:"reason"`
	Wait       int    `json:"wait"` // seconds to wait for a decision
}

const maxWaitSeconds = 600

func waitFor(sec int) time.Duration {
	if sec <= 0 {
		sec = 120
	}
	return time.Duration(min(sec, maxWaitSeconds)) * time.Second
}

func (b *Broker) mWrite(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p writeParams
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if p.Database == "" {
		return nil, Meta{}, errBody(CodeBadRequest, "", "database is required")
	}
	conn, eb := b.connAccess(role, p.Connection, AccessWrite)
	if eb != nil {
		return nil, Meta{}, eb
	}
	if err := service.RequireWritable(conn.Name); err != nil {
		return nil, Meta{}, fromErr(err)
	}
	eng, err := engine.Lookup(conn.EngineID())
	if err != nil {
		return nil, Meta{}, fromErr(err)
	}
	kind := p.Kind
	if kind == "" {
		if eng.Capabilities().SQL {
			kind = "sql"
		} else {
			kind = "doc"
		}
	}

	a := &Approval{Kind: kind, Connection: conn.Name, Database: p.Database, Reason: strings.TrimSpace(p.Reason)}
	switch kind {
	case "sql":
		if !eng.Capabilities().SQL {
			return nil, Meta{}, errBody(CodeBadRequest, "use kind \"doc\" for this connection", "connection is not a SQL database")
		}
		if strings.TrimSpace(p.Statement) == "" {
			return nil, Meta{}, errBody(CodeBadRequest, "", "statement is required")
		}
		class := safeguard.Classify(p.Statement)
		a.Statement, a.Risk = p.Statement, riskName(class.Risk)
		if class.Reason != "" && a.Reason == "" {
			a.Reason = class.Reason
		}
		stmt := p.Statement
		a.run = func(ctx context.Context) (any, error) {
			sess, release, err := service.SQLSessionFrom(ctx, b.engines, conn.Name)
			if err != nil {
				return nil, err
			}
			defer release()
			n, err := sess.Execute(ctx, p.Database, stmt)
			if err != nil {
				return nil, err
			}
			return map[string]any{"rowsAffected": n}, nil
		}
	case "doc":
		if eng.Capabilities().SQL || p.Collection == "" {
			return nil, Meta{}, errBody(CodeBadRequest, "pass collection and op (insert|update|delete)", "a document write needs a document connection and a collection")
		}
		op := strings.ToLower(p.Op)
		switch op {
		case "insert":
			if p.Document == "" {
				return nil, Meta{}, errBody(CodeBadRequest, "", "document is required")
			}
			a.Risk = "none"
		case "update":
			if p.Document == "" || p.ID == "" {
				return nil, Meta{}, errBody(CodeBadRequest, "pass id (the _id as JSON) and the full replacement document", "id and document are required")
			}
			a.Risk = "confirm"
		case "delete":
			if p.ID == "" {
				return nil, Meta{}, errBody(CodeBadRequest, "pass id (the _id as JSON)", "id is required")
			}
			a.Risk = "confirm"
		default:
			return nil, Meta{}, errBody(CodeBadRequest, "op must be insert, update or delete", "unknown op %q", p.Op)
		}
		a.Statement = describeDocWrite(op, p)
		a.run = func(ctx context.Context) (any, error) {
			sess, release, err := b.engines.Acquire(ctx, conn.Name)
			if err != nil {
				return nil, err
			}
			defer release()
			ds, ok := sess.(engine.DocumentSession)
			if !ok {
				return nil, fmt.Errorf("connection does not support document writes")
			}
			switch op {
			case "insert":
				err = ds.InsertDocument(ctx, p.Database, p.Collection, p.Document)
			case "update":
				err = ds.UpdateDocument(ctx, p.Database, p.Collection, p.ID, p.Document)
			case "delete":
				err = ds.DeleteDocument(ctx, p.Database, p.Collection, p.ID)
			}
			if err != nil {
				return nil, err
			}
			return map[string]any{"ok": true}, nil
		}
	default:
		return nil, Meta{}, errBody(CodeBadRequest, "kind must be sql or doc", "unknown kind %q", kind)
	}
	return b.submit(ctx, role, a, waitFor(p.Wait), true)
}

func describeDocWrite(op string, p writeParams) string {
	switch op {
	case "insert":
		return fmt.Sprintf("insert into %s.%s: %s", p.Database, p.Collection, p.Document)
	case "update":
		return fmt.Sprintf("replace %s.%s _id=%s with: %s", p.Database, p.Collection, p.ID, p.Document)
	}
	return fmt.Sprintf("delete from %s.%s where _id=%s", p.Database, p.Collection, p.ID)
}

func riskName(r safeguard.Risk) string {
	switch r {
	case safeguard.RiskDangerous:
		return "dangerous"
	case safeguard.RiskConfirm:
		return "confirm"
	}
	return "none"
}

func (b *Broker) mSnapshotRestore(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		target
		SnapshotID string `json:"snapshotId"`
		Reason     string `json:"reason"`
		Wait       int    `json:"wait"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if p.Database == "" || p.SnapshotID == "" {
		return nil, Meta{}, errBody(CodeBadRequest, "run `snapshot list` for ids", "database and snapshotId are required")
	}
	conn, eb := b.connAccess(role, p.Connection, AccessWrite)
	if eb != nil {
		return nil, Meta{}, eb
	}
	if err := service.RequireWritable(conn.Name); err != nil {
		return nil, Meta{}, fromErr(err)
	}
	a := &Approval{
		Kind: "restore", Connection: conn.Name, Database: p.Database, Risk: "dangerous",
		Statement: fmt.Sprintf("restore snapshot %s over %s (replaces current data; a safety snapshot is taken first)", p.SnapshotID, p.Database),
		Reason:    strings.TrimSpace(p.Reason),
	}
	a.run = func(ctx context.Context) (any, error) {
		out, err := service.RestoreSnapshot(ctx, b.engines, conn.Name, p.Database, p.SnapshotID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"restored": out.Result, "safetySnapshotId": out.SafetySnapshotID}, nil
	}
	// A restore takes its own safety snapshot, so the broker does not add one.
	return b.submit(ctx, role, a, waitFor(p.Wait), false)
}

// submit queues a change, applies the approval policy, and waits for the
// outcome (up to wait).
func (b *Broker) submit(ctx context.Context, role Role, a *Approval, wait time.Duration, safety bool) (any, Meta, *ErrorBody) {
	a.safety = safety
	b.queue.Add(a)
	id := a.ID
	b.audit(role, "write.requested", a.Connection, a.Database, a.Statement, id, "pending", nil)

	switch {
	case !b.operatorAttached():
		b.queue.transition(id, StatusPending, StatusDenied, "broker")
		eb := errBody(CodeNoOperator, "open the workspace in VS Code with the DBHelm extension so the user can approve, or ask the user to run the change", "no DBHelm operator is attached to approve changes")
		b.audit(role, "write.refused", a.Connection, a.Database, a.Statement, id, "refused", eb)
		return map[string]any{"requestId": id, "status": StatusDenied}, Meta{RequestID: id}, eb
	default:
		if auto := b.autopilotState(); auto.On && (a.Risk != "dangerous" || auto.AllowDangerous) {
			b.approve(id, "autopilot")
		} else {
			cur, _ := b.queue.Get(id)
			b.publish(Event{Type: "approval.pending", Data: cur})
		}
	}
	cur, _ := b.queue.Wait(ctx, id, wait)
	return b.outcome(cur)
}

// outcome turns an approval's current state into the response an agent sees.
func (b *Broker) outcome(a Approval) (any, Meta, *ErrorBody) {
	meta := Meta{RequestID: a.ID}
	view := map[string]any{"requestId": a.ID, "status": a.Status, "decidedBy": a.DecidedBy}
	switch a.Status {
	case StatusDone:
		view["result"] = a.Result
		return view, meta, nil
	case StatusFailed:
		return view, meta, a.Error
	case StatusDenied:
		return view, meta, errBody(CodeDenied, "the user refused this change; do not retry it or work around it, ask the user how to proceed", "the change was refused")
	case StatusExpired:
		return view, meta, errBody(CodeTimeout, "the request expired without a decision; ask the user before sending it again", "no decision was made in time")
	case StatusCanceled:
		return view, meta, errBody(CodeFailed, "", "the broker stopped before this request was decided")
	}
	return view, meta, errBody(CodePendingApproval, "a person is deciding; wait with `request "+a.ID+" --wait`, do not send it again", "waiting for the user to approve")
}

func (b *Broker) mRequestGet(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		ID   string `json:"id"`
		Wait int    `json:"wait"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	a, ok := b.queue.Get(p.ID)
	if !ok {
		return nil, Meta{}, errBody(CodeNotFound, "", "no request %q", p.ID)
	}
	if role == RoleAgent {
		if _, eb := b.connAccess(role, a.Connection, AccessRead); eb != nil {
			return nil, Meta{}, errBody(CodeNotFound, "", "no request %q", p.ID)
		}
	}
	if p.Wait > 0 {
		a, _ = b.queue.Wait(ctx, p.ID, waitFor(p.Wait))
	}
	return b.outcome(a)
}

// mSnapshotDiff compares two snapshots (or, for Mongo, a snapshot with the
// live database) and returns per-collection change counts; with a
// collection and change type it returns one bounded page of changed ids.
func (b *Broker) mSnapshotDiff(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		target
		From       string `json:"from"`
		To         string `json:"to"`
		Collection string `json:"collection"`
		Change     string `json:"change"` // added | modified | removed
		Offset     int    `json:"offset"`
		Limit      int    `json:"limit"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if p.Database == "" || p.From == "" {
		return nil, Meta{}, errBody(CodeBadRequest, "run `snapshot list` for ids", "database and from are required")
	}
	conn, eb := b.connAccess(role, p.Connection, AccessRead)
	if eb != nil {
		return nil, Meta{}, eb
	}
	liveURI := ""
	if p.To == "" {
		if eng, err := engine.Lookup(conn.EngineID()); err == nil && eng.Capabilities().SQL {
			return nil, Meta{}, errBody(CodeUnsupported, "take a snapshot now, then diff the two snapshots", "diffing against the live database is not supported for SQL connections")
		}
		liveURI = conn.URI
	}
	d, err := snapshot.OpenDiff(conn.Name, p.Database, p.From, p.To, liveURI)
	if err != nil {
		return nil, Meta{}, fromErr(err)
	}
	defer d.Close()

	if p.Collection != "" {
		var ct snapshot.ChangeType
		switch p.Change {
		case "added":
			ct = snapshot.Added
		case "modified":
			ct = snapshot.Modified
		case "removed":
			ct = snapshot.Removed
		default:
			return nil, Meta{}, errBody(CodeBadRequest, "change must be added, modified or removed", "unknown change %q", p.Change)
		}
		limit := p.Limit
		if limit <= 0 || limit > 100 {
			limit = 100
		}
		ids, total, err := d.CollectionPage(ctx, p.Collection, ct, p.Offset, limit)
		if err != nil {
			return nil, Meta{}, fromErr(err)
		}
		if ids == nil {
			ids = []string{}
		}
		b.audit(role, "snapshot.diff", conn.Name, p.Database, p.Collection+" "+p.Change, "", "ok", nil)
		return map[string]any{"ids": ids, "total": total, "offset": p.Offset}, Meta{Rows: len(ids), Truncated: p.Offset+len(ids) < total}, nil
	}

	diff, err := d.Compare(ctx)
	if err != nil {
		return nil, Meta{}, fromErr(err)
	}
	type row struct {
		Name     string `json:"name"`
		Added    int    `json:"added"`
		Modified int    `json:"modified"`
		Removed  int    `json:"removed"`
	}
	names := make([]string, 0, len(diff.Collections))
	for n := range diff.Collections {
		names = append(names, n)
	}
	sort.Strings(names)
	rows := make([]row, 0, len(names))
	for _, n := range names {
		c := diff.Collections[n]
		rows = append(rows, row{n, c.AddedCount, c.ModifiedCount, c.RemovedCount})
	}
	b.audit(role, "snapshot.diff", conn.Name, p.Database, p.From+".."+p.To, "", "ok", nil)
	return map[string]any{"from": diff.FromID, "to": diff.ToID, "collections": rows}, Meta{Rows: len(rows)}, nil
}
