package snapshot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

// SQLRestoreOptions configures restoring a SQL snapshot into a live
// database. Session is an already-resolved engine.SQLSession, mirroring
// SQLCreateOptions' shape. EngineID ("postgres"/"mysql"/"sqlite") is
// needed here (unlike CreateSQL, which never builds raw SQL text itself —
// engine.ConsistentReadTx.StreamRows already knows its own dialect's
// quoting) because restore builds INSERT/DELETE/index DDL text directly,
// the same way desktop/import.go's ImportCSV does, since
// engine.SQLSession.Execute takes a complete SQL string with no
// parameterized-query support to lean on instead.
type SQLRestoreOptions struct {
	SourceConnection string
	SourceDatabase   string
	SnapshotID       string

	Session        engine.SQLSession
	EngineID       string
	TargetDatabase string // defaults to SourceDatabase if empty
	Table          string // restore only this table; empty = every table in the snapshot
	// Drop clears each target table's existing rows before inserting
	// (DELETE FROM, not DROP TABLE — restore assumes the target table's
	// structure already exists; see the package-level scoping note).
	Drop bool
}

// RestoreSQL applies a stored SQL snapshot to a live database via batched
// multi-row INSERT, then replays the snapshot's captured index DDL, one
// table at a time — the SQL analog of Restore. Unlike Mongo collections
// (which spring into existence on first insert), a SQL restore requires
// the target table to already exist with a compatible structure: SQL
// snapshots version data, not table DDL (see internal/schemadiff for
// structural migrations) — restore errors clearly if a target table is
// missing rather than trying to create one.
func RestoreSQL(ctx context.Context, opts SQLRestoreOptions) (*RestoreResult, error) {
	m, err := Get(opts.SourceConnection, opts.SourceDatabase, opts.SnapshotID)
	if err != nil {
		return nil, err
	}

	scope, err := scopeDir(opts.SourceConnection, opts.SourceDatabase)
	if err != nil {
		return nil, err
	}
	backend, err := OpenBackend(scope, "")
	if err != nil {
		return nil, err
	}
	defer backend.Close()

	targetDB := opts.TargetDatabase
	if targetDB == "" {
		targetDB = opts.SourceDatabase
	}

	var names []string
	for name := range m.Collections {
		if opts.Table != "" && name != opts.Table {
			continue
		}
		names = append(names, name)
	}
	if opts.Table != "" && len(names) == 0 {
		return nil, fmt.Errorf("snapshot %s has no table %q", m.ID, opts.Table)
	}
	sort.Strings(names)

	result := &RestoreResult{Database: targetDB}
	for _, name := range names {
		cm := m.Collections[name]

		schema, err := opts.Session.TableSchema(ctx, targetDB, name)
		if err != nil || len(schema.Columns) == 0 {
			return result, fmt.Errorf("target table %s doesn't exist or couldn't be introspected — SQL snapshot restore requires the table structure to already exist: %w", name, err)
		}
		binaryCols := make(map[string]bool, len(schema.Columns))
		for _, c := range schema.Columns {
			if isBinaryDataType(c.DataType) {
				binaryCols[c.Name] = true
			}
		}

		if err := setForeignKeyChecks(ctx, opts.Session, opts.EngineID, targetDB, false); err != nil {
			return result, fmt.Errorf("disabling FK checks before restoring %s: %w", name, err)
		}
		restoreErr := func() error {
			if opts.Drop {
				// Recorded BEFORE the clear runs, not after — same
				// reasoning as Restore: the clear itself is the
				// destructive act.
				result.Touched = append(result.Touched, name)
				ident := quoteIdentSQL(opts.EngineID, name)
				if _, err := opts.Session.Execute(ctx, targetDB, "DELETE FROM "+ident); err != nil {
					return fmt.Errorf("clearing %s before restore: %w", name, err)
				}
			}

			it, err := backend.IterDocRefs(m.ID, name)
			if err != nil {
				return fmt.Errorf("reading doc refs for %s: %w", name, err)
			}
			written, err := insertSQLRows(ctx, opts.Session, opts.EngineID, targetDB, name, backend, it, binaryCols)
			result.DocsWritten += written
			if err != nil {
				return fmt.Errorf("restoring %s: %w", name, err)
			}

			if err := recreateSQLIndexes(ctx, opts.Session, targetDB, cm.IndexDDL); err != nil {
				return fmt.Errorf("recreating indexes for %s: %w", name, err)
			}
			return nil
		}()
		// Always attempt to re-enable FK checks, even if the restore body
		// above failed partway through — leaving a target database with
		// constraint checking permanently disabled would be a worse
		// outcome than the original error.
		if err := setForeignKeyChecks(ctx, opts.Session, opts.EngineID, targetDB, true); err != nil && restoreErr == nil {
			restoreErr = fmt.Errorf("re-enabling FK checks after restoring %s: %w", name, err)
		}
		if restoreErr != nil {
			return result, restoreErr
		}

		result.Collections = append(result.Collections, name)
	}

	return result, nil
}

