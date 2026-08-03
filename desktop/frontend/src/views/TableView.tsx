import { useCallback, useEffect, useState } from "react";
import { Database, RefreshCw, Table2, X, Sparkles, FileCode, Copy, Download, Upload, ArrowLeft } from "lucide-react";
import {
  ListConnections,
  TestConnection,
  ListTables,
  GetTableSchema,
  ListReferencingTables,
  RunSQLQuery,
  RunSQLExecute,
  GenerateMockData,
  GenerateAPISchema,
  ExportQueryResultsCSV,
  ExportQueryResultsJSON,
  PickCSVFile,
  ReadCSVHeader,
  ImportCSV,
} from "../../wailsjs/go/main/App";
import { main, engine } from "../../wailsjs/go/models";
import { Button } from "../components/Button";
import { Modal } from "../components/Modal";
import { EmptyState } from "../components/EmptyState";
import { Skeleton } from "../components/Skeleton";
import { DataGrid } from "../components/DataGrid";
import { AiPanel } from "../components/AiPanel";
import { Select } from "../components/Select";
import { SegmentedControl } from "../components/SegmentedControl";
import { useToast } from "../components/Toast";
import { useUIMode } from "../lib/uiMode";
import { quoteIdent, sqlLiteral, sqlValueForCell, buildSelectList } from "../lib/sql";
import { useStaleGuard } from "../hooks/useStaleGuard";
import "./BrowserView.css";
import "./TableView.css";
import "./WebhookView.css"; // shares the mapping-row layout ImportCSVModal reuses

const ROW_LIMIT = 100;

