import { useCallback, useEffect, useState } from "react";
import { Archive, Plus, RotateCcw, Trash2 } from "lucide-react";
import { ListBackups, ListConnections, TestConnection, CreateBackup, RestoreBackup, DeleteBackup } from "../../wailsjs/go/main/App";
import { main, store } from "../../wailsjs/go/models";
import { Button } from "../components/Button";
import { Card } from "../components/Card";
import { Modal } from "../components/Modal";
import { EmptyState } from "../components/EmptyState";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { Skeleton } from "../components/Skeleton";
import { useToast } from "../components/Toast";
import { useJobUpdates, Job } from "../hooks/useJobs";
import "./BackupsView.css";

function humanSize(n: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${units[i]}`;
}

export function BackupsView() {
  const [backups, setBackups] = useState<store.Backup[] | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [restoreTarget, setRestoreTarget] = useState<store.Backup | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<store.Backup | null>(null);
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  const load = useCallback(() => {
    ListBackups().then(setBackups);
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const onJobUpdate = useCallback(
    (job: Job) => {
      if (job.status === "done" && job.type === "backup-create") load();
    },
    [load]
  );
  useJobUpdates(onJobUpdate);

  async function handleRestore() {
    if (!restoreTarget) return;
    setBusy(true);
    try {
      await RestoreBackup(restoreTarget.connection, restoreTarget.id);
      toast.push("info", "Restore started...");
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setBusy(false);
      setRestoreTarget(null);
    }
  }

  async function handleDelete() {
    if (!deleteTarget) return;
    try {
      await DeleteBackup(deleteTarget.id);
      toast.push("success", "Backup deleted");
      load();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setDeleteTarget(null);
    }
  }

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">Backups</h1>
        <Button onClick={() => setShowCreate(true)}>
          <Plus size={16} /> Create backup
        </Button>
      </div>

      {backups === null && (
        <>
          <Skeleton height={56} />
          <div style={{ height: 8 }} />
          <Skeleton height={56} />
        </>
      )}

      {backups?.length === 0 && (
        <EmptyState
          icon={<Archive size={32} />}
          title="No backups yet"
          description="A backup is a full, portable mongodump archive you can restore anywhere."
          action={
            <Button onClick={() => setShowCreate(true)}>
              <Plus size={16} /> Create backup
            </Button>
          }
        />
      )}

      <div className="backup-list">
        {backups?.map((b) => (
          <Card key={b.id} className="backup-row">
            <div className="backup-info">
              <div className="backup-id mono">{b.id.slice(0, 8)}</div>
              <div className="backup-meta">
                <span>{b.connection}</span>
                <span>{b.database || "(all databases)"}</span>
                <span>{humanSize(b.sizeBytes)}</span>
                <span>{b.createdAt}</span>
              </div>
            </div>
            <div className="backup-actions">
              <Button variant="ghost" onClick={() => setRestoreTarget(b)}>
                <RotateCcw size={16} /> Restore
              </Button>
              <Button variant="danger" onClick={() => setDeleteTarget(b)}>
                <Trash2 size={16} />
              </Button>
            </div>
          </Card>
        ))}
      </div>

      {showCreate && (
        <CreateBackupModal
          onClose={() => setShowCreate(false)}
          onCreated={() => setShowCreate(false)}
        />
      )}

      {restoreTarget && (
        <ConfirmDialog
          title="Restore backup"
          message={`Restore backup ${restoreTarget.id.slice(0, 8)} into "${restoreTarget.connection}" in place?\nThis overwrites existing data and cannot be automatically undone.`}
          confirmLabel="Restore"
          danger
          busy={busy}
          onConfirm={handleRestore}
          onCancel={() => setRestoreTarget(null)}
        />
      )}

      {deleteTarget && (
        <ConfirmDialog
          title="Delete backup"
          message={`Delete backup ${deleteTarget.id.slice(0, 8)}? This removes the archive file permanently.`}
          confirmLabel="Delete"
          danger
          onConfirm={handleDelete}
          onCancel={() => setDeleteTarget(null)}
        />
      )}
    </div>
  );
}

function CreateBackupModal({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const [connections, setConnections] = useState<main.ConnectionInfo[]>([]);
  const [connection, setConnection] = useState("");
  const [databases, setDatabases] = useState<string[]>([]);
  const [database, setDatabase] = useState("");
  const [busy, setBusy] = useState(false);
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
    TestConnection(connection).then(setDatabases);
  }, [connection]);

  async function submit() {
    if (!connection) return;
    setBusy(true);
    try {
      await CreateBackup(connection, database);
      toast.push("info", "Backup started...");
      onCreated();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title="Create backup"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy || !connection}>
            {busy ? "Starting..." : "Create backup"}
          </Button>
        </>
      }
    >
      <div className="field">
        <label className="field-label">Connection</label>
        <select className="input" value={connection} onChange={(e) => setConnection(e.target.value)}>
          {connections.map((c) => (
            <option key={c.name} value={c.name}>
              {c.name}
            </option>
          ))}
        </select>
      </div>
      <div className="field">
        <label className="field-label">Database</label>
        <select className="input" value={database} onChange={(e) => setDatabase(e.target.value)}>
          <option value="">All databases</option>
          {databases.map((d) => (
            <option key={d} value={d}>
              {d}
            </option>
          ))}
        </select>
      </div>
    </Modal>
  );
}
