import { useEffect, useState } from "react";
import { Radio, Square, Trash2, Database } from "lucide-react";
import {
  StartWebhookListener,
  StopWebhookListener,
  IsWebhookListenerRunning,
  ListConnections,
  TestConnection,
  ListCollections,
  ListTables,
  GetTableSchema,
  InsertWebhookPayload,
  RunSQLExecute,
} from "../../wailsjs/go/main/App";
import { main, engine } from "../../wailsjs/go/models";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { Button } from "../components/Button";
import { Card } from "../components/Card";
import { Input } from "../components/Input";
import { Modal } from "../components/Modal";
import { EmptyState } from "../components/EmptyState";
import { JsonTree } from "../components/JsonTree";
import { useToast } from "../components/Toast";
import { quoteIdent, sqlLiteral } from "../lib/sql";
import "./BrowserView.css";
import "./WebhookView.css";

// flattenTopLevel turns a parsed JSON payload's top-level fields into
// string values suitable for a SQL literal — objects/arrays are
// JSON-stringified rather than excluded, so a mapping can still target
// them (e.g. into a JSON/JSONB column) even though most device payloads
// are flat.
function flattenTopLevel(payload: unknown): Record<string, string> {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) return {};
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(payload as Record<string, unknown>)) {
    out[k] = typeof v === "string" ? v : JSON.stringify(v);
  }
  return out;
}

interface WebhookRequest {
  id: string;
  method: string;
  path: string;
  query: string;
  headers: Record<string, string[]>;
  body: string;
  timestamp: string;
}

// WebhookView runs a local HTTP listener so a device that pushes data to a
// webhook (e.g. a ZKTeco/eSSL biometric terminal's ADMS protocol) can be
// pointed at it instead of production, for debugging what it actually
// sends before wiring up the real integration.
export function WebhookView() {
  const [running, setRunning] = useState(false);
  const [port, setPort] = useState("8089");
  const [addr, setAddr] = useState("");
  const [requests, setRequests] = useState<WebhookRequest[]>([]);
  const [insertTarget, setInsertTarget] = useState<WebhookRequest | null>(null);
  const toast = useToast();

  useEffect(() => {
    IsWebhookListenerRunning().then(setRunning);
  }, []);

  useEffect(() => {
    const unsub = EventsOn("webhook:request", (r: WebhookRequest) => {
      setRequests((prev) => [r, ...prev].slice(0, 200));
    });
    return unsub;
  }, []);

  async function start() {
    const p = parseInt(port, 10);
    if (!p || p < 1 || p > 65535) {
      toast.push("error", "Enter a valid port number");
      return;
    }
    try {
      const a = await StartWebhookListener(p);
      setAddr(a);
      setRunning(true);
      toast.push("success", `Listening on ${a}`);
    } catch (e) {
      toast.push("error", String(e));
    }
  }

  async function stop() {
    try {
      await StopWebhookListener();
      setRunning(false);
      toast.push("success", "Listener stopped");
    } catch (e) {
      toast.push("error", String(e));
    }
  }

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">Webhook Listener</h1>
      </div>
      <p className="webhook-hint">
        Point a device that pushes data over HTTP (e.g. a biometric terminal's ADMS push protocol) at this listener
        to see exactly what it sends before wiring up a real integration.
      </p>

      <div className="query-bar">
        <Input placeholder="Port" value={port} onChange={(e) => setPort(e.target.value)} disabled={running} style={{ width: 120 }} />
        {!running ? (
          <Button onClick={start}>
            <Radio size={14} /> Start listening
          </Button>
        ) : (
          <Button variant="danger" onClick={stop}>
            <Square size={14} /> Stop
          </Button>
        )}
        {running && addr && <span className="webhook-addr mono">listening on {addr}</span>}
        {requests.length > 0 && (
          <Button variant="ghost" onClick={() => setRequests([])}>
            <Trash2 size={14} /> Clear
          </Button>
        )}
      </div>

      {requests.length === 0 && (
        <EmptyState
          icon={<Radio size={32} />}
          title={running ? "Waiting for requests..." : "Not listening"}
          description={running ? "Point your device at the address above." : "Start the listener to begin capturing requests."}
        />
      )}

      <div className="webhook-list">
        {requests.map((r) => (
          <Card key={r.id} className="webhook-row">
            <div className="webhook-row-header">
              <span className="webhook-method">{r.method}</span>
              <span className="mono">
                {r.path}
                {r.query && `?${r.query}`}
              </span>
              <span className="webhook-time">{new Date(r.timestamp).toLocaleTimeString()}</span>
              <Button variant="ghost" onClick={() => setInsertTarget(r)}>
                <Database size={13} /> Map to database
              </Button>
            </div>
            <div className="webhook-body">
              <JsonTree json={r.body} />
            </div>
          </Card>
        ))}
      </div>

      {insertTarget && <InsertPayloadModal request={insertTarget} onClose={() => setInsertTarget(null)} />}
    </div>
  );
}

