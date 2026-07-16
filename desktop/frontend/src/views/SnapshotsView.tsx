import { useCallback, useEffect, useState } from "react";
import { Camera, GitCompare, RotateCcw, Tag, Trash, ChevronDown, ChevronRight } from "lucide-react";
import {
  ListConnections,
  TestConnection,
  ListSnapshots,
  CreateSnapshot,
  DiffSnapshots,
  DiffCollectionChanges,
  RestoreSnapshot,
  TagSnapshot,
  GCSnapshots,
} from "../../wailsjs/go/main/App";
import { main, snapshot } from "../../wailsjs/go/models";
import { Button } from "../components/Button";
import { Card } from "../components/Card";
import { Input } from "../components/Input";
import { Modal } from "../components/Modal";
import { EmptyState } from "../components/EmptyState";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { Skeleton } from "../components/Skeleton";
import { useToast } from "../components/Toast";
import { useJobUpdates, Job } from "../hooks/useJobs";
import "./SnapshotsView.css";

const RELOAD_ON_JOB_TYPES = new Set(["snapshot-create", "snapshot-restore"]);

export function SnapshotsView() {
  const [connections, setConnections] = useState<main.ConnectionInfo[]>([]);
  const [connection, setConnection] = useState("");
  const [databases, setDatabases] = useState<string[]>([]);
  const [database, setDatabase] = useState("");
  const [snapshots, setSnapshots] = useState<snapshot.Summary[] | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [restoreTarget, setRestoreTarget] = useState<snapshot.Summary | null>(null);
  const [restoreBusy, setRestoreBusy] = useState(false);
  const [tagTarget, setTagTarget] = useState<snapshot.Summary | null>(null);
  const [compareFrom, setCompareFrom] = useState("");
  const [compareTo, setCompareTo] = useState(""); // "" = live
  const toast = useToast();

  useEffect(() => {
    ListConnections().then((conns) => {
      setConnections(conns);
      if (conns.length > 0) setConnection(conns[0].name);
    });
  }, []);

  useEffect(() => {
    if (!connection) return;
    setDatabases([]);
    setDatabase("");
    TestConnection(connection)
      .then(setDatabases)
      .catch((e) => toast.push("error", String(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection]);

  const loadSnapshots = useCallback(() => {
    if (!connection || !database) return;
    ListSnapshots(connection, database).then((items) => {
      setSnapshots(items);
      if (items.length > 0) setCompareFrom((prev) => prev || items[items.length - 1].id);
    });
  }, [connection, database]);

  useEffect(() => {
    loadSnapshots();
  }, [loadSnapshots]);

  // A snapshot create or restore (which takes a safety snapshot first)
  // changes this database's history — reload once that job actually
  // finishes, rather than guessing with a fixed delay.
  const onJobUpdate = useCallback(
    (job: Job) => {
      if (job.status === "done" && RELOAD_ON_JOB_TYPES.has(job.type)) {
        loadSnapshots();
      }
    },
    [loadSnapshots]
  );
  useJobUpdates(onJobUpdate);

  async function handleRestore() {
    if (!restoreTarget) return;
    setRestoreBusy(true);
    try {
      await RestoreSnapshot(connection, database, restoreTarget.id);
      toast.push("info", "Restore started...");
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setRestoreBusy(false);
      setRestoreTarget(null);
    }
  }

  const canCompare = connection && database && compareFrom;

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">Snapshots</h1>
        <Button onClick={() => setShowCreate(true)} disabled={!connection || !database}>
          <Camera size={16} /> Take snapshot
        </Button>
      </div>

      <div className="scope-picker">
        <select className="input" value={connection} onChange={(e) => setConnection(e.target.value)}>
          {connections.length === 0 && <option value="">No connections</option>}
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

      {connection && database && (
        <>
          {snapshots && snapshots.length > 1 && (
            <CompareBar
              snapshots={snapshots}
              from={compareFrom}
              to={compareTo}
              onFrom={setCompareFrom}
              onTo={setCompareTo}
            />
          )}

          {canCompare && (
            <DiffPanel connection={connection} database={database} fromID={compareFrom} toID={compareTo} />
          )}

          <div className="timeline">
            {snapshots === null && (
              <>
                <Skeleton height={64} />
                <Skeleton height={64} />
              </>
            )}
            {snapshots?.length === 0 && (
              <EmptyState
                icon={<Camera size={32} />}
                title="No snapshots yet"
                description="Take a snapshot to start tracking this database's history."
              />
            )}
            {snapshots?.map((s) => (
              <Card key={s.id} className="snap-row">
                <div className="snap-info">
                  <div className="snap-id mono">{s.id.slice(0, 8)}</div>
                  <div className="snap-meta">
                    <span>{s.createdAt}</span>
                    <span>{s.docCount} docs</span>
                    {s.tags?.map((t) => (
                      <span key={t} className="snap-tag">
                        {t}
                      </span>
                    ))}
                  </div>
                  {s.message && <div className="snap-message">{s.message}</div>}
                </div>
                <div className="snap-actions">
                  <Button variant="ghost" onClick={() => setTagTarget(s)}>
                    <Tag size={16} />
                  </Button>
                  <Button variant="ghost" onClick={() => setRestoreTarget(s)}>
                    <RotateCcw size={16} /> Restore
                  </Button>
                </div>
              </Card>
            ))}
          </div>

          {snapshots && snapshots.length > 0 && (
            <GCBar
              connection={connection}
              database={database}
              onDone={loadSnapshots}
            />
          )}
        </>
      )}

      {showCreate && (
        <CreateSnapshotModal
          connection={connection}
          database={database}
          onClose={() => setShowCreate(false)}
          onCreated={() => setShowCreate(false)}
        />
      )}

      {restoreTarget && (
        <ConfirmDialog
          title="Restore snapshot"
          message={`Restore ${restoreTarget.id.slice(0, 8)} into ${connection}/${database} in place?\nA safety snapshot of the current state is taken automatically first.`}
          confirmLabel="Restore"
          danger
          busy={restoreBusy}
          onConfirm={handleRestore}
          onCancel={() => setRestoreTarget(null)}
        />
      )}

      {tagTarget && (
        <TagModal
          connection={connection}
          database={database}
          snapshotID={tagTarget.id}
          onClose={() => setTagTarget(null)}
          onTagged={() => {
            setTagTarget(null);
            loadSnapshots();
          }}
        />
      )}
    </div>
  );
}

function CompareBar({
  snapshots,
  from,
  to,
  onFrom,
  onTo,
}: {
  snapshots: snapshot.Summary[];
  from: string;
  to: string;
  onFrom: (v: string) => void;
  onTo: (v: string) => void;
}) {
  return (
    <div className="compare-bar">
      <GitCompare size={16} />
      <span>Compare</span>
      <select className="input" value={from} onChange={(e) => onFrom(e.target.value)}>
        {snapshots.map((s) => (
          <option key={s.id} value={s.id}>
            {s.id.slice(0, 8)} — {s.createdAt}
          </option>
        ))}
      </select>
      <span>vs.</span>
      <select className="input" value={to} onChange={(e) => onTo(e.target.value)}>
        <option value="">Live database</option>
        {snapshots.map((s) => (
          <option key={s.id} value={s.id}>
            {s.id.slice(0, 8)} — {s.createdAt}
          </option>
        ))}
      </select>
    </div>
  );
}

function DiffPanel({
  connection,
  database,
  fromID,
  toID,
}: {
  connection: string;
  database: string;
  fromID: string;
  toID: string;
}) {
  const [result, setResult] = useState<main.DiffSummaryResult | null>(null);
  const [loading, setLoading] = useState(false);
  const toast = useToast();

  useEffect(() => {
    if (!fromID) return;
    setLoading(true);
    setResult(null);
    DiffSnapshots(connection, database, fromID, toID)
      .then(setResult)
      .catch((e) => toast.push("error", String(e)))
      .finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection, database, fromID, toID]);

  return (
    <Card className="diff-panel">
      {loading && <Skeleton height={40} />}
      {!loading && result && result.collections.length === 0 && (
        <div className="diff-empty">No differences.</div>
      )}
      {!loading &&
        result?.collections.map((c) => (
          <DiffCollectionRow
            key={c.name}
            connection={connection}
            database={database}
            fromID={fromID}
            toID={toID}
            summary={c}
          />
        ))}
    </Card>
  );
}

function DiffCollectionRow({
  connection,
  database,
  fromID,
  toID,
  summary,
}: {
  connection: string;
  database: string;
  fromID: string;
  toID: string;
  summary: main.CollectionDiffSummary;
}) {
  const [open, setOpen] = useState<null | "added" | "modified" | "removed">(null);
  const [page, setPage] = useState<main.DiffChangePage | null>(null);
  const [loadingPage, setLoadingPage] = useState(false);

  async function toggle(kind: "added" | "modified" | "removed") {
    if (open === kind) {
      setOpen(null);
      return;
    }
    setOpen(kind);
    setLoadingPage(true);
    try {
      const p = await DiffCollectionChanges(connection, database, fromID, toID, summary.name, kind, 0, 100);
      setPage(p);
    } finally {
      setLoadingPage(false);
    }
  }

  return (
    <div className="diff-collection">
      <div className="diff-collection-header">{summary.name}</div>
      <div className="diff-badges">
        <DiffBadge kind="added" count={summary.addedCount} active={open === "added"} onClick={() => toggle("added")} />
        <DiffBadge
          kind="modified"
          count={summary.modifiedCount}
          active={open === "modified"}
          onClick={() => toggle("modified")}
        />
        <DiffBadge
          kind="removed"
          count={summary.removedCount}
          active={open === "removed"}
          onClick={() => toggle("removed")}
        />
      </div>
      {open && (
        <div className="diff-ids">
          {loadingPage && <Skeleton height={20} />}
          {!loadingPage &&
            page?.ids.map((id) => (
              <div key={id} className="mono diff-id">
                {id}
              </div>
            ))}
          {!loadingPage && page && page.total > page.ids.length + page.offset && (
            <div className="diff-more">
              +{page.total - page.ids.length - page.offset} more (showing first {page.ids.length + page.offset} of {page.total})
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function DiffBadge({
  kind,
  count,
  active,
  onClick,
}: {
  kind: "added" | "modified" | "removed";
  count: number;
  active: boolean;
  onClick: () => void;
}) {
  if (count === 0) return null;
  return (
    <button className={`diff-badge diff-badge-${kind} ${active ? "active" : ""}`} onClick={onClick}>
      {active ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
      {kind === "added" ? "+" : kind === "removed" ? "-" : "~"} {count}
    </button>
  );
}

function CreateSnapshotModal({
  connection,
  database,
  onClose,
  onCreated,
}: {
  connection: string;
  database: string;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  async function submit() {
    setBusy(true);
    try {
      await CreateSnapshot(connection, database, message);
      toast.push("info", "Snapshot started...");
      onCreated();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title="Take snapshot"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy}>
            {busy ? "Starting..." : "Take snapshot"}
          </Button>
        </>
      }
    >
      <Input
        label="Message (optional)"
        placeholder="before migration"
        value={message}
        onChange={(e) => setMessage(e.target.value)}
        autoFocus
      />
    </Modal>
  );
}

function TagModal({
  connection,
  database,
  snapshotID,
  onClose,
  onTagged,
}: {
  connection: string;
  database: string;
  snapshotID: string;
  onClose: () => void;
  onTagged: () => void;
}) {
  const [tag, setTag] = useState("");
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  async function submit() {
    if (!tag) return;
    setBusy(true);
    try {
      await TagSnapshot(connection, database, snapshotID, tag);
      toast.push("success", `Tagged ${snapshotID.slice(0, 8)} as "${tag}"`);
      onTagged();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title="Tag snapshot"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy || !tag}>
            {busy ? "Saving..." : "Tag"}
          </Button>
        </>
      }
    >
      <Input
        label="Tag"
        placeholder="v1.0-before-migration"
        value={tag}
        onChange={(e) => setTag(e.target.value)}
        autoFocus
      />
      <p className="hint">Tagged snapshots are always protected from cleanup (gc).</p>
    </Modal>
  );
}

function GCBar({ connection, database, onDone }: { connection: string; database: string; onDone: () => void }) {
  const [keepLast, setKeepLast] = useState(10);
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  const run = async () => {
    setBusy(true);
    try {
      const result = await GCSnapshots(connection, database, keepLast);
      toast.push("success", `Deleted ${result.manifestsDeleted} snapshot(s), freed ${result.objectsDeleted} object(s)`);
      onDone();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="gc-bar">
      <Trash size={14} />
      <span>Keep last</span>
      <input
        type="number"
        className="input gc-input"
        value={keepLast}
        min={0}
        onChange={(e) => setKeepLast(Number(e.target.value))}
      />
      <span>snapshots, prune the rest (tagged snapshots are always kept)</span>
      <Button variant="ghost" onClick={run} disabled={busy}>
        {busy ? "Running..." : "Run gc"}
      </Button>
    </div>
  );
}
