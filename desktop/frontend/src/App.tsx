import { useCallback, useState } from "react";
import { Database, GitBranch, Archive, Table2 } from "lucide-react";
import "./App.css";
import { ToastProvider, useToast } from "./components/Toast";
import { useJobUpdates, Job } from "./hooks/useJobs";
import { DependencyModal } from "./components/DependencyModal";
import { ConnectionsView } from "./views/ConnectionsView";
import { SnapshotsView } from "./views/SnapshotsView";
import { BackupsView } from "./views/BackupsView";
import { BrowserView } from "./views/BrowserView";

type View = "connections" | "browser" | "snapshots" | "backups";

const NAV: { id: View; label: string; icon: typeof Database }[] = [
  { id: "connections", label: "Connections", icon: Database },
  { id: "browser", label: "Browser", icon: Table2 },
  { id: "snapshots", label: "Snapshots", icon: GitBranch },
  { id: "backups", label: "Backups", icon: Archive },
];

const JOB_LABELS: Record<string, string> = {
  "snapshot-create": "Snapshot created",
  "snapshot-restore": "Snapshot restored",
  "backup-create": "Backup created",
  "backup-restore": "Backup restored",
};

function AppShell() {
  const [view, setView] = useState<View>("connections");
  const [depsResolved, setDepsResolved] = useState(false);
  const toast = useToast();

  const onJobUpdate = useCallback(
    (job: Job) => {
      if (job.status === "done") {
        toast.push("success", JOB_LABELS[job.type] ?? "Done");
      } else if (job.status === "failed") {
        toast.push("error", job.message ?? "Something went wrong");
      }
    },
    [toast]
  );
  useJobUpdates(onJobUpdate);

  return (
    <div className="shell">
      <nav className="sidebar">
        <div className="sidebar-title">mongobak</div>
        {NAV.map(({ id, label, icon: Icon }) => (
          <button
            key={id}
            className={`nav-item ${view === id ? "active" : ""}`}
            onClick={() => setView(id)}
          >
            <Icon size={16} />
            {label}
          </button>
        ))}
      </nav>
      <main className="main">
        {view === "connections" && <ConnectionsView />}
        {view === "browser" && <BrowserView />}
        {view === "snapshots" && <SnapshotsView />}
        {view === "backups" && <BackupsView />}
      </main>
      {!depsResolved && <DependencyModal onResolved={() => setDepsResolved(true)} />}
    </div>
  );
}

export default function App() {
  return (
    <ToastProvider>
      <AppShell />
    </ToastProvider>
  );
}