function InsertPayloadModal({ request, onClose }: { request: WebhookRequest; onClose: () => void }) {
  const [connections, setConnections] = useState<main.ConnectionInfo[]>([]);
  const [connection, setConnection] = useState("");
  const [databases, setDatabases] = useState<string[]>([]);
  const [database, setDatabase] = useState("");
  const [collections, setCollections] = useState<main.CollectionInfo[]>([]);
  const [collection, setCollection] = useState("");
  const [tables, setTables] = useState<main.TableInfo[]>([]);
  const [table, setTable] = useState("");
  const [schema, setSchema] = useState<engine.TableSchema | null>(null);
  const [mapping, setMapping] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  const activeConn = connections.find((c) => c.name === connection);
  const activeEngine = activeConn?.engine ?? "";
  const isSQL = !!activeConn?.capabilities?.sql;
  const isMongo = !!activeConn?.capabilities?.documents;

  let parsedBody: unknown = null;
  let parseError = "";
  try {
    parsedBody = JSON.parse(request.body);
  } catch {
    parseError = "This request's body isn't valid JSON.";
  }
  const flatFields = flattenTopLevel(parsedBody);
  const fieldKeys = Object.keys(flatFields);

  useEffect(() => {
    ListConnections().then((conns) => {
      setConnections(conns);
      if (conns.length > 0) setConnection(conns[0].name);
    });
  }, []);

  useEffect(() => {
    if (!connection) return;
    TestConnection(connection).then((dbs) => {
      setDatabases(dbs);
      if (dbs.length > 0) setDatabase(dbs[0]);
    });
  }, [connection]);

  useEffect(() => {
    if (!connection || !database) return;
    if (isMongo) ListCollections(connection, database).then(setCollections);
    if (isSQL) ListTables(connection, database).then(setTables);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection, database, activeEngine]);

  useEffect(() => {
    if (!isSQL || !connection || !database || !table) {
      setSchema(null);
      return;
    }
    GetTableSchema(connection, database, table).then((s) => {
      setSchema(s);
      // Pre-fill the mapping with case-insensitive name matches between
      // the payload's top-level keys and the table's columns.
      const nextMapping: Record<string, string> = {};
      for (const col of s.columns) {
        const match = fieldKeys.find((k) => k.toLowerCase() === col.name.toLowerCase());
        if (match) nextMapping[col.name] = match;
      }
      setMapping(nextMapping);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isSQL, connection, database, table]);

  async function submitMongo() {
    setBusy(true);
    try {
      await InsertWebhookPayload(connection, database, collection, request.body);
      toast.push("success", "Inserted");
      onClose();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setBusy(false);
    }
  }

  async function submitSQL() {
    if (!schema) return;
    const mapped = schema.columns.filter((c) => mapping[c.name]);
    if (mapped.length === 0) {
      toast.push("error", "Map at least one column to a payload field");
      return;
    }
    setBusy(true);
    try {
      const ident = quoteIdent(activeEngine, table);
      const cols = mapped.map((c) => quoteIdent(activeEngine, c.name)).join(", ");
      const vals = mapped
        .map((c) => {
          const raw = flatFields[mapping[c.name]] ?? "";
          const looksNumeric = /^-?\d+(\.\d+)?$/.test(raw.trim());
          return sqlLiteral(raw, looksNumeric ? "number" : "string");
        })
        .join(", ");
      const sql = `INSERT INTO ${ident} (${cols}) VALUES (${vals})`;
      await RunSQLExecute(connection, database, sql, database);
      toast.push("success", "Inserted");
      onClose();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setBusy(false);
    }
  }

  const canSubmit = isMongo ? !!collection : isSQL ? !!table && Object.keys(mapping).length > 0 : false;

  return (
    <Modal
      title="Map payload to database"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={isMongo ? submitMongo : submitSQL} disabled={busy || !!parseError || !canSubmit}>
            {busy ? "Inserting..." : "Insert"}
          </Button>
        </>
      }
    >
      {parseError && <div className="query-error">{parseError}</div>}
      <div className="field">
        <label className="field-label">Connection</label>
        <select className="input" value={connection} onChange={(e) => setConnection(e.target.value)}>
          {connections.map((c) => (
            <option key={c.name} value={c.name}>
              {c.name} ({c.engine})
            </option>
          ))}
        </select>
      </div>
      <div className="field">
        <label className="field-label">{activeEngine === "postgres" ? "Schema" : "Database"}</label>
        <select className="input" value={database} onChange={(e) => setDatabase(e.target.value)}>
          {databases.map((d) => (
            <option key={d} value={d}>
              {d}
            </option>
          ))}
        </select>
      </div>

      {isMongo && (
        <div className="field">
          <label className="field-label">Collection</label>
          <select className="input" value={collection} onChange={(e) => setCollection(e.target.value)}>
            <option value="">Select a collection</option>
            {collections.map((c) => (
              <option key={c.name} value={c.name}>
                {c.name}
              </option>
            ))}
          </select>
        </div>
      )}

      {isSQL && (
        <>
          <div className="field">
            <label className="field-label">Table</label>
            <select className="input" value={table} onChange={(e) => setTable(e.target.value)}>
              <option value="">Select a table</option>
              {tables.map((t) => (
                <option key={t.name} value={t.name}>
                  {t.name}
                </option>
              ))}
            </select>
          </div>
          {schema && fieldKeys.length === 0 && (
            <div className="query-error">This payload has no top-level fields to map (it may be an array or empty object).</div>
          )}
          {schema && fieldKeys.length > 0 && (
            <div className="field">
              <label className="field-label">Column mapping</label>
              <div className="webhook-mapping-list">
                {schema.columns.map((c) => (
                  <div key={c.name} className="webhook-mapping-row">
                    <span className="mono">{c.name}</span>
                    <span className="webhook-mapping-arrow">←</span>
                    <select
                      className="input"
                      value={mapping[c.name] ?? ""}
                      onChange={(e) => setMapping((m) => ({ ...m, [c.name]: e.target.value }))}
                    >
                      <option value="">(skip)</option>
                      {fieldKeys.map((k) => (
                        <option key={k} value={k}>
                          {k}
                        </option>
                      ))}
                    </select>
                  </div>
                ))}
              </div>
            </div>
          )}
        </>
      )}

      <pre className="doc-json mono">{request.body}</pre>
    </Modal>
  );
}
