// Package postgres implements engine.Engine/Session/SQLSession for
// PostgreSQL via pgx. A connection's DSN fixes the actual database at
// connect time (Postgres has no cross-database queries), so the
// "database" parameter that ListNamespaces/TableSchema/Query take
// elsewhere in the engine package means schema here (e.g. "public") —
// this is what Phase 7's schema-per-tenant switcher reconnects against.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine/sqlbase"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine/tunnel"
)

func init() {
	engine.Register(Engine{})
}

const connectTimeout = 10 * time.Second

type Engine struct{}

func (Engine) ID() string { return "postgres" }

func (Engine) Capabilities() engine.Caps {
	return engine.Caps{SQL: true, ForeignKeys: true}
}

func (Engine) Open(ctx context.Context, cfg engine.ConnConfig) (engine.Session, error) {
	pgCfg, err := pgx.ParseConfig(cfg.URI)
	if err != nil {
		return nil, fmt.Errorf("parsing postgres connection string: %w", err)
	}

	var tun *tunnel.Tunnel
	if cfg.SSHTunnel != nil {
		t, err := tunnel.Open(ctx, *cfg.SSHTunnel)
		if err != nil {
			return nil, err
		}
		tun = t
		pgCfg.DialFunc = tun.DialContext
	}

	db := stdlib.OpenDB(*pgCfg)
	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		if tun != nil {
			tun.Close()
		}
		return nil, err
	}

	if cfg.TenantSessionVar != "" {
		if !engine.ValidSessionVarName(cfg.TenantSessionVar) {
			db.Close()
			if tun != nil {
				tun.Close()
			}
			return nil, fmt.Errorf("invalid tenant session variable name %q", cfg.TenantSessionVar)
		}
		// set_config's name argument still can't be parameterized as a true
		// SQL identifier, but it's a regular string argument here (not
		// spliced into the statement), and the value is fully
		// parameterized — safer than a "SET x = y" string built with
		// fmt.Sprintf.
		if _, err := db.ExecContext(pingCtx, "SELECT set_config($1, $2, false)", cfg.TenantSessionVar, cfg.TenantValue); err != nil {
			db.Close()
			if tun != nil {
				tun.Close()
			}
			return nil, fmt.Errorf("setting tenant session variable: %w", err)
		}
	}

	return &Session{db: db, tunnel: tun, defaultSchema: pgCfg.Config.RuntimeParams["search_path"]}, nil
}

type Session struct {
	db            *sql.DB
	tunnel        *tunnel.Tunnel
	defaultSchema string
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
	err := s.db.Close()
	if s.tunnel != nil {
		s.tunnel.Close()
	}
	return err
}

// ListDatabases returns every non-template database on the server. This is
// informational only — Query/TableSchema/ListNamespaces operate on the
// schema within the database the DSN already connected to, since Postgres
// has no cross-database queries.
func (s *Session) ListDatabases(ctx context.Context) ([]string, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func schemaOrPublic(schema string) string {
	if schema == "" {
		return "public"
	}
	return schema
}

// ListNamespaces returns every table in the given schema with its live row
// estimate and total on-disk size.
func (s *Session) ListNamespaces(ctx context.Context, database string) ([]engine.NamespaceInfo, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.relname, COALESCE(st.n_live_tup, 0), COALESCE(pg_total_relation_size(c.oid), 0)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_stat_user_tables st ON st.relname = c.relname AND st.schemaname = n.nspname
		WHERE n.nspname = $1 AND c.relkind = 'r'
		ORDER BY c.relname`, schemaOrPublic(database))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []engine.NamespaceInfo
	for rows.Next() {
		var info engine.NamespaceInfo
		if err := rows.Scan(&info.Name, &info.DocCount, &info.StorageSize); err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, rows.Err()
}

func (s *Session) TableSchema(ctx context.Context, database, table string) (engine.TableSchema, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	schema := schemaOrPublic(database)
	out := engine.TableSchema{Name: table}

	colRows, err := s.db.QueryContext(ctx, `
		SELECT c.column_name, c.data_type, c.udt_name, c.is_nullable = 'YES',
		  EXISTS (
		    SELECT 1 FROM information_schema.table_constraints tc
		    JOIN information_schema.key_column_usage kcu
		      ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		    WHERE tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = $1
		      AND tc.table_name = $2 AND kcu.column_name = c.column_name
		  )
		FROM information_schema.columns c
		WHERE c.table_schema = $1 AND c.table_name = $2
		ORDER BY c.ordinal_position`, schema, table)
	if err != nil {
		return engine.TableSchema{}, err
	}
	for colRows.Next() {
		var c engine.Column
		var udtName string
		if err := colRows.Scan(&c.Name, &c.DataType, &udtName, &c.Nullable, &c.IsPK); err != nil {
			colRows.Close()
			return engine.TableSchema{}, err
		}
		// "USER-DEFINED" (information_schema's catch-all for anything
		// without a standard SQL type, including PostGIS' geometry/
		// geography) isn't useful on its own — the underlying type name
		// is, e.g. so the frontend can offer ST_AsGeoJSON for it.
		if c.DataType == "USER-DEFINED" {
			c.DataType = udtName
		}
		out.Columns = append(out.Columns, c)
	}
	if err := colRows.Err(); err != nil {
		colRows.Close()
		return engine.TableSchema{}, err
	}
	colRows.Close()

	fkRows, err := s.db.QueryContext(ctx, `
		SELECT kcu.column_name, ccu.table_name, ccu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		JOIN information_schema.constraint_column_usage ccu
		  ON tc.constraint_name = ccu.constraint_name AND tc.table_schema = ccu.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_schema = $1 AND tc.table_name = $2`,
		schema, table)
	if err != nil {
		return engine.TableSchema{}, err
	}
	defer fkRows.Close()
	for fkRows.Next() {
		var fk engine.ForeignKey
		if err := fkRows.Scan(&fk.Column, &fk.RefTable, &fk.RefColumn); err != nil {
			return engine.TableSchema{}, err
		}
		out.ForeignKeys = append(out.ForeignKeys, fk)
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
	return sqlbase.FormatExplainRows(ctx, s.db, "EXPLAIN "+sqlText)
}
