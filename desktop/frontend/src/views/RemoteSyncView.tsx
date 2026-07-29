import { useCallback, useEffect, useState } from "react";
import { Cloud, Upload, Download, GitBranch } from "lucide-react";
import {
  ListConnections,
  TestConnection,
  IsRemoteInitialized,
  InitRemote,
  PushRemote,
  PullRemote,
  CloneRemote,
} from "../../wailsjs/go/main/App";
import { main } from "../../wailsjs/go/models";
import { Button } from "../components/Button";
import { Card } from "../components/Card";
import { Input } from "../components/Input";
import { Select } from "../components/Select";
import { EmptyState } from "../components/EmptyState";
import { useToast } from "../components/Toast";
import "./RemoteSyncView.css";

// RemoteSyncView is the GUI counterpart to `dbhelm remote init/push/pull/
// clone` — internal/remote's Git+Git-LFS sync only works with the fs
// snapshot storage backend, which is why this view is gated on the same
// "snapshots" capability as Snapshots/Backups (see App.tsx's NAV).
export function RemoteSyncView() {
  const [connections, setConnections] = useState<main.ConnectionInfo[]>([]);
  const [connection, setConnection] = useState("");
  const [databases, setDatabases] = useState<string[]>([]);
  const [database, setDatabase] = useState("");
  const [initialized, setInitialized] = useState<boolean | null>(null);
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

  const refreshStatus = useCallback(() => {
    if (!connection || !database) {
      setInitialized(null);
      return;
    }
    IsRemoteInitialized(connection, database)
      .then(setInitialized)
      .catch(() => setInitialized(null));
  }, [connection, database]);

  useEffect(refreshStatus, [refreshStatus]);

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">Remote Sync</h1>
      </div>
      <p className="remote-hint">
        Push or pull a database's snapshot history to a Git remote (e.g. GitHub) via Git LFS — the same sync
        `dbhelm remote` does from the terminal.
      </p>

      {connections.length === 0 ? (
        <EmptyState icon={<Cloud size={32} />} title="No connections yet" description="Add a connection to sync its snapshot history." />
      ) : (
        <>
          <div className="scope-picker">
            <Select
              value={connection}
              onChange={setConnection}
              options={connections.map((c) => ({ value: c.name, label: c.name }))}
            />
            <Select
              value={database}
              onChange={setDatabase}
              options={databases.map((d) => ({ value: d, label: d }))}
              placeholder="Select a database"
              disabled={databases.length === 0}
            />
          </div>

          {connection && database && initialized === false && (
            <SetupPanel connection={connection} database={database} onDone={refreshStatus} />
          )}
          {connection && database && initialized === true && (
            <SyncPanel connection={connection} database={database} />
          )}
        </>
      )}
    </div>
  );
}

function SetupPanel({ connection, database, onDone }: { connection: string; database: string; onDone: () => void }) {
  const [mode, setMode] = useState<"init" | "clone">("init");
  const [url, setUrl] = useState("");
  const [remoteName, setRemoteName] = useState("origin");
  const [branch, setBranch] = useState("main");
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  async function submit() {
    setBusy(true);
    try {
      if (mode === "init") {
        await InitRemote(connection, database, url, remoteName);
        toast.push("success", "Git + Git LFS initialized");
      } else {
        if (!url.trim()) {
          toast.push("error", "A remote URL is required to clone");
          return;
        }
        await CloneRemote(connection, database, url, branch);
        toast.push("success", "Cloned");
      }
      onDone();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card className="remote-setup-card">
      <div className="remote-mode-toggle">
        <Button variant={mode === "init" ? "primary" : "ghost"} onClick={() => setMode("init")}>
          Initialize new
        </Button>
        <Button variant={mode === "clone" ? "primary" : "ghost"} onClick={() => setMode("clone")}>
          Clone existing
        </Button>
      </div>

      {mode === "init" && (
        <>
          <p className="remote-hint">Sets up this database's local snapshot history as a Git + Git LFS repository.</p>
          <div className="field">
            <label className="field-label">Remote URL (optional)</label>
            <Input placeholder="git@github.com:me/myapp-snapshots.git" value={url} onChange={(e) => setUrl(e.target.value)} />
          </div>
          {url && (
            <div className="field">
              <label className="field-label">Remote name</label>
              <Input value={remoteName} onChange={(e) => setRemoteName(e.target.value)} />
            </div>
          )}
        </>
      )}

      {mode === "clone" && (
        <>
          <p className="remote-hint">
            Pulls in an existing remote's snapshot history — the local scope must be empty (it is, for a database with no
            snapshots yet).
          </p>
          <div className="field">
            <label className="field-label">Remote URL</label>
            <Input placeholder="git@github.com:me/myapp-snapshots.git" value={url} onChange={(e) => setUrl(e.target.value)} />
          </div>
          <div className="field">
            <label className="field-label">Branch</label>
            <Input value={branch} onChange={(e) => setBranch(e.target.value)} />
          </div>
        </>
      )}

      <Button onClick={submit} disabled={busy || (mode === "clone" && !url.trim())}>
        {busy ? "Working..." : mode === "init" ? "Initialize" : "Clone"}
      </Button>
    </Card>
  );
}

function SyncPanel({ connection, database }: { connection: string; database: string }) {
  const [pushRemote, setPushRemote] = useState("origin");
  const [pushBranch, setPushBranch] = useState("main");
  const [message, setMessage] = useState("");
  const [pullRemote, setPullRemote] = useState("origin");
  const [pullBranch, setPullBranch] = useState("main");
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  async function push() {
    setBusy(true);
    try {
      await PushRemote(connection, database, pushRemote, pushBranch, message);
      toast.push("success", "Pushed");
      setMessage("");
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setBusy(false);
    }
  }

  async function pull() {
    setBusy(true);
    try {
      await PullRemote(connection, database, pullRemote, pullBranch);
      toast.push("success", "Pulled");
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="remote-sync-panels">
      <Card className="remote-sync-card">
        <div className="remote-sync-card-title">
          <Upload size={14} /> Push
        </div>
        <div className="field">
          <label className="field-label">Remote</label>
          <Input value={pushRemote} onChange={(e) => setPushRemote(e.target.value)} />
        </div>
        <div className="field">
          <label className="field-label">Branch</label>
          <Input value={pushBranch} onChange={(e) => setPushBranch(e.target.value)} />
        </div>
        <div className="field">
          <label className="field-label">Commit message (optional)</label>
          <Input placeholder="dbhelm sync" value={message} onChange={(e) => setMessage(e.target.value)} />
        </div>
        <Button onClick={push} disabled={busy}>
          {busy ? "Working..." : "Push"}
        </Button>
      </Card>

      <Card className="remote-sync-card">
        <div className="remote-sync-card-title">
          <Download size={14} /> Pull
        </div>
        <div className="field">
          <label className="field-label">Remote</label>
          <Input value={pullRemote} onChange={(e) => setPullRemote(e.target.value)} />
        </div>
        <div className="field">
          <label className="field-label">Branch</label>
          <Input value={pullBranch} onChange={(e) => setPullBranch(e.target.value)} />
        </div>
        <Button onClick={pull} disabled={busy}>
          {busy ? "Working..." : "Pull"}
        </Button>
      </Card>

      <div className="remote-sync-hint">
        <GitBranch size={13} /> Already initialized for this database.
      </div>
    </div>
  );
}
