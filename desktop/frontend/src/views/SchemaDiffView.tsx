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
import { Select } from "../components/Select";
import { SegmentedControl } from "../components/SegmentedControl";
import { EmptyState } from "../components/EmptyState";
import { Skeleton } from "../components/Skeleton";
import { useToast } from "../components/Toast";
import { useStaleGuard } from "../hooks/useStaleGuard";
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
  const startDatabasesRequest = useStaleGuard();

  useEffect(() => {
    if (!connection) return;
    const isStale = startDatabasesRequest();
    setDatabases([]);
    TestConnection(connection)
      .then((dbs) => {
        if (isStale()) return;
        setDatabases(dbs);
        if (dbs.length > 0) setDatabase(dbs[0]);
      })
      .catch((e) => {
        if (!isStale()) toast.push("error", String(e));
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection]);

  return (
    <div className="diff-side">
      <div className="diff-side-label">{label}</div>
      <Select
        value={connection}
        onChange={setConnection}
        options={connections.map((c) => ({ value: c.name, label: c.name }))}
        placeholder="Select a connection"
      />
      <Select
        value={database}
        onChange={setDatabase}
        options={databases.map((d) => ({ value: d, label: d }))}
        placeholder="Select a database"
        disabled={databases.length === 0}
      />
    </div>
  );
}

const CHANGE_LABELS: Record<string, string> = { added: "Added", removed: "Removed", modified: "Modified", unchanged: "Unchanged" };

interface DiffedPair {
  connA: string;
  dbA: string;
  connB: string;
  dbB: string;
}

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
  // The exact connection/database pair the currently-displayed diffs
  // actually came from — captured once DiffSchemas succeeds, and cleared
  // whenever any of the four selects change afterward. generateMigration
  // reads this snapshot instead of the live connA/dbA/connB/dbB state, so
  // it can never silently generate a migration for a different pair than
  // the one the diff table on screen is showing (e.g. diffing A vs B,
  // then changing "After" to C without re-running Diff — the diff table
  // clears immediately in that case instead of misleadingly continuing
  // to show the stale A-vs-B result).
  const [diffedPair, setDiffedPair] = useState<DiffedPair | null>(null);
  const toast = useToast();
  const startDiffRequest = useStaleGuard();

  useEffect(() => {
    ListConnections().then((conns) => setConnections(conns.filter((c) => c.capabilities?.sql)));
  }, []);

  function clearDiffState() {
    // Invalidate any in-flight runDiff so its response, once it resolves,
    // can't resurrect diffs/diffedPair for a pair the user has already
    // moved away from — see runDiff's isStale checks below. Also clear
    // loading immediately rather than waiting for that now-abandoned
    // request's own (staleness-skipped) finally block, which would
    // otherwise never run if the user doesn't start a fresh diff.
    startDiffRequest();
    setDiffs(null);
    setMigration(null);
    setDiffedPair(null);
    setError("");
    setLoading(false);
  }

  function updateConnA(v: string) {
    setConnA(v);
    clearDiffState();
  }
  function updateDbA(v: string) {
    setDbA(v);
    clearDiffState();
  }
  function updateConnB(v: string) {
    setConnB(v);
    clearDiffState();
  }
  function updateDbB(v: string) {
    setDbB(v);
    clearDiffState();
  }

  async function runDiff() {
    if (!connA || !dbA || !connB || !dbB) return;
    const isStale = startDiffRequest();
    const pair = { connA, dbA, connB, dbB };
    setLoading(true);
    setError("");
    setMigration(null);
    try {
      const result = await DiffSchemas(connA, dbA, connB, dbB);
      if (isStale()) return;
      setDiffs(result);
      setDiffedPair(pair);
    } catch (e) {
      if (isStale()) return;
      setError(String(e));
      setDiffs(null);
      setDiffedPair(null);
    } finally {
      if (!isStale()) setLoading(false);
    }
  }

  async function generateMigration() {
    if (!diffedPair) return;
    try {
      const m = await GenerateSchemaMigration(diffedPair.connA, diffedPair.dbA, diffedPair.connB, diffedPair.dbB, dialect);
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
            <ConnDbPicker label="Before" connections={connections} connection={connA} setConnection={updateConnA} database={dbA} setDatabase={updateDbA} />
            <ConnDbPicker label="After" connections={connections} connection={connB} setConnection={updateConnB} database={dbB} setDatabase={updateDbB} />
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
                <SegmentedControl
                  value={dialect}
                  onChange={setDialect}
                  options={[
                    { value: "postgres", label: "PostgreSQL" },
                    { value: "mysql", label: "MySQL" },
                    { value: "sqlite", label: "SQLite" },
                  ]}
                />
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
