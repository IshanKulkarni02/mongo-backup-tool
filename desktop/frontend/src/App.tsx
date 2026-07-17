import { Suspense, lazy, useCallback, useEffect, useState, type PropsWithChildren } from "react";
import {
  Database,
  GitBranch,
  Archive,
  Table2,
  Grid3x3,
  Terminal,
  Workflow,
  Sparkles,
  LayoutDashboard,
  GitCompare,
  GitCompareArrows,
  Map,
  Radio,
} from "lucide-react";
import "./App.css";
import { ToastProvider, useToast } from "./components/Toast";
import { useJobUpdates, Job } from "./hooks/useJobs";
import { DependencyModal } from "./components/DependencyModal";
import { Skeleton } from "./components/Skeleton";
import { ConnectionsView } from "./views/ConnectionsView";
import { SnapshotsView } from "./views/SnapshotsView";
import { BackupsView } from "./views/BackupsView";
import { BrowserView } from "./views/BrowserView";
import { TableView } from "./views/TableView";
import { VectorToolView } from "./views/VectorToolView";
import { WebhookView } from "./views/WebhookView";
import { ListConnections } from "../wailsjs/go/main/App";

// Lazy-loaded: each of these pulls in a heavy standalone dependency
// (CodeMirror, Recharts, React Flow, Leaflet) that only needs to be
// fetched/parsed once the user actually opens that view, not on first
// paint — the production bundle was ~1.5MB before this split.
const QueryView = lazy(() => import("./views/QueryView").then((m) => ({ default: m.QueryView })));
const PipelineView = lazy(() => import("./views/PipelineView").then((m) => ({ default: m.PipelineView })));
const DashboardView = lazy(() => import("./views/DashboardView").then((m) => ({ default: m.DashboardView })));
const SchemaDiffView = lazy(() => import("./views/SchemaDiffView").then((m) => ({ default: m.SchemaDiffView })));
const AISettingsView = lazy(() => import("./views/AISettingsView").then((m) => ({ default: m.AISettingsView })));
const GeoView = lazy(() => import("./views/GeoView").then((m) => ({ default: m.GeoView })));
const RulesView = lazy(() => import("./views/RulesView").then((m) => ({ default: m.RulesView })));

function ViewSuspense({ children }: PropsWithChildren) {
  return <Suspense fallback={<Skeleton height={320} />}>{children}</Suspense>;
}

type View =
  | "connections"
  | "browser"
  | "tables"
  | "query"
  | "pipeline"
  | "dashboard"
  | "schemadiff"
  | "ai"
  | "vector"
  | "geo"
  | "rules"
  | "webhook"
  | "snapshots"
  | "backups";

// requires gates a nav item on at least one saved connection whose actual
// engine.Caps report that capability — not on the connection's engine
// *name*, so a future engine that sets, say, Caps.SQL without being named
// "postgres"/"mysql"/"sqlite" is handled automatically. Items with no
// `requires` are engine-agnostic tools (AI, Vector/Geo/Rules, Webhook,
// Dashboard) and always show.
type CapKey = "sql" | "documents" | "aggregation" | "snapshots";

const NAV: { id: View; label: string; icon: typeof Database; requires?: CapKey }[] = [
  { id: "connections", label: "Connections", icon: Database },
  { id: "browser", label: "Browser", icon: Table2, requires: "documents" },
  { id: "tables", label: "Tables", icon: Grid3x3, requires: "sql" },
  { id: "query", label: "Query", icon: Terminal, requires: "sql" },
  { id: "pipeline", label: "Pipeline", icon: Workflow, requires: "aggregation" },
  { id: "dashboard", label: "Dashboard", icon: LayoutDashboard },
  { id: "schemadiff", label: "Schema Diff", icon: GitCompare, requires: "sql" },
  { id: "ai", label: "AI", icon: Sparkles },
  { id: "vector", label: "Vector Compare", icon: GitCompareArrows },
  { id: "geo", label: "Geo Viewer", icon: Map },
  { id: "rules", label: "Rules Visualizer", icon: Workflow },
  { id: "webhook", label: "Webhook Listener", icon: Radio },
  { id: "snapshots", label: "Snapshots", icon: GitBranch, requires: "snapshots" },
  { id: "backups", label: "Backups", icon: Archive, requires: "snapshots" },
];

