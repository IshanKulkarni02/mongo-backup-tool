package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/guard"
	"github.com/IshanKulkarni02/dbhelm/internal/service"
)

// approve moves a pending request to running and executes it. by is who
// decided: "operator" (the user, in the extension) or "autopilot".
func (b *Broker) approve(id, by string) bool {
	a, ok := b.queue.transition(id, StatusPending, StatusRunning, by)
	if !ok {
		return false
	}
	b.audit(Role(by), "write.approved", a.Connection, a.Database, a.Statement, id, "approved", nil)
	if cur, ok := b.queue.Get(id); ok {
		b.publish(Event{Type: "approval.updated", Data: cur})
	}
	go b.execute(id)
	return true
}

func (b *Broker) deny(id, by string) bool {
	a, ok := b.queue.transition(id, StatusPending, StatusDenied, by)
	if !ok {
		return false
	}
	b.audit(Role(by), "write.denied", a.Connection, a.Database, a.Statement, id, "refused", nil)
	if cur, ok := b.queue.Get(id); ok {
		b.publish(Event{Type: "approval.updated", Data: cur})
	}
	return true
}

// execute runs an approved change. The agent that asked may have stopped
// waiting long ago; the result is stored on the request either way.
func (b *Broker) execute(id string) {
	a, ok := b.queue.raw(id)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	var safetyID string
	if a.safety && b.opts.SafetySnapshot {
		res, err := service.CreateSnapshot(ctx, b.engines, a.Connection, a.Database, "DBHelm safety snapshot before agent change "+id)
		if err != nil {
			eb := errBody(CodeFailed, "nothing was changed", "could not take the safety snapshot first: %s", fromErr(err).Message)
			b.queue.finish(id, nil, eb)
			b.audit("broker", "write.failed", a.Connection, a.Database, a.Statement, id, "failed", eb)
			b.publishUpdate(id)
			return
		}
		safetyID = res.Summary.ID
	}

	result, err := a.run(ctx)
	if err != nil {
		eb := fromErr(err)
		if safetyID != "" {
			eb.Hint = "the safety snapshot " + safetyID + " was taken first"
		}
		b.queue.finish(id, nil, eb)
		b.audit("broker", "write.failed", a.Connection, a.Database, a.Statement, id, "failed", eb)
	} else {
		if m, ok := result.(map[string]any); ok && safetyID != "" {
			m["safetySnapshotId"] = safetyID
			m["undo"] = "request snapshot.restore of " + safetyID + " to revert this change"
		}
		b.queue.finish(id, result, nil)
		b.audit("broker", "write.done", a.Connection, a.Database, a.Statement, id, "ok", nil)
	}
	b.publishUpdate(id)
}

func (b *Broker) publishUpdate(id string) {
	if cur, ok := b.queue.Get(id); ok {
		b.publish(Event{Type: "approval.updated", Data: cur})
	}
}

// --- operator-only methods ---

func (b *Broker) mApprovalsList(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		Finished int `json:"finished"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if p.Finished == 0 {
		p.Finished = 20
	}
	list := b.queue.List(p.Finished)
	return list, Meta{Rows: len(list)}, nil
}

func (b *Broker) mApprovalsDecide(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		ID      string `json:"id"`
		Approve bool   `json:"approve"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	cur, ok := b.queue.Get(p.ID)
	if !ok {
		return nil, Meta{}, errBody(CodeNotFound, "", "no request %q", p.ID)
	}
	var done bool
	if p.Approve {
		done = b.approve(p.ID, "operator")
	} else {
		done = b.deny(p.ID, "operator")
	}
	if !done {
		return cur, Meta{}, errBody(CodeFailed, "", "request %s was already %s", p.ID, cur.Status)
	}
	cur, _ = b.queue.Get(p.ID)
	return cur, Meta{RequestID: p.ID}, nil
}

func (b *Broker) mAutopilotGet(ctx context.Context, role Role, _ json.RawMessage) (any, Meta, *ErrorBody) {
	return b.autopilotState(), Meta{}, nil
}

