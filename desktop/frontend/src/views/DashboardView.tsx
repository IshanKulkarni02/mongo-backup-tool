import { useCallback, useEffect, useState } from "react";
import { Plus, RefreshCw, Trash2, LayoutDashboard } from "lucide-react";
import {
  ListSavedQueries,
  DeleteSavedQuery,
  RunSavedQuery,
  ListWidgets,
  SaveWidget,
  DeleteWidget,
} from "../../wailsjs/go/main/App";
import { dashboard, engine } from "../../wailsjs/go/models";
import {
  ResponsiveContainer,
  BarChart,
  Bar,
  LineChart,
  Line,
  ScatterChart,
  Scatter,
  PieChart,
  Pie,
  Cell as PieCell,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
} from "recharts";
import { Button } from "../components/Button";
import { Card } from "../components/Card";
import { Modal } from "../components/Modal";
import { Input } from "../components/Input";
import { EmptyState } from "../components/EmptyState";
import { Skeleton } from "../components/Skeleton";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { useToast } from "../components/Toast";
import "./DashboardView.css";

const CHART_COLORS = ["#0066ff", "#1f8a4c", "#d33a2c", "#9a6b00", "#6b46c1", "#0e7490"];

type ChartType = "bar" | "line" | "scatter" | "pie";

function cellToNumber(cell: engine.Cell | undefined): number {
  if (!cell) return 0;
  if (typeof cell.raw === "number") return cell.raw;
  const n = Number(cell.display);
  return Number.isFinite(n) ? n : 0;
}

function cellToLabel(cell: engine.Cell | undefined): string {
  return cell?.display ?? "";
}

function toChartData(result: engine.SQLResult, xColumn: string, yColumns: string[]) {
  return result.rows.map((row) => {
    const point: Record<string, string | number> = { [xColumn]: cellToLabel(row[xColumn]) };
    for (const y of yColumns) {
      point[y] = cellToNumber(row[y]);
    }
    return point;
  });
}