// Job types whose completion is already surfaced inline by their own view
// (e.g. QueryView shows rows-affected / query errors next to the editor,
// AISettingsView shows install/pull results directly) — a redundant
// global toast for these would just be noise.
const SILENT_JOB_TYPES = new Set(["sql-query", "ollama-install", "ollama-pull"]);

const JOB_LABELS: Record<string, string> = {
  "snapshot-create": "Snapshot created",
  "snapshot-restore": "Snapshot restored",
  "backup-create": "Backup created",
  "backup-restore": "Backup restored",
};

function AppShell() {
  const [view, setView] = useState<View>("connections");
  const [depsResolved, setDepsResolved] = useState(false);
  const [availableCaps, setAvailableCaps] = useState<Set<CapKey>>(
    new Set(["sql", "documents", "aggregation", "snapshots"])
  );
  const toast = useToast();

  const onJobUpdate = useCallback(
    (job: Job) => {
      if (SILENT_JOB_TYPES.has(job.type)) return;
      if (job.status === "done") {
        toast.push("success", JOB_LABELS[job.type] ?? "Done");
      } else if (job.status === "failed") {
        toast.push("error", job.message ?? "Something went wrong");
      }
    },
    [toast]
  );
  useJobUpdates(onJobUpdate);

  // Refetched on every view change (not just on mount) so adding/removing
  // a connection on the Connections screen is reflected in the nav as
  // soon as the user navigates away from it.
  useEffect(() => {
    ListConnections()
      .then((conns) => {
        const caps = new Set<CapKey>();
        for (const c of conns) {
          if (c.capabilities?.sql) caps.add("sql");
          if (c.capabilities?.documents) caps.add("documents");
          if (c.capabilities?.aggregation) caps.add("aggregation");
          if (c.capabilities?.snapshots) caps.add("snapshots");
        }
        setAvailableCaps(caps);
      })
      .catch(() => {
        // Leave capability flags as they were — a transient list failure
        // shouldn't hide nav items that were visible a moment ago.
      });
  }, [view]);

  const visibleNav = NAV.filter((item) => !item.requires || availableCaps.has(item.requires));

  // If the currently active view just got hidden (its last connection was
  // removed), fall back to Connections rather than showing a blank pane.
  useEffect(() => {
    if (!visibleNav.some((item) => item.id === view)) {
      setView("connections");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visibleNav.length]);

  return (
    <div className="shell">
      <nav className="sidebar">
        <div className="sidebar-title">mongobak</div>
        {visibleNav.map(({ id, label, icon: Icon }) => (
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
        {view === "tables" && <TableView />}
        {view === "query" && (
          <ViewSuspense>
            <QueryView />
          </ViewSuspense>
        )}
        {view === "pipeline" && (
          <ViewSuspense>
            <PipelineView />
          </ViewSuspense>
        )}
        {view === "dashboard" && (
          <ViewSuspense>
            <DashboardView />
          </ViewSuspense>
        )}
        {view === "schemadiff" && (
          <ViewSuspense>
            <SchemaDiffView />
          </ViewSuspense>
        )}
        {view === "ai" && (
          <ViewSuspense>
            <AISettingsView />
          </ViewSuspense>
        )}
        {view === "vector" && <VectorToolView />}
        {view === "geo" && (
          <ViewSuspense>
            <GeoView />
          </ViewSuspense>
        )}
        {view === "rules" && (
          <ViewSuspense>
            <RulesView />
          </ViewSuspense>
        )}
        {view === "webhook" && <WebhookView />}
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
