// Package engine defines the database-engine abstraction that every
// interface (CLI, TUI, desktop) talks through. An Engine is a factory for
// one database technology (MongoDB today; PostgreSQL/MySQL/SQLite planned);
// a Session is a live, reusable connection to one saved profile. Sessions
// are cached and shared via Manager rather than opened per call.
package engine

import (
	"context"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine/tunnel"
)

// Caps describes what a database engine supports, so UI surfaces can be
// shown or hidden per connection instead of hardcoding engine checks.
type Caps struct {
	// SQL: the engine speaks SQL (query editor, EXPLAIN, DDL surfaces).
	SQL bool `json:"sql"`
	// Documents: the engine stores schemaless documents (JSON tree browser,
	// document CRUD, index management by key-spec).
	Documents bool `json:"documents"`
	// Aggregation: MongoDB-style aggregation pipelines.
	Aggregation bool `json:"aggregation"`
	// ForeignKeys: declared FK constraints exist for relational navigation.
	ForeignKeys bool `json:"foreignKeys"`
	// Snapshots: mongobak's snapshot/backup engines can operate on it.
	Snapshots bool `json:"snapshots"`
}

// ConnConfig is the engine-facing view of a saved connection profile.
type ConnConfig struct {
	Name     string
	URI      string
	ReadOnly bool
	// SSHTunnel, when set, routes the database connection through an SSH
	// bastion instead of dialing it directly.
	SSHTunnel *tunnel.Config
	// TenantSessionVar and TenantValue configure multi-tenant mode: when
	// TenantSessionVar is non-empty, a SQL engine's Open sets it to
	// TenantValue right after connecting (e.g. Postgres:
	// `SET app.current_tenant = 'acme'`), so every query in the session
	// runs under that tenant's row-level-security context.
	TenantSessionVar string
	TenantValue      string
}

// Engine is a factory for sessions against one database technology.
type Engine interface {
	// ID is the stable identifier persisted in connection profiles,
	// e.g. "mongodb", "postgres", "mysql", "sqlite".
	ID() string
	Capabilities() Caps
	// Open establishes a live session. Implementations should fail fast
	// (ping) so a dead connection is never cached.
	Open(ctx context.Context, cfg ConnConfig) (Session, error)
}

// Session is a live connection to one profile. Implementations must be
// safe for concurrent use; Manager hands the same Session to overlapping
// callers.
type Session interface {
	Ping(ctx context.Context) error
	// ListDatabases returns user databases (system/internal ones filtered),
	// sorted by name.
	ListDatabases(ctx context.Context) ([]string, error)
	// ListNamespaces returns the collections/tables of one database with
	// size summaries.
	ListNamespaces(ctx context.Context, database string) ([]NamespaceInfo, error)
	Close(ctx context.Context) error
}

// DocumentSession is the document-store surface (MongoDB; later any engine
// whose Caps.Documents is true). Documents cross this boundary as Extended
// JSON strings — the same representation the desktop browser already uses.
type DocumentSession interface {
	Session
	QueryDocuments(ctx context.Context, q DocQuery) (DocPage, error)
	InsertDocument(ctx context.Context, database, namespace, docJSON string) error
	// UpdateDocument replaces a document matched by its original _id
	// (captured before editing, so an edit can't retarget itself by
	// changing _id).
	UpdateDocument(ctx context.Context, database, namespace, originalIDJSON, docJSON string) error
	DeleteDocument(ctx context.Context, database, namespace, idJSON string) error
	CreateNamespace(ctx context.Context, database, namespace string) error
	DropNamespace(ctx context.Context, database, namespace string) error
	ListIndexes(ctx context.Context, database, namespace string) ([]IndexInfo, error)
	CreateIndex(ctx context.Context, database, namespace, keysJSON string, unique bool) error
	DropIndex(ctx context.Context, database, namespace, name string) error
}

// AggregateSession is the aggregation-pipeline surface (MongoDB today).
type AggregateSession interface {
	Session
	// Aggregate runs a pipeline (a JSON array of stage documents, Extended
	// JSON text) against one collection and returns each result document
	// as relaxed Extended JSON, same representation as DocPage.
	Aggregate(ctx context.Context, database, namespace, pipelineJSON string) ([]string, error)
}

// SQLSession is the relational surface (PostgreSQL, MySQL, SQLite).
type SQLSession interface {
	Session
	// TableSchema introspects one table: columns, primary key, and
	// declared foreign keys.
	TableSchema(ctx context.Context, database, table string) (TableSchema, error)
	// Query runs an arbitrary read query and returns a typed result page.
	// Total is -1 when the row count of an arbitrary statement isn't known
	// without a second pass.
	Query(ctx context.Context, database, sqlText string) (SQLResult, error)
	// Execute runs a data-modifying statement and returns rows affected.
	Execute(ctx context.Context, database, sqlText string) (int64, error)
	// Explain returns the database's own query-plan text for sqlText.
	Explain(ctx context.Context, database, sqlText string) (string, error)
}
