import { useCallback, useEffect, useState } from "react";
import { Database, RefreshCw, Table2, X, Sparkles, FileCode, Copy } from "lucide-react";
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
} from "../../wailsjs/go/main/App";
import { main, engine } from "../../wailsjs/go/models";
import { Button } from "../components/Button";
import { Modal } from "../components/Modal";
import { EmptyState } from "../components/EmptyState";
import { Skeleton } from "../components/Skeleton";
import { DataGrid } from "../components/DataGrid";
import { AiPanel } from "../components/AiPanel";
import { useToast } from "../components/Toast";
import { quoteIdent, sqlLiteral, buildSelectList } from "../lib/sql";
import "./BrowserView.css";
import "./TableView.css";

const ROW_LIMIT = 100;

export function TableView() {
  const [connections, setConnections] = useState<main.ConnectionInfo[]>([]);
  const [connection, setConnection] = useState("");
  const [databases, setDatabases] = useState<string[]>([]);
  const [database, setDatabase] = useState("");
  const [tables, setTables] = useState<main.TableInfo[] | null>(null);
  const [table, setTable] = useState("");
  const toast = useToast();

  useEffect(() => {
    ListConnections().then((conns) => {
      const sqlConns = conns.filter((c) => c.capabilities?.sql);
      setConnections(sqlConns);
      if (sqlConns.length > 0) setConnection(sqlConns[0].name);
    });
  }, []);

  const activeEngine = connections.find((c) => c.name === connection)?.engine ?? "postgres";

  useEffect(() => {
    if (!connection) return;
    setDatabases([]);
    setDatabase("");
    TestConnection(connection)
      .then((dbs) => {
        setDatabases(dbs);
        if (dbs.length > 0) setDatabase(dbs[0]);
      })
      .catch((e) => toast.push("error", String(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection]);

  const loadTables = useCallback(() => {
    if (!connection || !database) return;
    ListTables(connection, database)
      .then(setTables)
      .catch((e) => toast.push("error", String(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection, database]);

  useEffect(() => {
    setTable("");
    loadTables();
  }, [loadTables]);

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">Tables</h1>
      </div>

      {connections.length === 0 ? (
        <EmptyState
          icon={<Database size={32} />}
          title="No SQL connections yet"
          description="Add a PostgreSQL, MySQL, or SQLite connection to browse tables here."
        />
      ) : (
        <>
          <div className="scope-picker">
            <select className="input" value={connection} onChange={(e) => setConnection(e.target.value)}>
              {connections.map((c) => (
                <option key={c.name} value={c.name}>
                  {c.name}
                </option>
              ))}
            </select>
            <select className="input" value={database} onChange={(e) => setDatabase(e.target.value)} disabled={databases.length === 0}>
              <option value="">{activeEngine === "postgres" ? "Select a schema" : "Select a database"}</option>
              {databases.map((d) => (
                <option key={d} value={d}>
                  {d}
                </option>
              ))}
            </select>
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
                    <button className="collection-item-btn" onClick={() => setTable(t.name)}>
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
}: {
  connection: string;
  database: string;
  table: string;
  engineId: string;
  onMutated: () => void;
}) {
  const [schema, setSchema] = useState<engine.TableSchema | null>(null);
  const [schemaLoaded, setSchemaLoaded] = useState(false);
  const [referencingTables, setReferencingTables] = useState<main.IncomingForeignKey[]>([]);
  const [result, setResult] = useState<engine.SQLResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [queryError, setQueryError] = useState("");
  const [peekRow, setPeekRow] = useState<{ table: string; result: engine.SQLResult | null; error: string } | null>(null);
  const [selectedRow, setSelectedRow] = useState<number | null>(null);
  const [showMockAi, setShowMockAi] = useState(false);
  const [showExport, setShowExport] = useState(false);
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
    RunSQLQuery(connection, database, `SELECT ${cols} FROM ${ident} LIMIT ${ROW_LIMIT}`)
      .then(setResult)
      .catch((e) => setQueryError(String(e)))
      .finally(() => setLoading(false));
  }, [connection, database, table, engineId, schema]);

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
    const setClause = `${quoteIdent(engineId, column)} = ${sqlLiteral(newDisplay, cell?.type ?? "string")}`;
    const whereClause = `${quoteIdent(engineId, pkCol)} = ${sqlLiteral(pkCell.display, pkCell.type)}`;
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

  async function handleLinkClick(rowIndex: number, column: string, cell: engine.Cell) {
    const fk = fkByColumn.get(column);
    if (!fk || !result) return;
    setPeekRow({ table: fk.refTable, result: null, error: "" });
    try {
      const ident = quoteIdent(engineId, fk.refTable);
      const whereClause = `${quoteIdent(engineId, fk.refColumn)} = ${sqlLiteral(cell.display, cell.type)}`;
      const r = await RunSQLQuery(connection, database, `SELECT * FROM ${ident} WHERE ${whereClause} LIMIT 1`);
      setPeekRow({ table: fk.refTable, result: r, error: "" });
    } catch (e) {
      setPeekRow({ table: fk.refTable, result: null, error: String(e) });
    }
  }

  return (
    <div>
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

      {peekRow && (
        <Modal title={`${peekRow.table} — referenced row`} onClose={() => setPeekRow(null)}>
          {peekRow.error && <div className="query-error">{peekRow.error}</div>}
          {!peekRow.error && !peekRow.result && <Skeleton height={120} />}
          {peekRow.result && peekRow.result.rows.length === 0 && <EmptyState icon={<Database size={24} />} title="Referenced row not found" />}
          {peekRow.result && peekRow.result.rows.length > 0 && (
            <DataGrid columns={peekRow.result.columns} rows={peekRow.result.rows} />
          )}
        </Modal>
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
        <select className="input" value={format} onChange={(e) => setFormat(e.target.value as typeof format)}>
          <option value="openapi">OpenAPI (YAML)</option>
          <option value="typescript">TypeScript interface</option>
          <option value="pydantic">Pydantic model</option>
        </select>
      </div>
      {loading ? <Skeleton height={160} /> : <pre className="ai-output mono">{code}</pre>}
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

  useEffect(() => {
    if (!pkCell) return;
    setCounts({});
    setExpanded(null);
    setChildRows(null);
    referencingTables.forEach((ref) => {
      const ident = quoteIdent(engineId, ref.table);
      const whereClause = `${quoteIdent(engineId, ref.column)} = ${sqlLiteral(pkCell.display, pkCell.type)}`;
      RunSQLQuery(connection, database, `SELECT * FROM ${ident} WHERE ${whereClause} LIMIT 10`)
        .then((r) => setCounts((c) => ({ ...c, [`${ref.table}.${ref.column}`]: r.rows.length })))
        .catch(() => setCounts((c) => ({ ...c, [`${ref.table}.${ref.column}`]: "error" })));
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
    setExpanded(key);
    setChildRows(null);
    if (!pkCell) return;
    const ident = quoteIdent(engineId, ref.table);
    const whereClause = `${quoteIdent(engineId, ref.column)} = ${sqlLiteral(pkCell.display, pkCell.type)}`;
    const r = await RunSQLQuery(connection, database, `SELECT * FROM ${ident} WHERE ${whereClause} LIMIT 10`);
    setChildRows(r);
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