func (b *Broker) mAutopilotSet(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		On             bool `json:"on"`
		AllowDangerous bool `json:"allowDangerous"`
		Minutes        int  `json:"minutes"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if p.On && !b.operatorAttached() {
		return nil, Meta{}, errBody(CodeBadRequest, "open the /events stream first", "Autopilot only runs while an operator is attached")
	}
	a := Autopilot{On: p.On, AllowDangerous: p.On && p.AllowDangerous}
	if p.On && p.Minutes > 0 {
		a.Until = now().Add(time.Duration(p.Minutes) * time.Minute).Format(time.RFC3339)
	}
	b.setAutopilot(a, "set by operator")
	return b.autopilotState(), Meta{}, nil
}

func (b *Broker) mConnectionsAll(ctx context.Context, role Role, _ json.RawMessage) (any, Meta, *ErrorBody) {
	list, err := b.listConns(true)
	if err != nil {
		return nil, Meta{}, fromErr(err)
	}
	return list, Meta{Rows: len(list)}, nil
}

func (b *Broker) mSetAccess(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		Name   string `json:"name"`
		Access string `json:"access"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if p.Access != AccessOff && p.Access != AccessRead && p.Access != AccessWrite {
		return nil, Meta{}, errBody(CodeBadRequest, "use off, read or write", "invalid access %q", p.Access)
	}
	err := config.Update(func(c *config.Config) error {
		conn, ok := c.Find(p.Name)
		if !ok {
			return fmt.Errorf("no connection named %q", p.Name)
		}
		if p.Access == AccessOff {
			conn.AgentAccess = ""
		} else {
			conn.AgentAccess = p.Access
		}
		return nil
	})
	if err != nil {
		return nil, Meta{}, errBody(CodeNotFound, "", "%v", err)
	}
	b.log.Add(LogEntry{Actor: "operator", Event: "connection.access", Connection: p.Name, Outcome: p.Access})
	list, _ := b.listConns(true)
	return list, Meta{Rows: len(list)}, nil
}

func (b *Broker) mLogbookTail(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		Limit int `json:"limit"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if p.Limit <= 0 {
		p.Limit = 100
	}
	entries := b.log.Tail(p.Limit)
	return entries, Meta{Rows: len(entries)}, nil
}

// --- operator-only: the extension's own actions (these are the user acting,
// so there is no approval step; agents cannot reach them) ---

func (b *Broker) mConnAdd(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		Name             string `json:"name"`
		URI              string `json:"uri"`
		Engine           string `json:"engine"`
		Environment      string `json:"environment"`
		ReadOnly         bool   `json:"readOnly"`
		AgentAccess      string `json:"agentAccess"`
		SSHHost          string `json:"sshHost"`
		SSHUser          string `json:"sshUser"`
		SSHPassword      string `json:"sshPassword"`
		SSHPrivateKey    string `json:"sshPrivateKey"`
		TenantSessionVar string `json:"tenantSessionVar"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	err := service.AddConnection(service.ConnectionInput{
		Name: p.Name, URI: p.URI, Engine: p.Engine, Environment: p.Environment, ReadOnly: p.ReadOnly,
		AgentAccess: p.AgentAccess, SSHHost: p.SSHHost, SSHUser: p.SSHUser, SSHPassword: p.SSHPassword,
		SSHPrivateKey: p.SSHPrivateKey, TenantSessionVar: p.TenantSessionVar,
	})
	if err != nil {
		return nil, Meta{}, errBody(CodeBadRequest, "", "%s", guard.Scrub(err.Error()))
	}
	b.engines.Invalidate(p.Name)
	b.log.Add(LogEntry{Actor: "operator", Event: "connection.saved", Connection: p.Name})
	list, _ := b.listConns(true)
	return list, Meta{Rows: len(list)}, nil
}

func (b *Broker) mConnRemove(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		Name string `json:"name"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if err := service.RemoveConnection(p.Name); err != nil {
		return nil, Meta{}, errBody(CodeNotFound, "", "%v", err)
	}
	b.engines.Invalidate(p.Name)
	b.log.Add(LogEntry{Actor: "operator", Event: "connection.removed", Connection: p.Name})
	list, _ := b.listConns(true)
	return list, Meta{Rows: len(list)}, nil
}

func (b *Broker) mConnTest(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		Name string `json:"name"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	sess, release, eb := b.session(ctx, p.Name)
	if eb != nil {
		return nil, Meta{}, eb
	}
	defer release()
	if err := sess.Ping(ctx); err != nil {
		b.engines.Invalidate(p.Name) // don't keep serving a session that failed its health check
		return nil, Meta{}, errBody(CodeConnectionFailed, "check the URI, network and credentials", "%s", guard.Scrub(err.Error()))
	}
	names, err := service.DatabaseNames(ctx, sess)
	if err != nil {
		return nil, Meta{}, fromErr(err)
	}
	return names, Meta{Rows: len(names)}, nil
}

