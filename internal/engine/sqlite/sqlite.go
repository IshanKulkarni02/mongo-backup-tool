// Package sqlite implements engine.Engine/Session/SQLSession for SQLite,
// using modernc.org/sqlite (pure Go — a cgo driver like mattn/go-sqlite3
// would break cross-platform `wails build`). A SQLite "connection" is a
// single file, so there's exactly one database and it isn't reachable
// through an SSH tunnel — ConnConfig.SSHTunnel is ignored here.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine/sqlbase"
)

func init() {
	engine.Register(Engine{})
}

const connectTimeout = 10 * time.Second

type Engine struct{}

func (Engine) ID() string { return "sqlite" }

func (Engine) Capabilities() engine.Caps {
	return engine.Caps{SQL: true, ForeignKeys: true}
}

// Open treats cfg.URI as a file path (or ":memory:"/"file::memory:?..."
// for an in-memory database).
func (Engine) Open(ctx context.Context, cfg engine.ConnConfig) (engine.Session, error) {
	db, err := sql.Open("sqlite", cfg.URI)
	if err != nil {
		return nil, err
	}
	// database/sql pools connections, but SQLite serializes writers and (for
	// a plain ":memory:" DSN, without shared-cache) each new connection is
	// a distinct, empty in-memory database. A single connection avoids
	// both problems; SQLite's own file-level locking makes this the
	// standard recommendation for this driver.
	db.SetMaxOpenConns(1)

	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, err
	}
	// SQLite's own client library defaults foreign_keys off for backward
	// compatibility; turn it on so declared FKs are actually enforced and
	// (more importantly for this tool) so PRAGMA foreign_key_list stays
	// meaningful to the caller relying on it.
	db.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	return &Session{db: db, path: cfg.URI}, nil
}

type Session struct {
	db   *sql.DB
	path string
}

func opCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 30*time.Second)
}

func (s *Session) Ping(ctx context.Context) error {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	return s.db.PingContext(ctx)
}

func (s *Session) Close(ctx context.Context) error {
	return s.db.Close()
}

// ListDatabases returns the single logical database this file represents.
// SQLite has no server-level namespace of multiple databases to enumerate.
func (s *Session) ListDatabases(ctx context.Context) ([]string, error) {
	return []string{"main"}, nil
}

func (s *Session) ListNamespaces(ctx context.Context, database string) ([]engine.NamespaceInfo, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	// The connection pool is capped at 1 (see Open), so the count queries
	// below must run only after this cursor is fully released.
	rows.Close()

	out := make([]engine.NamespaceInfo, 0, len(names))
	for _, name := range names {
		info := engine.NamespaceInfo{Name: name}
		var count int64
		// Best-effort row count; a malformed/locked table shouldn't fail
		// the whole listing, just show 0.
		if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, escapeIdent(name))).Scan(&count); err == nil {
			info.DocCount = count
		}
		out = append(out, info)
	}
	return out, nil
}

// escapeIdent doubles embedded double-quotes so a table name can be safely
// interpolated inside a double-quoted SQLite identifier. Table names come
// from sqlite_master (the database's own catalog), not user input, but
// this keeps the interpolation correct if a table name itself contains a
// quote character.
func escapeIdent(name string) string {
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		if name[i] == '"' {
			out = append(out, '"', '"')
		} else {
			out = append(out, name[i])
		}
	}
	return string(out)
}

func (s *Session) TableSchema(ctx context.Context, database, table string) (engine.TableSchema, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	out := engine.TableSchema{Name: table}

	colRows, err := s.db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info("%s")`, escapeIdent(table)))
	if err != nil {
		return engine.TableSchema{}, err
	}
	for colRows.Next() {
		// cid, name, type, notnull, dflt_value, pk
		var cid int
		var name, ctype string
		var notNull, pk int
		var dflt sql.NullString
		if err := colRows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			colRows.Close()
			return engine.TableSchema{}, err
		}
		out.Columns = append(out.Columns, engine.Column{
			Name: name, DataType: ctype, Nullable: notNull == 0, IsPK: pk > 0,
		})
	}
	if err := colRows.Err(); err != nil {
		colRows.Close()
		return engine.TableSchema{}, err
	}
	colRows.Close()

	fkRows, err := s.db.QueryContext(ctx, fmt.Sprintf(`PRAGMA foreign_key_list("%s")`, escapeIdent(table)))
	if err != nil {
		return engine.TableSchema{}, err
	}
	defer fkRows.Close()
	for fkRows.Next() {
		// id, seq, table, from, to, on_update, on_delete, match
		var id, seq int
		var refTable, from, to, onUpdate, onDelete, match string
		if err := fkRows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return engine.TableSchema{}, err
		}
		out.ForeignKeys = append(out.ForeignKeys, engine.ForeignKey{
			Column: from, RefTable: refTable, RefColumn: to,
		})
	}
	return out, fkRows.Err()
}

func (s *Session) Query(ctx context.Context, database, sqlText string) (engine.SQLResult, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	return sqlbase.RunQuery(ctx, s.db, sqlText)
}

func (s *Session) Execute(ctx context.Context, database, sqlText string) (int64, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	return sqlbase.RunExec(ctx, s.db, sqlText)
}

func (s *Session) Explain(ctx context.Context, database, sqlText string) (string, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	return sqlbase.FormatExplainRows(ctx, s.db, "EXPLAIN QUERY PLAN "+sqlText)
}
