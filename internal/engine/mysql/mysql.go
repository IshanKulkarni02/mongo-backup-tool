// Package mysql implements engine.Engine/Session/SQLSession for MySQL.
// Unlike Postgres, a single MySQL connection can query across databases,
// so the "database" parameter to ListNamespaces/TableSchema/Query means an
// actual MySQL database/schema, and ListDatabases enumerates real
// databases on the server (not just informational).
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine/sqlbase"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine/tunnel"
)

func init() {
	engine.Register(Engine{})
}

const connectTimeout = 10 * time.Second

type Engine struct{}

func (Engine) ID() string { return "mysql" }

func (Engine) Capabilities() engine.Caps {
	return engine.Caps{SQL: true, ForeignKeys: true}
}

func (Engine) Open(ctx context.Context, cfg engine.ConnConfig) (engine.Session, error) {
	dsnCfg, err := mysql.ParseDSN(cfg.URI)
	if err != nil {
		return nil, fmt.Errorf("parsing mysql connection string: %w", err)
	}

	var tun *tunnel.Tunnel
	var netName string
	if cfg.SSHTunnel != nil {
		t, err := tunnel.Open(ctx, *cfg.SSHTunnel)
		if err != nil {
			return nil, err
		}
		tun = t
		// The dial-context registry is process-global and keyed by network
		// name, so each tunneled connection needs a unique name to avoid
		// colliding with (or being torn down by) another profile's tunnel.
		netName = "mongobak-ssh-" + uuid.NewString()
		mysql.RegisterDialContext(netName, func(ctx context.Context, addr string) (net.Conn, error) {
			return tun.DialContext(ctx, "tcp", addr)
		})
		dsnCfg.Net = netName
	}

	db, err := sql.Open("mysql", dsnCfg.FormatDSN())
	if err != nil {
		if tun != nil {
			tun.Close()
			mysql.DeregisterDialContext(netName)
		}
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		if tun != nil {
			tun.Close()
			mysql.DeregisterDialContext(netName)
		}
		return nil, err
	}

	if cfg.TenantSessionVar != "" {
		// MySQL user-defined session variables (`@name`) don't allow dots,
		// unlike Postgres' namespaced GUCs (engine.ValidSessionVarName
		// allows "app.current_tenant" for that reason) — reject it here
		// with a clear error instead of sending a statement MySQL itself
		// would reject less legibly.
		if !engine.ValidSessionVarName(cfg.TenantSessionVar) || strings.Contains(cfg.TenantSessionVar, ".") {
			db.Close()
			if tun != nil {
				tun.Close()
				mysql.DeregisterDialContext(netName)
			}
			return nil, fmt.Errorf("invalid tenant session variable name %q (MySQL session variables can't contain '.')", cfg.TenantSessionVar)
		}
		// The variable name still can't be parameterized (SQL doesn't
		// allow parameterized identifiers), but it's now validated above;
		// the value is always sent as a query argument.
		if _, err := db.ExecContext(pingCtx, "SET @"+cfg.TenantSessionVar+" = ?", cfg.TenantValue); err != nil {
			db.Close()
			if tun != nil {
				tun.Close()
				mysql.DeregisterDialContext(netName)
			}
			return nil, fmt.Errorf("setting tenant session variable: %w", err)
		}
	}

	return &Session{db: db, tunnel: tun, tunnelNet: netName}, nil
}

type Session struct {
	db        *sql.DB
	tunnel    *tunnel.Tunnel
	tunnelNet string
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
		mysql.DeregisterDialContext(s.tunnelNet)
	}
	return err
}

var systemDatabases = map[string]bool{
	"mysql": true, "information_schema": true, "performance_schema": true, "sys": true,
}

func (s *Session) ListDatabases(ctx context.Context) ([]string, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, "SHOW DATABASES")
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
		if !systemDatabases[name] {
			out = append(out, name)
		}
	}
	return out, rows.Err()
}

func (s *Session) ListNamespaces(ctx context.Context, database string) ([]engine.NamespaceInfo, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
		SELECT table_name, IFNULL(table_rows, 0), IFNULL(data_length + index_length, 0)
		FROM information_schema.tables
		WHERE table_schema = ?
		ORDER BY table_name`, database)
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
	out := engine.TableSchema{Name: table}

	colRows, err := s.db.QueryContext(ctx, `
		SELECT column_name, data_type, is_nullable = 'YES', column_key = 'PRI'
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position`, database, table)
	if err != nil {
		return engine.TableSchema{}, err
	}
	for colRows.Next() {
		var c engine.Column
		if err := colRows.Scan(&c.Name, &c.DataType, &c.Nullable, &c.IsPK); err != nil {
			colRows.Close()
			return engine.TableSchema{}, err
		}
		out.Columns = append(out.Columns, c)
	}
	if err := colRows.Err(); err != nil {
		colRows.Close()
		return engine.TableSchema{}, err
	}
	colRows.Close()

	fkRows, err := s.db.QueryContext(ctx, `
		SELECT column_name, referenced_table_name, referenced_column_name
		FROM information_schema.key_column_usage
		WHERE table_schema = ? AND table_name = ? AND referenced_table_name IS NOT NULL`,
		database, table)
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