export function TableView({
  initialTarget,
  onConsumeInitialTarget,
}: {
  initialTarget?: { connection: string; database: string } | null;
  onConsumeInitialTarget?: () => void;
} = {}) {
  const { mode } = useUIMode();
  const beginner = mode === "beginner";
  // Captured once at mount so a parent clearing initialTarget afterwards
  // (via onConsumeInitialTarget) doesn't affect this already-mounted view.
  const [pendingTarget] = useState(initialTarget ?? null);
  const [connections, setConnections] = useState<main.ConnectionInfo[]>([]);
  const [connection, setConnection] = useState("");
  const [databases, setDatabases] = useState<string[]>([]);
  const [database, setDatabase] = useState("");
  const [tables, setTables] = useState<main.TableInfo[] | null>(null);
  const [table, setTable] = useState("");
  // Set when a foreign-key cell is clicked in RowsPanel: narrows the target
  // table's rows down to the one being referenced, so "clicking a foreign
  // value" really does jump to that row rather than just naming its table.
  const [rowFilter, setRowFilter] = useState<{ column: string; cell: engine.Cell } | null>(null);
  const [navStack, setNavStack] = useState<{ table: string; rowFilter: { column: string; cell: engine.Cell } | null }[]>([]);
  const toast = useToast();

  function selectTable(name: string) {
    setTable(name);
    setRowFilter(null);
    setNavStack([]);
  }

  function navigateToForeignRow(refTable: string, refColumn: string, cell: engine.Cell) {
    setNavStack((s) => [...s, { table, rowFilter }]);
    setTable(refTable);
    setRowFilter({ column: refColumn, cell });
  }

  function navigateBack() {
    setNavStack((s) => {
      if (s.length === 0) return s;
      const prev = s[s.length - 1];
      setTable(prev.table);
      setRowFilter(prev.rowFilter);
      return s.slice(0, -1);
    });
  }

  useEffect(() => {
    ListConnections().then((conns) => {
      const sqlConns = conns.filter((c) => c.capabilities?.sql);
      setConnections(sqlConns);
      if (pendingTarget && sqlConns.some((c) => c.name === pendingTarget.connection)) {
        setConnection(pendingTarget.connection);
      } else if (sqlConns.length > 0) {
        setConnection(sqlConns[0].name);
      }
      onConsumeInitialTarget?.();
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const activeEngine = connections.find((c) => c.name === connection)?.engine ?? "postgres";
  const startDatabasesRequest = useStaleGuard();
  const startTablesRequest = useStaleGuard();

  useEffect(() => {
    if (!connection) return;
    const isStale = startDatabasesRequest();
    setDatabases([]);
    setDatabase("");
    TestConnection(connection)
      .then((dbs) => {
        if (isStale()) return;
        setDatabases(dbs);
        if (pendingTarget && pendingTarget.connection === connection && dbs.includes(pendingTarget.database)) {
          setDatabase(pendingTarget.database);
        } else if (dbs.length > 0) {
          setDatabase(dbs[0]);
        }
      })
      .catch((e) => {
        if (!isStale()) toast.push("error", String(e));
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection]);

  const loadTables = useCallback(() => {
    if (!connection || !database) return;
    const isStale = startTablesRequest();
    ListTables(connection, database)
      .then((infos) => {
        if (!isStale()) setTables(infos);
      })
      .catch((e) => {
        if (!isStale()) toast.push("error", String(e));
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection, database]);

  useEffect(() => {
    setTable("");
    setRowFilter(null);
    setNavStack([]);
    loadTables();
  }, [loadTables]);

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">{beginner ? "My Tables" : "Tables"}</h1>
      </div>

      {connections.length === 0 ? (
        <EmptyState
          icon={<Database size={32} />}
          title={beginner ? "No databases yet" : "No SQL connections yet"}
          description={
            beginner
              ? "Connect a database from My Databases to see your data here."
              : "Add a PostgreSQL, MySQL, or SQLite connection to browse tables here."
          }
        />
      ) : (
        <>
          <div className="scope-picker">
            <Select value={connection} onChange={setConnection} options={connections.map((c) => ({ value: c.name, label: c.name }))} />
            <Select
              value={database}
              onChange={setDatabase}
              disabled={databases.length === 0}
              placeholder={activeEngine === "postgres" ? "Select a schema" : "Select a database"}
              options={databases.map((d) => ({ value: d, label: d }))}
            />
          </div>

          {connection && database && (
            <div className="browser-layout">
              <div className="browser-sidebar">
                <div className="browser-sidebar-header">
                  <span>Tables</span>
                </div>
                {tables === null && <Skeleton height={80} />}
                {tables?.length === 0 && <div className="browser-empty-hint">No tables yet.</div>}
                {tables?.map((t) => (
                  <div key={t.name} className={`collection-item ${table === t.name ? "active" : ""}`}>
                    <button className="collection-item-btn" onClick={() => selectTable(t.name)}>
                      <span className="collection-name mono">{t.name}</span>
                      <span className="collection-meta">~{t.rowCount} rows</span>
                    </button>
                  </div>
                ))}
              </div>

              <div className="browser-main">
                {!table && (
                  <EmptyState icon={<Table2 size={32} />} title="Select a table" description="Pick a table on the left to browse its rows." />
                )}
                {table && (
                  <RowsPanel
                    connection={connection}
                    database={database}
                    table={table}
                    engineId={activeEngine}
                    onMutated={loadTables}
                    rowFilter={rowFilter}
                    onClearFilter={() => setRowFilter(null)}
                    canGoBack={navStack.length > 0}
                    onBack={navigateBack}
                    onNavigateToForeignRow={navigateToForeignRow}
                  />
                )}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}

function RowsPanel({
  connection,
  database,
  table,
  engineId,
  onMutated,
  rowFilter,
  onClearFilter,
  canGoBack,
  onBack,
  onNavigateToForeignRow,
}: {
  connection: string;
  database: string;
  table: string;
  engineId: string;
  onMutated: () => void;
  rowFilter: { column: string; cell: engine.Cell } | null;
  onClearFilter: () => void;
  canGoBack: boolean;
  onBack: () => void;
  onNavigateToForeignRow: (refTable: string, refColumn: string, cell: engine.Cell) => void;
}) {
  const [schema, setSchema] = useState<engine.TableSchema | null>(null);
  const [schemaLoaded, setSchemaLoaded] = useState(false);
  const [referencingTables, setReferencingTables] = useState<main.IncomingForeignKey[]>([]);
  const [result, setResult] = useState<engine.SQLResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [queryError, setQueryError] = useState("");
  const [selectedRow, setSelectedRow] = useState<number | null>(null);
  const [showMockAi, setShowMockAi] = useState(false);
  const [showExport, setShowExport] = useState(false);
  const [showImport, setShowImport] = useState(false);
  const toast = useToast();

  const runQuery = useCallback(() => {
    setLoading(true);
    setQueryError("");
    setSelectedRow(null);
    const ident = quoteIdent(engineId, table);
    // Waits for schema (via schemaLoaded, below) so a Postgres geometry
    // column can be wrapped in ST_AsGeoJSON from the very first query
    // instead of showing raw WKB text and re-fetching a moment later.
    const cols = buildSelectList(engineId, schema?.columns);
    // rowFilter narrows to the single referenced row after a foreign-key
    // click navigated here — same query shape, just WHERE-qualified.
    const whereClause = rowFilter
      ? ` WHERE ${quoteIdent(engineId, rowFilter.column)} = ${sqlValueForCell(rowFilter.cell, engineId)}`
      : "";
    RunSQLQuery(connection, database, `SELECT ${cols} FROM ${ident}${whereClause} LIMIT ${ROW_LIMIT}`)
      .then(setResult)
      .catch((e) => setQueryError(String(e)))
      .finally(() => setLoading(false));
  }, [connection, database, table, engineId, schema, rowFilter]);

  useEffect(() => {
    setSchema(null);
    setSchemaLoaded(false);
    GetTableSchema(connection, database, table)
      .then(setSchema)
      .catch(() => setSchema(null))
      .finally(() => setSchemaLoaded(true));
    ListReferencingTables(connection, database, table)
      .then(setReferencingTables)
      .catch(() => setReferencingTables([]));
  }, [connection, database, table]);

  useEffect(() => {
    if (!schemaLoaded) return;
    runQuery();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [schemaLoaded, runQuery]);

  const pkColumns = (schema?.columns ?? []).filter((c) => c.isPk).map((c) => c.name);
  // Only single-column primary keys support inline editing in this view —
  // composite-key UPDATEs need a WHERE clause builder the SQL editor
  // (Phase 3) will provide.
  const editableColumns = pkColumns.length === 1 ? new Set(result?.columns.filter((c) => c !== pkColumns[0]) ?? []) : new Set<string>();
  const fkByColumn = new Map((schema?.foreignKeys ?? []).map((fk) => [fk.column, fk]));
  const linkColumns = new Set(fkByColumn.keys());

  async function handleCellCommit(rowIndex: number, column: string, newDisplay: string, cell: engine.Cell) {
    if (!result || pkColumns.length !== 1) return;
    const pkCol = pkColumns[0];
    const pkCell = result.rows[rowIndex][pkCol];
    if (!pkCell) return;
    const ident = quoteIdent(engineId, table);
    // newDisplay is always a literal edited value, never inferred as
    // NULL from its text content (see sqlLiteral/sqlValueForCell) — a
    // column that legitimately stores the string "null" must be settable
    // to exactly that.
    const setClause = `${quoteIdent(engineId, column)} = ${sqlLiteral(newDisplay, cell?.type ?? "string", engineId)}`;
    const whereClause = `${quoteIdent(engineId, pkCol)} = ${sqlValueForCell(pkCell, engineId)}`;
    try {
      // Always a WHERE-qualified single-row UPDATE, so it's never
      // classified Safe-Mode-dangerous — the confirm param only matters
      // for unqualified writes, which this view never issues.
      await RunSQLExecute(connection, database, `UPDATE ${ident} SET ${setClause} WHERE ${whereClause}`, database);
      toast.push("success", "Row updated");
      runQuery();
      onMutated();
    } catch (e) {
      toast.push("error", String(e));
    }
  }

  function handleLinkClick(rowIndex: number, column: string, cell: engine.Cell) {
    const fk = fkByColumn.get(column);
    if (!fk) return;
    onNavigateToForeignRow(fk.refTable, fk.refColumn, cell);
  }

  return (
    <div>
      {(canGoBack || rowFilter) && (
        <div className="fk-nav-bar">
          {canGoBack && (
            <Button variant="ghost" onClick={onBack}>
              <ArrowLeft size={14} /> Back
            </Button>
          )}
          {rowFilter && (
            <span className="fk-filter-chip">
              Filtered: {rowFilter.column} = {rowFilter.cell.display}
              <button className="icon-btn" onClick={onClearFilter} title="Clear filter, show all rows">
                <X size={12} />
              </button>
            </span>
          )}
        </div>
      )}
      <div className="query-bar">
        <Button variant="ghost" onClick={runQuery}>
          <RefreshCw size={14} /> Refresh
        </Button>
        <Button variant="ghost" onClick={() => setShowMockAi(true)}>
          <Sparkles size={14} /> Generate mock data
        </Button>
        <Button variant="ghost" onClick={() => setShowExport(true)}>
          <FileCode size={14} /> Export schema
        </Button>
        <Button
          variant="ghost"
          disabled={!result || result.rows.length === 0}
          onClick={async () => {
            if (!result) return;
            const path = await ExportQueryResultsCSV(result, table);
            if (path) toast.push("success", `Exported to ${path}`);
          }}
        >
          <Download size={14} /> Export CSV
        </Button>
        <Button
          variant="ghost"
          disabled={!result || result.rows.length === 0}
          onClick={async () => {
            if (!result) return;
            const path = await ExportQueryResultsJSON(result, table);
            if (path) toast.push("success", `Exported to ${path}`);
          }}
        >
          <Download size={14} /> Export JSON
        </Button>
        <Button variant="ghost" onClick={() => setShowImport(true)}>
          <Upload size={14} /> Import CSV
        </Button>
        {pkColumns.length !== 1 && (
          <span className="table-pk-hint">
            {pkColumns.length === 0 ? "No primary key detected — rows are read-only." : "Composite primary key — rows are read-only."}
          </span>
        )}
      </div>

      {queryError && <div className="query-error">{queryError}</div>}
      {loading && <Skeleton height={200} />}
      {!loading && result && result.rows.length === 0 && !queryError && <EmptyState icon={<Database size={28} />} title="No rows" />}
      {!loading && result && result.rows.length > 0 && (
        <DataGrid
          columns={result.columns}
          rows={result.rows}
          editableColumns={editableColumns}
          onCellCommit={handleCellCommit}
          linkColumns={linkColumns}
          onLinkClick={handleLinkClick}
          onRowClick={setSelectedRow}
          selectedRowIndex={selectedRow ?? undefined}
        />
      )}

      {pkColumns.length === 1 && selectedRow !== null && result && (
        <RelationshipInspector
          connection={connection}
          database={database}
          engineId={engineId}
          referencingTables={referencingTables}
          pkCell={result.rows[selectedRow][pkColumns[0]]}
        />
      )}

      {showMockAi && (
        <AiPanel
          title={`Generate mock data for ${table}`}
          mode="auto"
          onGenerate={() => GenerateMockData(connection, database, engineId, table, 10)}
          onInsert={(text) => {
            navigator.clipboard.writeText(text);
            toast.push("success", "Copied INSERT statements to clipboard");
          }}
          insertLabel="Copy to clipboard"
          onClose={() => setShowMockAi(false)}
        />
      )}

      {showExport && (
        <ExportSchemaModal connection={connection} database={database} table={table} onClose={() => setShowExport(false)} />
      )}

      {showImport && (
        <ImportCSVModal
          connection={connection}
          database={database}
          table={table}
          engineId={engineId}
          schema={schema}
          onClose={() => setShowImport(false)}
          onImported={() => {
            setShowImport(false);
            runQuery();
            onMutated();
          }}
        />
      )}
    </div>
  );
}

function ExportSchemaModal({
  connection,
  database,
  table,
  onClose,
}: {
  connection: string;
  database: string;
  table: string;
  onClose: () => void;
}) {
  const [format, setFormat] = useState<"openapi" | "typescript" | "pydantic">("typescript");
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(false);
  const toast = useToast();

  const load = useCallback(() => {
    setLoading(true);
    GenerateAPISchema(connection, database, table, format)
      .then(setCode)
      .catch((e) => toast.push("error", String(e)))
      .finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection, database, table, format]);

  useEffect(load, [load]);

  return (
    <Modal
      title={`Export schema — ${table}`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Close
          </Button>
          <Button
            onClick={() => {
              navigator.clipboard.writeText(code);
              toast.push("success", "Copied to clipboard");
            }}
            disabled={!code}
          >
            <Copy size={14} /> Copy
          </Button>
        </>
      }
    >
      <div className="field">
        <label className="field-label">Format</label>
        <SegmentedControl
          value={format}
          onChange={(v) => setFormat(v as typeof format)}
          options={[
            { value: "openapi", label: "OpenAPI (YAML)" },
            { value: "typescript", label: "TypeScript" },
            { value: "pydantic", label: "Pydantic" },
          ]}
        />
      </div>
      {loading ? <Skeleton height={160} /> : <pre className="ai-output mono">{code}</pre>}
    </Modal>
  );
}

// ImportCSVModal reuses WebhookView's InsertPayloadModal pattern: pick a
// source (a CSV file's header row here, instead of a webhook payload's
// top-level keys), auto-match to table columns case-insensitively, and let
// the user override per-column before running the real import.
function ImportCSVModal({
  connection,
  database,
  table,
  engineId,
  schema,
  onClose,
  onImported,
}: {
  connection: string;
  database: string;
  table: string;
  engineId: string;
  schema: engine.TableSchema | null;
  onClose: () => void;
  onImported: () => void;
}) {
  const [path, setPath] = useState("");
  const [header, setHeader] = useState<string[]>([]);
  const [hasHeaderRow, setHasHeaderRow] = useState(true);
  const [mapping, setMapping] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const toast = useToast();

  async function pickFile() {
    const p = await PickCSVFile();
    if (!p) return;
    setPath(p);
    setError("");
    try {
      // Read regardless of hasHeaderRow: with no header, this first row is
      // still needed as a preview to know how many CSV columns there are.
      const h = await ReadCSVHeader(p);
      setHeader(h);
      const nextMapping: Record<string, string> = {};
      if (hasHeaderRow) {
        for (const col of schema?.columns ?? []) {
          const match = h.find((k) => k.toLowerCase() === col.name.toLowerCase());
          if (match) nextMapping[col.name] = match;
        }
      }
      setMapping(nextMapping);
    } catch (e) {
      setError(String(e));
    }
  }

  // With no header row, there's no header text to map against — the
  // mapping value sent to ImportCSV is instead the CSV column's 0-based
  // positional index (as a string), matching ImportCSV's contract.
  function toggleHasHeaderRow(v: boolean) {
    setHasHeaderRow(v);
    setMapping({});
  }

  const csvColumnOptions = hasHeaderRow
    ? header.map((h) => ({ value: h, label: h }))
    : header.map((_, i) => ({ value: String(i), label: `Column ${i + 1} (e.g. "${header[i]}")` }));

  async function submit() {
    if (Object.keys(mapping).length === 0) {
      toast.push("error", "Map at least one column to a CSV field");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const n = await ImportCSV(connection, database, table, path, engineId, hasHeaderRow, mapping);
      toast.push("success", `Imported ${n} row${n === 1 ? "" : "s"}`);
      onImported();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={`Import CSV — ${table}`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy || !path || Object.keys(mapping).length === 0}>
            {busy ? "Importing..." : "Import"}
          </Button>
        </>
      }
    >
      {error && <div className="query-error">{error}</div>}
      <div className="field">
        <Button variant="ghost" onClick={pickFile} disabled={busy}>
          <Upload size={14} /> {path ? "Change file" : "Choose CSV file"}
        </Button>
        {path && <span className="mono webhook-addr">{path}</span>}
      </div>
      {path && (
        <div className="field">
          <label className="field-label">
            <input type="checkbox" checked={hasHeaderRow} onChange={(e) => toggleHasHeaderRow(e.target.checked)} /> First row is a
            header
          </label>
        </div>
      )}
      {path && !hasHeaderRow && (
        <div className="field-hint">No header row: map each table column to a CSV position below (every row, including the first, is imported as data).</div>
      )}
      {path && header.length > 0 && (
        <div className="field">
          <label className="field-label">Column mapping</label>
          <div className="webhook-mapping-list">
            {(schema?.columns ?? []).map((c) => (
              <div key={c.name} className="webhook-mapping-row">
                <span className="mono">{c.name}</span>
                <span className="webhook-mapping-arrow">←</span>
                <Select
                  value={mapping[c.name] ?? ""}
                  onChange={(v) => setMapping((m) => ({ ...m, [c.name]: v }))}
                  placeholder="(skip)"
                  options={csvColumnOptions}
                />
              </div>
            ))}
          </div>
        </div>
      )}
    </Modal>
  );
}

// RelationshipInspector shows, for the row selected in the grid above, how
// many rows in each table with a foreign key pointing at this one actually
// reference it — the "split-pane relationship inspector" from the
// blueprint, minus the split pane (a below-grid panel reads better at this
// component's width than a permanent side column).
function RelationshipInspector({
  connection,
  database,
  engineId,
  referencingTables,
  pkCell,
}: {
  connection: string;
  database: string;
  engineId: string;
  referencingTables: main.IncomingForeignKey[];
  pkCell: engine.Cell | undefined;
}) {
  const [counts, setCounts] = useState<Record<string, number | "error">>({});
  const [expanded, setExpanded] = useState<string | null>(null);
  const [childRows, setChildRows] = useState<engine.SQLResult | null>(null);
  const startCountsRequest = useStaleGuard();
  const startExpandRequest = useStaleGuard();
  const toast = useToast();

  useEffect(() => {
    if (!pkCell) return;
    // One guard call covers every request this effect run fires (one per
    // referencing table) — selecting a different row before any of them
    // resolve must discard all of this run's responses together, not
    // just the first one to land.
    const isStale = startCountsRequest();
    setCounts({});
    setExpanded(null);
    setChildRows(null);
    referencingTables.forEach((ref) => {
      const ident = quoteIdent(engineId, ref.table);
      const whereClause = `${quoteIdent(engineId, ref.column)} = ${sqlValueForCell(pkCell, engineId)}`;
      RunSQLQuery(connection, database, `SELECT * FROM ${ident} WHERE ${whereClause} LIMIT 10`)
        .then((r) => {
          if (!isStale()) setCounts((c) => ({ ...c, [`${ref.table}.${ref.column}`]: r.rows.length }));
        })
        .catch(() => {
          if (!isStale()) setCounts((c) => ({ ...c, [`${ref.table}.${ref.column}`]: "error" }));
        });
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection, database, engineId, pkCell?.display, referencingTables]);

  async function toggleExpand(ref: main.IncomingForeignKey) {
    const key = `${ref.table}.${ref.column}`;
    if (expanded === key) {
      setExpanded(null);
      setChildRows(null);
      return;
    }
    const isStale = startExpandRequest();
    setExpanded(key);
    setChildRows(null);
    if (!pkCell) return;
    const ident = quoteIdent(engineId, ref.table);
    const whereClause = `${quoteIdent(engineId, ref.column)} = ${sqlLiteral(pkCell.display, pkCell.type, engineId)}`;
    try {
      const r = await RunSQLQuery(connection, database, `SELECT * FROM ${ident} WHERE ${whereClause} LIMIT 10`);
      if (!isStale()) setChildRows(r);
    } catch (e) {
      if (isStale()) return;
      toast.push("error", String(e));
      setExpanded(null);
    }
  }

  if (referencingTables.length === 0 || !pkCell) return null;

  return (
    <div className="relationship-inspector">
      <div className="relationship-inspector-title">
        Referenced by (row {pkCell.display})
        <button className="icon-btn" onClick={() => setExpanded(null)}>
          <X size={13} />
        </button>
      </div>
      <div className="relationship-list">
        {referencingTables.map((ref) => {
          const key = `${ref.table}.${ref.column}`;
          const count = counts[key];
          return (
            <div key={key} className="relationship-item">
              <button className="relationship-item-btn" onClick={() => toggleExpand(ref)}>
                <span className="mono">{ref.table}</span>.<span className="mono">{ref.column}</span>
                <span className="relationship-count">
                  {count === undefined ? "…" : count === "error" ? "error" : count === 10 ? "10+" : count}
                </span>
              </button>
              {expanded === key && (
                <div className="relationship-detail">
                  {!childRows && <Skeleton height={60} />}
                  {childRows && childRows.rows.length === 0 && <div className="browser-empty-hint">No referencing rows.</div>}
                  {childRows && childRows.rows.length > 0 && <DataGrid columns={childRows.columns} rows={childRows.rows} />}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