func (b *Broker) mSnapRestoreNow(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		target
		SnapshotID string `json:"snapshotId"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	out, err := service.RestoreSnapshot(ctx, b.engines, p.Connection, p.Database, p.SnapshotID)
	if err != nil {
		return nil, Meta{}, fromErr(err)
	}
	b.log.Add(LogEntry{Actor: "operator", Event: "snapshot.restored", Connection: p.Connection, Database: p.Database, Detail: p.SnapshotID, Outcome: "ok"})
	return map[string]any{"restored": out.Result, "safetySnapshotId": out.SafetySnapshotID}, Meta{}, nil
}

func (b *Broker) mBackupsList(ctx context.Context, role Role, _ json.RawMessage) (any, Meta, *ErrorBody) {
	list, err := service.ListBackups()
	if err != nil {
		return nil, Meta{}, fromErr(err)
	}
	return list, Meta{Rows: len(list)}, nil
}

func (b *Broker) mBackupsCreate(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p target
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	id, err := service.CreateBackup(p.Connection, p.Database)
	if err != nil {
		return nil, Meta{}, fromErr(err)
	}
	b.log.Add(LogEntry{Actor: "operator", Event: "backup.created", Connection: p.Connection, Database: p.Database, Detail: id, Outcome: "ok"})
	return map[string]string{"backupId": id}, Meta{}, nil
}

func (b *Broker) mBackupsRestore(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		Connection string `json:"connection"`
		BackupID   string `json:"backupId"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if err := service.RestoreBackup(p.Connection, p.BackupID); err != nil {
		return nil, Meta{}, fromErr(err)
	}
	b.log.Add(LogEntry{Actor: "operator", Event: "backup.restored", Connection: p.Connection, Detail: p.BackupID, Outcome: "ok"})
	return map[string]any{"restored": true}, Meta{}, nil
}

func (b *Broker) mBackupsDelete(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		ID string `json:"id"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if err := service.DeleteBackup(p.ID); err != nil {
		return nil, Meta{}, errBody(CodeNotFound, "", "%v", err)
	}
	return map[string]any{"deleted": p.ID}, Meta{}, nil
}

// mConnUpdate changes a saved connection's non-secret settings without
// touching its credentials: the extension never has the URI's password, so it
// cannot (and must not) re-send it just to flip a setting.
func (b *Broker) mConnUpdate(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		Name        string  `json:"name"`
		Environment *string `json:"environment"`
		ReadOnly    *bool   `json:"readOnly"`
		AgentAccess *string `json:"agentAccess"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if p.Environment != nil {
		switch *p.Environment {
		case "", "dev", "staging", "prod":
		default:
			return nil, Meta{}, errBody(CodeBadRequest, "use dev, staging or prod", "invalid environment %q", *p.Environment)
		}
	}
	if p.AgentAccess != nil && *p.AgentAccess != AccessOff && *p.AgentAccess != AccessRead && *p.AgentAccess != AccessWrite {
		return nil, Meta{}, errBody(CodeBadRequest, "use off, read or write", "invalid access %q", *p.AgentAccess)
	}
	err := config.Update(func(c *config.Config) error {
		conn, ok := c.Find(p.Name)
		if !ok {
			return fmt.Errorf("no connection named %q", p.Name)
		}
		if p.Environment != nil {
			conn.Environment = *p.Environment
		}
		if p.ReadOnly != nil {
			conn.ReadOnly = *p.ReadOnly
		}
		if p.AgentAccess != nil {
			if *p.AgentAccess == AccessOff {
				conn.AgentAccess = ""
			} else {
				conn.AgentAccess = *p.AgentAccess
			}
		}
		return nil
	})
	if err != nil {
		return nil, Meta{}, errBody(CodeNotFound, "", "%v", err)
	}
	b.engines.Invalidate(p.Name)
	b.log.Add(LogEntry{Actor: "operator", Event: "connection.updated", Connection: p.Name})
	list, _ := b.listConns(true)
	return list, Meta{Rows: len(list)}, nil
}

func (b *Broker) mConnSetPassword(ctx context.Context, role Role, raw json.RawMessage) (any, Meta, *ErrorBody) {
	var p struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if eb := decode(raw, &p); eb != nil {
		return nil, Meta{}, eb
	}
	if err := service.SetConnectionPassword(p.Name, p.Password); err != nil {
		return nil, Meta{}, errBody(CodeBadRequest, "", "%s", guard.Scrub(err.Error()))
	}
	b.engines.Invalidate(p.Name)
	b.log.Add(LogEntry{Actor: "operator", Event: "connection.password", Connection: p.Name}) // never the value
	return map[string]any{"set": true}, Meta{}, nil
}