// RestoreSQLWithSafety is RestoreWithSafety's SQL-engine counterpart, kept
// as a separate function rather than a dispatching shared implementation:
// RestoreOptions (URI-based) and SQLRestoreOptions (session-based) don't
// unify into one options type without a larger refactor of the
// already-working Mongo path, and this mirrors how Create/CreateSQL and
// Restore/RestoreSQL are themselves kept parallel rather than merged. See
// RestoreWithSafety's doc comment for the safety-snapshot/auto-rollback
// behavior this replicates. safetyConnectionName is the connection the
// safety snapshot is scoped under — the restore *target*'s own connection
// name, which may differ from SourceConnection (restoring a snapshot from
// one connection into a different one is supported), exactly as
// RestoreWithSafety's safetyConnectionName parameter already works.
func RestoreSQLWithSafety(ctx context.Context, opts SQLRestoreOptions, safetyConnectionName string) (result *RestoreResult, safety *CreateResult, rolledBack bool, err error) {
	targetDB := opts.TargetDatabase
	if targetDB == "" {
		targetDB = opts.SourceDatabase
	}

	if opts.Drop {
		safety, err = CreateSQL(ctx, SQLCreateOptions{
			Connection: safetyConnectionName,
			Database:   targetDB,
			Message:    fmt.Sprintf("auto safety snapshot before restoring %s", opts.SnapshotID),
			Session:    opts.Session,
		})
		if err != nil {
			return nil, nil, false, fmt.Errorf("taking safety snapshot before restore: %w", err)
		}
	}

	result, err = RestoreSQL(ctx, opts)
	if err == nil {
		return result, safety, false, nil
	}

	partialDamage := opts.Drop && safety != nil && result != nil && len(result.Touched) > 0
	if !partialDamage {
		return result, safety, false, err
	}

	_, rbErr := RestoreSQL(ctx, SQLRestoreOptions{
		SourceConnection: safetyConnectionName,
		SourceDatabase:   targetDB,
		SnapshotID:       safety.Summary.ID,
		Session:          opts.Session,
		EngineID:         opts.EngineID,
		TargetDatabase:   targetDB,
		Drop:             true,
	})
	if rbErr != nil {
		return result, safety, false, fmt.Errorf("restore failed partway through (%d table(s) applied: %w) — automatic rollback ALSO failed (%v); manually restore safety snapshot %s into %q to recover", len(result.Collections), err, rbErr, safety.Summary.ID, targetDB)
	}
	return result, safety, true, fmt.Errorf("restore failed partway through (%d table(s) applied) and was automatically rolled back to the pre-restore state using safety snapshot %s: %w", len(result.Collections), safety.Summary.ID, err)
}

// insertSQLRows streams rows out of it (never materializing the whole
// table at once) and batch-inserts them as multi-row INSERT statements,
// returning how many were written.
func insertSQLRows(ctx context.Context, sess engine.SQLSession, engineID, database, table string, store ObjectStore, it docRefIterator, binaryCols map[string]bool) (int, error) {
	defer it.Close()

	ident := quoteIdentSQL(engineID, table)
	const batchSize = 500
	var batch []map[string]any
	var columns []string // derived once from the first row; every row shares the same column set (see below)
	written := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if columns == nil {
			// Every row in a table's snapshot came from the same
			// StreamRows SELECT * scan, so every row map has an
			// identical key set (SQL NULLs are present as a nil value,
			// not an absent key) — safe to derive column order once.
			for k := range batch[0] {
				columns = append(columns, k)
			}
			sort.Strings(columns)
		}
		colIdents := make([]string, len(columns))
		for i, c := range columns {
			colIdents[i] = quoteIdentSQL(engineID, c)
		}
		valueGroups := make([]string, len(batch))
		for i, row := range batch {
			vals := make([]string, len(columns))
			for j, col := range columns {
				vals[j] = sqlLiteralForRestore(engineID, row[col], binaryCols[col])
			}
			valueGroups[i] = "(" + strings.Join(vals, ", ") + ")"
		}
		sqlText := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", ident, strings.Join(colIdents, ", "), strings.Join(valueGroups, ", "))
		if _, err := sess.Execute(ctx, database, sqlText); err != nil {
			return err
		}
		written += len(batch)
		batch = batch[:0]
		return nil
	}

	for {
		ref, ok, err := it.Next()
		if err != nil {
			return written, err
		}
		if !ok {
			break
		}
		data, err := store.Get(ref.Hash)
		if err != nil {
			return written, err
		}
		row, err := decodeSQLRow(data)
		if err != nil {
			return written, err
		}
		batch = append(batch, row)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return written, err
			}
		}
	}
	if err := flush(); err != nil {
		return written, err
	}
	return written, nil
}

