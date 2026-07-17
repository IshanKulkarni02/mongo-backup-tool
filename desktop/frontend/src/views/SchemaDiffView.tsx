import { useEffect, useState } from "react";
import { GitCompare, FileCode, Copy, FolderGit2 } from "lucide-react";
import {
  ListConnections,
  TestConnection,
  DiffSchemas,
  GenerateSchemaMigration,
  PickMigrationsFolder,
  SaveMigration,
} from "../../wailsjs/go/main/App";
import { main, schemadiff } from "../../wailsjs/go/models";
import { Button } from "../components/Button";
import { Card } from "../components/Card";
import { Modal } from "../components/Modal";
import { Input } from "../components/Input";
import { EmptyState } from "../components/EmptyState";
import { Skeleton } from "../components/Skeleton";
import { useToast } from "../components/Toast";
import "./BrowserView.css";
import "./SchemaDiffView.css";

function ConnDbPicker({
  label,
  connections,
  connection,
  setConnection,
  database,
  setDatabase,
}: {
  label: string;
  connections: main.ConnectionInfo[];
  connection: string;
  setConnection: (v: string) => void;
  database: string;
  setDatabase: (v: string) => void;
}) {
  const [databases, setDatabases] = useState<string[]>([]);
  const toast = useToast();

  useEffect(() => {
    if (!connection) return;
    setDatabases([]);
    TestConnection(connection)
      .then((dbs) => {
        setDatabases(dbs);
        if (dbs.length > 0) setDatabase(dbs[0]);
      })
      .catch((e) => toast.push("error", String(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection]);

  return (
    <div className="diff-side">
      <div className="diff-side-label">{label}</div>
      <select className="input" value={connection} onChange={(e) => setConnection(e.target.value)}>
        <option value="">Select a connection</option>
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
  );
}

const CHANGE_LABELS: Record<string, string> = { added: "Added", removed: "Removed", modified: "Modified", unchanged: "Unchanged" };

export function SchemaDiffView() {
  const [connections, setConnections] = useState<main.ConnectionInfo[]>([]);
  const [connA, setConnA] = useState("");
  const [dbA, setDbA] = useState("");
  const [connB, setConnB] = useState("");
  const [dbB, setDbB] = useState("");
  const [diffs, setDiffs] = useState<schemadiff.TableDiff[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [dialect, setDialect] = useState("postgres");
  const [migration, setMigration] = useState<schemadiff.Migration | null>(null);
  const [showSave, setShowSave] = useState(false);
  const toast = useToast();

  useEffect(() => {
    ListConnections().then((conns) => setConnections(conns.filter((c) => c.capabilities?.sql)));
  }, []);

  async function runDiff() {
    if (!connA || !dbA || !connB || !dbB) return;
    setLoading(true);
    setError("");
    setMigration(null);
    try {
      const result = await DiffSchemas(connA, dbA, connB, dbB);
      setDiffs(result);
    } catch (e) {
      setError(String(e));
      setDiffs(null);
    } finally {
      setLoading(false);
    }
  }

  async function generateMigration() {
    try {
      const m = await GenerateSchemaMigration(connA, dbA, connB, dbB, dialect);
      setMigration(m);
    } catch (e) {
      toast.push("error", String(e));
    }
  }

  function copyMigration() {
    if (!migration) return;
    navigator.clipboard.writeText(migration.sql);
    toast.push("success", "Migration script copied to clipboard");
  }

  const changed = diffs?.filter((d) => d.change !== "unchanged") ?? [];

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">Schema Diff</h1>
      </div>

      {connections.length < 1 ? (
        <EmptyState icon={<GitCompare size={32} />} title="No SQL connections yet" description="Add at least one PostgreSQL, MySQL, or SQLite connection to diff schemas." />
      ) : (
        <>
          <div className="diff-picker-row">
            <ConnDbPicker label="Before" connections={connections} connection={connA} setConnection={setConnA} database={dbA} setDatabase={setDbA} />
            <ConnDbPicker label="After" connections={connections} connection={connB} setConnection={setConnB} database={dbB} setDatabase={setDbB} />
          </div>

          <div className="query-bar">
            <Button onClick={runDiff} disabled={!connA || !dbA || !connB || !dbB || loading}>
              <GitCompare size={14} /> Diff
            </Button>
          </div>

          {error && <div className="query-error">{error}</div>}
          {loading && <Skeleton height={160} />}

          {!loading && diffs && changed.length === 0 && <EmptyState icon={<GitCompare size={28} />} title="No differences" />}

          {!loading && changed.length > 0 && (
            <>
              <div className="diff-table-list">
                {changed.map((d) => (
                  <Card key={d.table} className={`diff-table-card diff-change-${d.change}`}>
                    <div className="diff-table-header">
                      <span className="mono">{d.table}</span>
                      <span className={`diff-badge diff-badge-${d.change}`}>{CHANGE_LABELS[d.change]}</span>
                    </div>
                    {d.change === "modified" && (
                      <div className="diff-columns">
                        {d.columns
                          .filter((c) => c.change !== "unchanged")
                          .map((c) => (
                            <div key={c.name} className={`diff-column-row diff-change-${c.change}`}>
                              <span className={`diff-badge diff-badge-${c.change}`}>{CHANGE_LABELS[c.change]}</span>
                              <span className="mono">{c.name}</span>
                              {c.change === "modified" && (
                                <span className="diff-column-types mono">
                                  {c.before?.dataType} → {c.after?.dataType}
                                </span>
                              )}
                              {c.change === "added" && <span className="diff-column-types mono">{c.after?.dataType}</span>}
                              {c.change === "removed" && <span className="diff-column-types mono">{c.before?.dataType}</span>}
                            </div>
                          ))}
                      </div>
                    )}
                  </Card>
                ))}
              </div>

              <div className="query-bar">
                <select className="input" value={dialect} onChange={(e) => setDialect(e.target.value)}>
                  <option value="postgres">PostgreSQL</option>
                  <option value="mysql">MySQL</option>
                  <option value="sqlite">SQLite</option>
                </select>
                <Button onClick={generateMigration}>
                  <FileCode size={14} /> Generate migration
                </Button>
              </div>

              {migration && (
                <div className="migration-panel">
                  {migration.warnings.length > 0 && (
                    <div className="migration-warnings">
                      {migration.warnings.map((w, i) => (
                        <div key={i}>{w}</div>
                      ))}
                    </div>
                  )}
                  <div className="migration-header">
                    <span>Migration script</span>
                    <div className="migration-header-actions">
                      <Button variant="ghost" onClick={copyMigration}>
                        <Copy size={13} /> Copy
                      </Button>
                      <Button variant="ghost" onClick={() => setShowSave(true)}>
                        <FolderGit2 size={13} /> Save to folder
                      </Button>
                    </div>
                  </div>
                  <pre className="migration-sql mono">{migration.sql}</pre>
                </div>
              )}
            </>
          )}
        </>
      )}

      {showSave && migration && <SaveMigrationModal sql={migration.sql} onClose={() => setShowSave(false)} />}
    </div>
  );
}

function SaveMigrationModal({ sql, onClose }: { sql: string; onClose: () => void }) {
  const [folder, setFolder] = useState("");
  const [name, setName] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  async function pickFolder() {
    try {
      const dir = await PickMigrationsFolder();
      if (dir) setFolder(dir);
    } catch (e) {
      toast.push("error", String(e));
    }
  }

  async function submit() {
    if (!folder) return;
    setBusy(true);
    try {
      const result = await SaveMigration(folder, name, sql, message);
      toast.push("success", result.committed ? `Saved and committed to git: ${result.filePath}` : `Saved: ${result.filePath}`);
      onClose();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title="Save migration to folder"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy || !folder}>
            {busy ? "Saving..." : "Save"}
          </Button>
        </>
      }
    >
      <div className="field">
        <label className="field-label">Folder</label>
        <div className="migration-folder-row">
          <Input value={folder} onChange={(e) => setFolder(e.target.value)} mono placeholder="/path/to/migrations" />
          <Button variant="ghost" onClick={pickFolder}>
            Browse
          </Button>
        </div>
      </div>
      <Input label="Migration name" placeholder="e.g. add users email" value={name} onChange={(e) => setName(e.target.value)} />
      <Input
        label="Commit message (optional — used if the folder is a git repo)"
        placeholder="Add migration for users.email"
        value={message}
        onChange={(e) => setMessage(e.target.value)}
      />
    </Modal>
  );
}
