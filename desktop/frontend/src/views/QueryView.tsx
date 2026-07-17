import { useCallback, useEffect, useState } from "react";
import CodeMirror from "@uiw/react-codemirror";
import { sql as sqlLang } from "@codemirror/lang-sql";
import { Database, Play, Square, Wand2, Sparkles, Wrench, Save } from "lucide-react";
import {
  ListConnections,
  TestConnection,
  ListTables,
  RunSQLQueryJob,
  RunSQLExecute,
  ClassifySQL,
  ExplainSQL,
  ExplainWithAI,
  GenerateSQL,
  FixSQLError,
  CancelJob,
  SaveQuery,
} from "../../wailsjs/go/main/App";
import { main, engine, safeguard } from "../../wailsjs/go/models";
import { useJobUpdates, Job } from "../hooks/useJobs";
import { Button } from "../components/Button";
import { Modal } from "../components/Modal";
import { Input } from "../components/Input";
import { EmptyState } from "../components/EmptyState";
import { Skeleton } from "../components/Skeleton";
import { DataGrid } from "../components/DataGrid";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { AiPanel } from "../components/AiPanel";
import { useToast } from "../components/Toast";
import "./BrowserView.css";
import "./QueryView.css";

function looksLikeRead(text: string): boolean {
  const s = text.trim().toUpperCase();
  return (
    s.startsWith("SELECT") ||
    s.startsWith("WITH") ||
    s.startsWith("SHOW") ||
    s.startsWith("PRAGMA") ||
    s.startsWith("EXPLAIN")
  );
}

