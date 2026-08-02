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
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/engine/sqlbase"
	"github.com/IshanKulkarni02/dbhelm/internal/engine/tunnel"
)

func init() {
	engine.Register(Engine{})
}

const connectTimeout = 10 * time.Second

type Engine struct{}

func (Engine) ID() string { return "mysql" }

func (Engine) Capabilities() engine.Caps {
	return engine.Caps{SQL: true, ForeignKeys: true, Snapshots: true}
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
		netName = "dbhelm-ssh-" + uuid.NewString()
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
		//
		// A fresh timeout, not the ping-bounded pingCtx: on a slow
		// connection, PingContext may have already consumed most of
		// connectTimeout, leaving this statement too little budget and
		// causing a spurious "context deadline exceeded" even though the
		// connection itself is healthy.
		setCtx, setCancel := context.WithTimeout(ctx, connectTimeout)
		_, err := db.ExecContext(setCtx, "SET @"+cfg.TenantSessionVar+" = ?", cfg.TenantValue)
		setCancel()
		if err != nil {
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
	out := []string{}
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
	out := []engine.NamespaceInfo{}
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
		SELECT c.column_name, c.data_type, c.is_nullable = 'YES', pk.ordinal_position
		FROM information_schema.columns c
		LEFT JOIN information_schema.key_column_usage pk
		  ON pk.table_schema = c.table_schema AND pk.table_name = c.table_name
		  AND pk.column_name = c.column_name AND pk.constraint_name = 'PRIMARY'
		WHERE c.table_schema = ? AND c.table_name = ?
		ORDER BY c.ordinal_position`, database, table)
	if err != nil {
		return engine.TableSchema{}, err
	}
	type pkCol struct {
		name    string
		ordinal int64
	}
	var pkCols []pkCol
	for colRows.Next() {
		var c engine.Column
		var pkOrdinal sql.NullInt64
		if err := colRows.Scan(&c.Name, &c.DataType, &c.Nullable, &pkOrdinal); err != nil {
			colRows.Close()
			return engine.TableSchema{}, err
		}
		if pkOrdinal.Valid {
			c.IsPK = true
			pkCols = append(pkCols, pkCol{name: c.Name, ordinal: pkOrdinal.Int64})
		}
		out.Columns = append(out.Columns, c)
	}
	if err := colRows.Err(); err != nil {
		colRows.Close()
		return engine.TableSchema{}, err
	}
	colRows.Close()
	sort.Slice(pkCols, func(i, j int) bool { return pkCols[i].ordinal < pkCols[j].ordinal })
	for _, pc := range pkCols {
		out.PrimaryKey = append(out.PrimaryKey, pc.name)
	}

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

// withDatabase runs fn against a single connection pinned out of the pool,
// having first issued USE <database> on it. Unlike Postgres (fixed DSN
// database, no cross-database queries) a MySQL connection can query any
// database on the server, so Query/Execute/Explain need to select
// whichever one the caller actually asked for — but *sql.DB is a pool, and
// USE only affects the specific connection it runs on, so issuing it via
// db.ExecContext and then running the real query via another db.*Context
// call gives no guarantee both land on the same underlying connection.
// Pinning one *sql.Conn for both closes that gap.
func (s *Session) withDatabase(ctx context.Context, database string, fn func(sqlbase.QueryExecer) error) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "USE "+quoteIdent(database)); err != nil {
		return fmt.Errorf("selecting database %s: %w", quoteIdent(database), err)
	}
	return fn(conn)
}

func (s *Session) Query(ctx context.Context, database, sqlText string) (engine.SQLResult, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	var result engine.SQLResult
	err := s.withDatabase(ctx, database, func(q sqlbase.QueryExecer) error {
		var err error
		result, err = sqlbase.RunQuery(ctx, q, sqlText)
		return err
	})
	return result, err
}

func (s *Session) Execute(ctx context.Context, database, sqlText string) (int64, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	var n int64
	err := s.withDatabase(ctx, database, func(q sqlbase.QueryExecer) error {
		var err error
		n, err = sqlbase.RunExec(ctx, q, sqlText)
		return err
	})
	return n, err
}

func (s *Session) Explain(ctx context.Context, database, sqlText string) (string, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	var out string
	err := s.withDatabase(ctx, database, func(q sqlbase.QueryExecer) error {
		var err error
		out, err = sqlbase.FormatExplainRows(ctx, q, "EXPLAIN "+sqlText)
		return err
	})
	return out, err
}

// ListTableIndexes returns the table's non-PRIMARY indexes, reconstructed
// as literal CREATE INDEX DDL — unlike Postgres/SQLite, MySQL has no
// single built-in DDL-text column for an index, so this builds the
// statement manually from information_schema.statistics. Unlike Postgres,
// MySQL has no separate "unique constraint" catalog object distinct from a
// unique index, so a unique index backing a table-level UNIQUE constraint
// can't be reliably excluded here the way Postgres' constraint-backed
// indexes are — restore treats "index already exists" as non-fatal
// (see restore_sql.go) as the safety net for that ambiguity instead.
func (s *Session) ListTableIndexes(ctx context.Context, database, table string) ([]engine.IndexDef, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	// One row per index column, ordered so each index's columns arrive
	// consecutively and in declared order — grouped in Go below rather
	// than via GROUP_CONCAT/strings.Split, which broke on a column name
	// containing a literal comma (legal in a backtick-quoted MySQL
	// identifier): concatenating column names with a "," separator and
	// splitting on "," can't tell a real separator from a comma inside a
	// name, silently turning one indexed column into two fake ones.
	rows, err := s.db.QueryContext(ctx, `
		SELECT index_name, non_unique = 0 AS is_unique, column_name
		FROM information_schema.statistics
		WHERE table_schema = ? AND table_name = ? AND index_name != 'PRIMARY'
		ORDER BY index_name, seq_in_index`, database, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type index struct {
		unique bool
		cols   []string
	}
	var order []string
	byName := map[string]*index{}
	for rows.Next() {
		var name, col string
		var unique bool
		if err := rows.Scan(&name, &unique, &col); err != nil {
			return nil, err
		}
		ix, ok := byName[name]
		if !ok {
			ix = &index{unique: unique}
			byName[name] = ix
			order = append(order, name)
		}
		ix.cols = append(ix.cols, col)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := []engine.IndexDef{}
	for _, name := range order {
		ix := byName[name]
		colList := make([]string, len(ix.cols))
		for i, c := range ix.cols {
			colList[i] = quoteIdent(c)
		}
		kind := "INDEX"
		if ix.unique {
			kind = "UNIQUE INDEX"
		}
		ddl := fmt.Sprintf("CREATE %s %s ON %s (%s)", kind, quoteIdent(name), quoteIdent(table), strings.Join(colList, ", "))
		out = append(out, engine.IndexDef{Name: name, DDL: ddl})
	}
	return out, nil
}

// BeginConsistentRead opens a REPEATABLE READ, read-only transaction —
// InnoDB's MVCC gives this a consistent snapshot as of the transaction's
// start, so every table streamed through it sees one point-in-time view
// of the database.
func (s *Session) BeginConsistentRead(ctx context.Context) (engine.ConsistentReadTx, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return &readTx{tx: tx}, nil
}

type readTx struct {
	tx *sql.Tx
}

func (r *readTx) StreamRows(ctx context.Context, database, table string, onRow func(row map[string]any) error) error {
	sqlText := fmt.Sprintf("SELECT * FROM %s.%s", quoteIdent(database), quoteIdent(table))
	return sqlbase.StreamRows(ctx, r.tx, sqlText, onRow)
}

// Close rolls back rather than commits — read-only, nothing to persist.
func (r *readTx) Close(ctx context.Context) error {
	return r.tx.Rollback()
}

func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