export function DashboardView() {
  const [queries, setQueries] = useState<dashboard.SavedQuery[]>([]);
  const [widgets, setWidgets] = useState<dashboard.Widget[]>([]);
  const [showNewWidget, setShowNewWidget] = useState(false);
  const [deleteQueryTarget, setDeleteQueryTarget] = useState<string | null>(null);
  const toast = useToast();

  const load = useCallback(() => {
    ListSavedQueries().then(setQueries).catch((e) => toast.push("error", String(e)));
    ListWidgets().then(setWidgets).catch((e) => toast.push("error", String(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(load, [load]);

  async function handleDeleteQuery() {
    if (!deleteQueryTarget) return;
    try {
      await DeleteSavedQuery(deleteQueryTarget);
      toast.push("success", "Saved query deleted");
      load();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setDeleteQueryTarget(null);
    }
  }

  async function handleDeleteWidget(id: string) {
    try {
      await DeleteWidget(id);
      load();
    } catch (e) {
      toast.push("error", String(e));
    }
  }

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">Dashboard</h1>
        <div className="view-header-actions">
          <Button variant="ghost" onClick={load}>
            <RefreshCw size={14} /> Refresh
          </Button>
          <Button onClick={() => setShowNewWidget(true)} disabled={queries.length === 0}>
            <Plus size={14} /> New widget
          </Button>
        </div>
      </div>

      {queries.length === 0 && (
        <EmptyState
          icon={<LayoutDashboard size={32} />}
          title="No saved queries yet"
          description="Save a query from the Query view, then come back here to chart it."
        />
      )}

      {queries.length > 0 && widgets.length === 0 && (
        <EmptyState icon={<LayoutDashboard size={32} />} title="No widgets yet" description="Add a widget to chart one of your saved queries." />
      )}

      <div className="dashboard-grid">
        {widgets.map((w) => (
          <WidgetCard key={w.id} widget={w} onDelete={() => handleDeleteWidget(w.id)} />
        ))}
      </div>

      {queries.length > 0 && (
        <>
          <h2 className="dashboard-section-title">Saved queries</h2>
          <div className="saved-query-list">
            {queries.map((q) => (
              <Card key={q.id} className="saved-query-row">
                <div>
                  <div className="saved-query-name">{q.name}</div>
                  <div className="saved-query-sql mono">{q.sqlText}</div>
                </div>
                <Button variant="danger" onClick={() => setDeleteQueryTarget(q.id)}>
                  <Trash2 size={14} />
                </Button>
              </Card>
            ))}
          </div>
        </>
      )}

      {showNewWidget && (
        <NewWidgetModal
          queries={queries}
          onClose={() => setShowNewWidget(false)}
          onCreated={() => {
            setShowNewWidget(false);
            load();
          }}
        />
      )}

      {deleteQueryTarget && (
        <ConfirmDialog
          title="Delete saved query"
          message="Delete this saved query? Any dashboard widgets built on it will be removed too."
          confirmLabel="Delete"
          danger
          onConfirm={handleDeleteQuery}
          onCancel={() => setDeleteQueryTarget(null)}
        />
      )}
    </div>
  );
}

function WidgetCard({ widget, onDelete }: { widget: dashboard.Widget; onDelete: () => void }) {
  const [result, setResult] = useState<engine.SQLResult | null>(null);
  const [error, setError] = useState("");

  const load = useCallback(() => {
    RunSavedQuery(widget.queryId)
      .then(setResult)
      .catch((e) => setError(String(e)));
  }, [widget.queryId]);

  useEffect(load, [load]);

  const data = result ? toChartData(result, widget.xColumn, widget.yColumns) : [];

  return (
    <Card className="widget-card">
      <div className="widget-header">
        <span>{widget.title}</span>
        <div className="widget-actions">
          <button className="icon-btn" onClick={load} title="Refresh">
            <RefreshCw size={13} />
          </button>
          <button className="icon-btn danger" onClick={onDelete} title="Remove widget">
            <Trash2 size={13} />
          </button>
        </div>
      </div>
      {error && <div className="query-error">{error}</div>}
      {!error && !result && <Skeleton height={220} />}
      {!error && result && data.length === 0 && <EmptyState icon={<LayoutDashboard size={24} />} title="No data" />}
      {!error && result && data.length > 0 && (
        <div className="widget-chart">
          <ResponsiveContainer width="100%" height={240}>
            {renderChart(widget, data)}
          </ResponsiveContainer>
        </div>
      )}
    </Card>
  );
}

function renderChart(widget: dashboard.Widget, data: Record<string, string | number>[]) {
  switch (widget.chartType) {
    case "line":
      return (
        <LineChart data={data}>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey={widget.xColumn} />
          <YAxis />
          <Tooltip />
          <Legend />
          {widget.yColumns.map((y, i) => (
            <Line key={y} type="monotone" dataKey={y} stroke={CHART_COLORS[i % CHART_COLORS.length]} />
          ))}
        </LineChart>
      );
    case "scatter":
      return (
        <ScatterChart>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey={widget.xColumn} type="category" name={widget.xColumn} />
          <YAxis dataKey={widget.yColumns[0]} name={widget.yColumns[0]} />
          <Tooltip cursor={{ strokeDasharray: "3 3" }} />
          <Legend />
          <Scatter data={data} fill={CHART_COLORS[0]} />
        </ScatterChart>
      );
    case "pie": {
      const y = widget.yColumns[0];
      const pieData = data.map((d) => ({ name: String(d[widget.xColumn]), value: Number(d[y]) || 0 }));
      return (
        <PieChart>
          <Tooltip />
          <Legend />
          <Pie data={pieData} dataKey="value" nameKey="name" outerRadius={80} label>
            {pieData.map((_, i) => (
              <PieCell key={i} fill={CHART_COLORS[i % CHART_COLORS.length]} />
            ))}
          </Pie>
        </PieChart>
      );
    }
    case "bar":
    default:
      return (
        <BarChart data={data}>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey={widget.xColumn} />
          <YAxis />
          <Tooltip />
          <Legend />
          {widget.yColumns.map((y, i) => (
            <Bar key={y} dataKey={y} fill={CHART_COLORS[i % CHART_COLORS.length]} />
          ))}
        </BarChart>
      );
  }
}

function NewWidgetModal({
  queries,
  onClose,
  onCreated,
}: {
  queries: dashboard.SavedQuery[];
  onClose: () => void;
  onCreated: () => void;
}) {
  const [title, setTitle] = useState("");
  const [queryId, setQueryId] = useState(queries[0]?.id ?? "");
  const [chartType, setChartType] = useState<ChartType>("bar");
  const [columns, setColumns] = useState<string[]>([]);
  const [xColumn, setXColumn] = useState("");
  const [yColumns, setYColumns] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const toast = useToast();

  useEffect(() => {
    if (!queryId) return;
    RunSavedQuery(queryId)
      .then((r) => {
        setColumns(r.columns);
        setXColumn(r.columns[0] ?? "");
        setYColumns(r.columns[1] ? [r.columns[1]] : []);
      })
      .catch((e) => setError(String(e)));
  }, [queryId]);

  function toggleY(col: string) {
    setYColumns((cols) => (cols.includes(col) ? cols.filter((c) => c !== col) : [...cols, col]));
  }

  async function submit() {
    if (!title || !queryId || !xColumn || yColumns.length === 0) {
      setError("Title, x column, and at least one y column are required");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await SaveWidget("", title, queryId, chartType, xColumn, yColumns);
      toast.push("success", "Widget added");
      onCreated();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title="New widget"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy}>
            {busy ? "Saving..." : "Add widget"}
          </Button>
        </>
      }
    >
      <Input label="Title" value={title} onChange={(e) => setTitle(e.target.value)} autoFocus />
      <div className="field">
        <label className="field-label">Saved query</label>
        <select className="input" value={queryId} onChange={(e) => setQueryId(e.target.value)}>
          {queries.map((q) => (
            <option key={q.id} value={q.id}>
              {q.name}
            </option>
          ))}
        </select>
      </div>
      <div className="field">
        <label className="field-label">Chart type</label>
        <select className="input" value={chartType} onChange={(e) => setChartType(e.target.value as ChartType)}>
          <option value="bar">Bar</option>
          <option value="line">Line</option>
          <option value="scatter">Scatter</option>
          <option value="pie">Pie</option>
        </select>
      </div>
      {columns.length > 0 && (
        <>
          <div className="field">
            <label className="field-label">X column</label>
            <select className="input" value={xColumn} onChange={(e) => setXColumn(e.target.value)}>
              {columns.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label className="field-label">Y column(s)</label>
            <div className="y-column-list">
              {columns.map((c) => (
                <label key={c} className="checkbox-row">
                  <input type="checkbox" checked={yColumns.includes(c)} onChange={() => toggleY(c)} />
                  {c}
                </label>
              ))}
            </div>
          </div>
        </>
      )}
      {error && <div className="query-error">{error}</div>}
    </Modal>
  );
}