export function QueryView() {
  const [connections, setConnections] = useState<main.ConnectionInfo[]>([]);
  const [connection, setConnection] = useState("");
  const [databases, setDatabases] = useState<string[]>([]);
  const [database, setDatabase] = useState("");
  const [sqlText, setSqlText] = useState("SELECT 1");

  const [jobId, setJobId] = useState<string | null>(null);
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<engine.SQLResult | null>(null);
  const [queryError, setQueryError] = useState("");
  const [rowsAffected, setRowsAffected] = useState<number | null>(null);
  const [explain, setExplain] = useState<string | null>(null);
  const [pending, setPending] = useState<{ sql: string; classification: safeguard.Classification } | null>(null);
  const [aiMode, setAiMode] = useState<"generate" | "fix" | "explain" | null>(null);
  const [showSave, setShowSave] = useState(false);
  const toast = useToast();

  const activeEngine = connections.find((c) => c.name === connection)?.engine ?? "postgres";

  useEffect(() => {
    ListConnections().then((conns) => {
      const sqlConns = conns.filter((c) => c.capabilities?.sql);
      setConnections(sqlConns);
      if (sqlConns.length > 0) setConnection(sqlConns[0].name);
    });
  }, []);

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

  const onJobUpdate = useCallback(
    (job: Job) => {
      if (job.type !== "sql-query" || job.id !== jobId || job.status === "running") return;
      setRunning(false);
      setJobId(null);
      if (job.status === "done") {
        setResult(job.result as unknown as engine.SQLResult);
        setRowsAffected(null);
        setQueryError("");
      } else {
        setQueryError(job.message ?? "Query failed");
        setResult(null);
      }
    },
    [jobId]
  );
  useJobUpdates(onJobUpdate);

  function runQuery() {
    setRunning(true);
    setQueryError("");
    setExplain(null);
    RunSQLQueryJob(connection, database, sqlText).then(setJobId);
  }

  async function executeStatement(text: string, confirmDatabaseName: string) {
    setRunning(true);
    setQueryError("");
    setExplain(null);
    try {
      const n = await RunSQLExecute(connection, database, text, confirmDatabaseName);
      setRowsAffected(n);
      setResult(null);
      toast.push("success", `${n} row${n === 1 ? "" : "s"} affected`);
    } catch (e) {
      setQueryError(String(e));
    } finally {
      setRunning(false);
    }
  }

  async function handleRun() {
    const trimmed = sqlText.trim();
    if (!trimmed || !connection || !database) return;
    if (looksLikeRead(trimmed)) {
      runQuery();
      return;
    }
    const classification = await ClassifySQL(trimmed);
    if (classification.risk === "none") {
      executeStatement(trimmed, "");
      return;
    }
    setPending({ sql: trimmed, classification });
  }

  function handleCancel() {
    if (jobId) CancelJob(jobId);
  }

  async function handleExplain() {
    const trimmed = sqlText.trim();
    if (!trimmed || !connection || !database) return;
    try {
      const plan = await ExplainSQL(connection, database, trimmed);
      setExplain(plan);
    } catch (e) {
      toast.push("error", String(e));
    }
  }

  async function currentTableNames(): Promise<string[]> {
    if (!connection || !database) return [];
    try {
      const tables = await ListTables(connection, database);
      return tables.map((t) => t.name);
    } catch {
      return [];
    }
  }

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">Query</h1>
      </div>

      {connections.length === 0 ? (
        <EmptyState
          icon={<Database size={32} />}
          title="No SQL connections yet"
          description="Add a PostgreSQL, MySQL, or SQLite connection to run queries here."
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
              <option value="">Select a database</option>
              {databases.map((d) => (
                <option key={d} value={d}>
                  {d}
                </option>
              ))}
            </select>
          </div>

          <div className="sql-editor-wrap">
            <CodeMirror
              value={sqlText}
              height="160px"
              extensions={[sqlLang()]}
              onChange={setSqlText}
              theme="dark"
            />
          </div>

          <div className="query-bar">
            {!running ? (
              <Button onClick={handleRun} disabled={!connection || !database}>
                <Play size={14} /> Run
              </Button>
            ) : (
              <Button variant="danger" onClick={handleCancel}>
                <Square size={14} /> Cancel
              </Button>
            )}
            <Button variant="ghost" onClick={handleExplain} disabled={!connection || !database || running}>
              <Wand2 size={14} /> Explain
            </Button>
            <Button variant="ghost" onClick={() => setAiMode("generate")} disabled={!connection || !database}>
              <Sparkles size={14} /> Ask AI
            </Button>
            {explain && (
              <Button variant="ghost" onClick={() => setAiMode("explain")}>
                <Sparkles size={14} /> Explain (AI)
              </Button>
            )}
            {queryError && !queryError.toLowerCase().includes("context canceled") && (
              <Button variant="ghost" onClick={() => setAiMode("fix")}>
                <Wrench size={14} /> Fix with AI
              </Button>
            )}
            <Button variant="ghost" onClick={() => setShowSave(true)} disabled={!connection || !database || !sqlText.trim()}>
              <Save size={14} /> Save query
            </Button>
          </div>

          {queryError && <div className="query-error">{queryError}</div>}
          {running && <Skeleton height={120} />}

          {explain && (
            <div className="explain-panel">
              <div className="explain-title">Query plan</div>
              <pre className="explain-text mono">{explain}</pre>
            </div>
          )}

          {!running && rowsAffected !== null && !result && (
            <div className="query-success">{rowsAffected} row(s) affected.</div>
          )}

          {!running && result && result.rows.length === 0 && !queryError && <EmptyState icon={<Database size={28} />} title="No rows" />}
          {!running && result && result.rows.length > 0 && <DataGrid columns={result.columns} rows={result.rows} />}
        </>
      )}

      {pending && pending.classification.risk === "confirm" && (
        <ConfirmDialog
          title="Confirm statement"
          message={`${pending.classification.reason}. Run it?`}
          confirmLabel="Run"
          danger
          onConfirm={() => {
            const p = pending;
            setPending(null);
            executeStatement(p.sql, "");
          }}
          onCancel={() => setPending(null)}
        />
      )}

      {pending && pending.classification.risk === "dangerous" && (
        <DangerousConfirmModal
          reason={pending.classification.reason}
          database={database}
          onCancel={() => setPending(null)}
          onConfirm={(typedName) => {
            const p = pending;
            setPending(null);
            executeStatement(p.sql, typedName);
          }}
        />
      )}

      {aiMode === "generate" && (
        <AiPanel
          title="Ask AI to write SQL"
          mode="prompt"
          placeholder="e.g. show users who signed up last month"
          onGenerate={async (request) => {
            const tables = await currentTableNames();
            return GenerateSQL(connection, database, activeEngine, tables, request ?? "");
          }}
          onInsert={(text) => setSqlText(text.trim())}
          insertLabel="Insert into editor"
          onClose={() => setAiMode(null)}
        />
      )}

      {aiMode === "fix" && (
        <AiPanel
          title="Fix this statement with AI"
          mode="auto"
          onGenerate={async () => {
            const tables = await currentTableNames();
            return FixSQLError(connection, database, activeEngine, sqlText, queryError, tables);
          }}
          onInsert={(text) => {
            setSqlText(text.trim());
            setQueryError("");
          }}
          insertLabel="Replace editor"
          onClose={() => setAiMode(null)}
        />
      )}

      {aiMode === "explain" && explain && (
        <AiPanel
          title="Explain plan (AI)"
          mode="auto"
          onGenerate={() => ExplainWithAI(activeEngine, sqlText, explain)}
          onClose={() => setAiMode(null)}
        />
      )}

      {showSave && (
        <SaveQueryModal
          connection={connection}
          database={database}
          sqlText={sqlText}
          onClose={() => setShowSave(false)}
        />
      )}
    </div>
  );
}

function SaveQueryModal({
  connection,
  database,
  sqlText,
  onClose,
}: {
  connection: string;
  database: string;
  sqlText: string;
  onClose: () => void;
}) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  async function submit() {
    if (!name.trim()) return;
    setBusy(true);
    try {
      await SaveQuery("", name.trim(), connection, database, sqlText);
      toast.push("success", `Saved "${name}" — see it on the Dashboard`);
      onClose();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title="Save query"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy || !name.trim()}>
            {busy ? "Saving..." : "Save"}
          </Button>
        </>
      }
    >
      <Input label="Name" placeholder="e.g. Active users" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
    </Modal>
  );
}

function DangerousConfirmModal({
  reason,
  database,
  onCancel,
  onConfirm,
}: {
  reason: string;
  database: string;
  onCancel: () => void;
  onConfirm: (typedName: string) => void;
}) {
  const [typed, setTyped] = useState("");
  const matches = typed === database;

  return (
    <Modal
      title="Dangerous statement"
      onClose={onCancel}
      footer={
        <>
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="danger" disabled={!matches} onClick={() => onConfirm(typed)}>
            Run anyway
          </Button>
        </>
      }
    >
      <p className="danger-reason">{reason}.</p>
      <p>
        Type the database name <strong className="mono">{database}</strong> to confirm.
      </p>
      <Input value={typed} onChange={(e) => setTyped(e.target.value)} mono autoFocus placeholder={database} />
    </Modal>
  );
}
