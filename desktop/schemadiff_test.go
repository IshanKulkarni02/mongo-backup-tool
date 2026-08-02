package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

// fakeSchemaSession is a minimal engine.SQLSession for exercising
// collectSchemas: it lists a fixed set of namespaces and lets a test
// script per-table TableSchema failures and a Ping outcome, without
// needing a real database connection.
type fakeSchemaSession struct {
	namespaces []engine.NamespaceInfo
	schemaErr  map[string]error // table name -> error TableSchema should return
	pingErr    error
	pingCalls  int
}

func (f *fakeSchemaSession) Ping(ctx context.Context) error {
	f.pingCalls++
	return f.pingErr
}
func (f *fakeSchemaSession) ListDatabases(ctx context.Context) ([]string, error) { return nil, nil }
func (f *fakeSchemaSession) ListNamespaces(ctx context.Context, database string) ([]engine.NamespaceInfo, error) {
	return f.namespaces, nil
}
func (f *fakeSchemaSession) Close(ctx context.Context) error { return nil }
func (f *fakeSchemaSession) TableSchema(ctx context.Context, database, table string) (engine.TableSchema, error) {
	if err, ok := f.schemaErr[table]; ok {
		return engine.TableSchema{}, err
	}
	return engine.TableSchema{Name: table}, nil
}
func (f *fakeSchemaSession) Query(ctx context.Context, database, sqlText string) (engine.SQLResult, error) {
	return engine.SQLResult{}, nil
}
func (f *fakeSchemaSession) Execute(ctx context.Context, database, sqlText string) (int64, error) {
	return 0, nil
}
func (f *fakeSchemaSession) Explain(ctx context.Context, database, sqlText string) (string, error) {
	return "", nil
}
func (f *fakeSchemaSession) ListTableIndexes(ctx context.Context, database, table string) ([]engine.IndexDef, error) {
	return nil, nil
}
func (f *fakeSchemaSession) BeginConsistentRead(ctx context.Context) (engine.ConsistentReadTx, error) {
	return nil, fmt.Errorf("not implemented")
}

type fakeSchemaEngine struct {
	sess *fakeSchemaSession
}

func (e *fakeSchemaEngine) ID() string              { return "fake" }
func (e *fakeSchemaEngine) Capabilities() engine.Caps { return engine.Caps{} }
func (e *fakeSchemaEngine) Open(ctx context.Context, cfg engine.ConnConfig) (engine.Session, error) {
	return e.sess, nil
}

func newSchemaDiffTestApp(sess *fakeSchemaSession) *App {
	eng := &fakeSchemaEngine{sess: sess}
	mgr := engine.NewManager(func(name string) (engine.ConnConfig, engine.Engine, error) {
		return engine.ConnConfig{Name: name}, eng, nil
	})
	return &App{engines: mgr}
}

// TestCollectSchemasSkipsOnlyTheFailingTable confirms the ordinary case
// (an oddball table that just can't be introspected) still behaves as
// before: it's dropped from the result, and the rest of the schema comes
// back intact.
func TestCollectSchemasSkipsOnlyTheFailingTable(t *testing.T) {
	sess := &fakeSchemaSession{
		namespaces: []engine.NamespaceInfo{{Name: "users"}, {Name: "weird_view"}, {Name: "orders"}},
		schemaErr:  map[string]error{"weird_view": errors.New("unsupported view definition")},
	}
	app := newSchemaDiffTestApp(sess)

	schemas, err := app.collectSchemas("conn", "db")
	if err != nil {
		t.Fatalf("collectSchemas returned an error for a single bad table: %v", err)
	}
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas (weird_view skipped), got %d: %+v", len(schemas), schemas)
	}
	if sess.pingCalls != 1 {
		t.Fatalf("expected exactly 1 Ping call (to check the one failure), got %d", sess.pingCalls)
	}
}

// TestCollectSchemasAbortsOnConnectionDrop is the regression test for
// #64: previously, once TableSchema started failing (e.g. because the
// connection dropped), every remaining table was silently treated as
// "doesn't participate in the diff" — indistinguishable from a genuinely
// missing table. A schema diff run against a connection that died midway
// would then report every table after the drop point as dropped, and a
// generated migration could contain DROP TABLE statements for tables
// that still exist. Ping must catch this and fail the whole call instead.
func TestCollectSchemasAbortsOnConnectionDrop(t *testing.T) {
	sess := &fakeSchemaSession{
		namespaces: []engine.NamespaceInfo{{Name: "users"}, {Name: "orders"}, {Name: "products"}},
		schemaErr: map[string]error{
			"orders":   errors.New("connection reset by peer"),
			"products": errors.New("connection reset by peer"),
		},
		pingErr: errors.New("connection reset by peer"),
	}
	app := newSchemaDiffTestApp(sess)

	schemas, err := app.collectSchemas("conn", "db")
	if err == nil {
		t.Fatalf("expected collectSchemas to report an error when the connection dropped, got schemas: %+v", schemas)
	}
	if schemas != nil {
		t.Fatalf("expected no partial schema result on a connection drop, got: %+v", schemas)
	}
}