// decodeSQLRow parses a stored row's canonical JSON back into a
// map[string]any, using json.Number for numeric fields instead of the
// default float64 — encoding/json's default number decoding loses
// precision above 2^53, which a plain float64 round-trip would silently
// corrupt for a large BIGINT/BIGSERIAL primary key or similar.
func decodeSQLRow(data []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var row map[string]any
	if err := dec.Decode(&row); err != nil {
		return nil, err
	}
	return row, nil
}

// sqlLiteralForRestore renders a decoded row value as a SQL literal.
// Binary columns store the original []byte as a base64 string (see
// canonicalRowBytes/StreamRows — JSON has no binary type), so isBinary
// triggers a decode back to raw bytes rendered as a hex literal instead of
// the base64 text itself.
func sqlLiteralForRestore(engineID string, value any, isBinary bool) string {
	if value == nil {
		return "NULL"
	}
	if isBinary {
		s, ok := value.(string)
		if !ok {
			return "NULL"
		}
		raw, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return "NULL"
		}
		return hexBinaryLiteral(engineID, hex.EncodeToString(raw))
	}
	switch v := value.(type) {
	case json.Number:
		return v.String()
	case bool:
		if v {
			return "TRUE"
		}
		return "FALSE"
	case string:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	default:
		// Shouldn't happen for a scalar SQL column value, but fail safe
		// rather than panic on an unexpected shape.
		b, _ := json.Marshal(v)
		return "'" + strings.ReplaceAll(string(b), "'", "''") + "'"
	}
}

// hexBinaryLiteral renders hexStr as a binary literal in engineID's own
// syntax. MySQL and SQLite both accept X'<hex>' directly; Postgres' X'...'
// is a *bit-string* literal (a different type), not bytea — Postgres needs
// the hex *format* string form, '\x<hex>', instead.
func hexBinaryLiteral(engineID, hexStr string) string {
	if engineID == "postgres" {
		return `'\x` + hexStr + `'`
	}
	return "X'" + hexStr + "'"
}

// setForeignKeyChecks disables (or re-enables) FK constraint checking for
// the duration of a restore — chosen over topologically ordering tables by
// FK dependency because it also handles dependency cycles, which
// topological sort can't. engineID == "" (or unrecognized) is a no-op,
// not an error, so this stays safe to call unconditionally.
func setForeignKeyChecks(ctx context.Context, sess engine.SQLSession, engineID, database string, enabled bool) error {
	var sqlText string
	switch engineID {
	case "postgres":
		role := "replica"
		if enabled {
			role = "origin"
		}
		sqlText = fmt.Sprintf("SET session_replication_role = '%s'", role)
	case "mysql":
		val := "0"
		if enabled {
			val = "1"
		}
		sqlText = "SET FOREIGN_KEY_CHECKS=" + val
	case "sqlite":
		val := "OFF"
		if enabled {
			val = "ON"
		}
		sqlText = "PRAGMA foreign_keys = " + val
	default:
		return nil
	}
	_, err := sess.Execute(ctx, database, sqlText)
	if err != nil && engineID == "postgres" {
		return fmt.Errorf("%w (disabling FK checks via session_replication_role requires Postgres superuser — if this connection isn't one, foreign-key-constrained tables may need restoring in dependency order, or constraints temporarily dropped, instead)", err)
	}
	return err
}

// recreateSQLIndexes replays each captured index's literal CREATE INDEX
// DDL. "Already exists" is tolerated rather than treated as fatal: MySQL's
// ListTableIndexes can't always reliably exclude a unique index backing a
// table-level UNIQUE constraint (see its doc comment), so replaying it
// here may legitimately collide with what the target table's own
// structure already created.
func recreateSQLIndexes(ctx context.Context, sess engine.SQLSession, database string, indexDDL []string) error {
	for _, ddl := range indexDDL {
		if _, err := sess.Execute(ctx, database, ddl); err != nil {
			if isAlreadyExistsError(err) {
				continue
			}
			return fmt.Errorf("index DDL %q: %w", ddl, err)
		}
	}
	return nil
}

func isAlreadyExistsError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "duplicate key name") || strings.Contains(msg, "duplicate")
}

func quoteIdentSQL(engineID, name string) string {
	if engineID == "mysql" {
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func isBinaryDataType(dbType string) bool {
	// information_schema.columns.data_type is reported lowercase for both
	// Postgres ("bytea") and MySQL ("blob"/"binary"/"varbinary"); only the
	// SQLite test schema happens to declare BLOB uppercase, which is why a
	// plain-uppercase comparison here worked in tests but never matched
	// real Postgres/MySQL binary columns.
	switch strings.ToUpper(dbType) {
	case "BYTEA", "BLOB", "BINARY", "VARBINARY":
		return true
	}
	return false
}
