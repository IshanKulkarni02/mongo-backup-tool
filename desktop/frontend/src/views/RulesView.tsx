import { useMemo, useState } from "react";
import { ReactFlow, Background, Controls, type Node, type Edge } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { Workflow } from "lucide-react";
import { JsonTree } from "../components/JsonTree";
import { EmptyState } from "../components/EmptyState";
import "./RulesView.css";

const SAMPLE_RULE = `{
  "conditions": {
    "all": [
      { "fact": "hoursWorked", "operator": "greaterThan", "value": 10 },
      {
        "any": [
          { "fact": "dayType", "operator": "equal", "value": "publicHoliday" },
          { "fact": "isWeekend", "operator": "equal", "value": true }
        ]
      }
    ]
  },
  "event": {
    "type": "overtimeMultiplier",
    "params": { "multiplier": 2.0 }
  }
}`;

interface ConditionGroup {
  all?: unknown[];
  any?: unknown[];
  fact?: string;
  operator?: string;
  value?: unknown;
}

function buildGraph(rule: { conditions: ConditionGroup; event?: { type?: string; params?: unknown } }): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = [];
  const edges: Edge[] = [];
  const posById: Record<string, number> = {};
  let idCounter = 0;
  let leafCounter = 0;
  const LEAF_GAP = 260;
  const ROW_HEIGHT = 110;

  // Neutral, theme-adaptive backgrounds with a semantic-colored border
  // (rather than the light pastel fills a hardcoded hex gives you) — the
  // pastel tints looked right on a white canvas but would either vanish or
  // clash against a dark one, and React Flow's default node text color
  // isn't overridden here, so it needs a background that stays legible in
  // both themes.
  const groupStyle = { background: "var(--color-surface)", border: "1px solid var(--color-accent)", borderRadius: 8, padding: 8, fontWeight: 700, color: "var(--color-text)" };
  const leafStyle = { background: "var(--color-surface)", border: "1px solid var(--color-success)", borderRadius: 8, padding: 8, fontSize: 12, color: "var(--color-text)" };
  const eventStyle = { background: "var(--color-surface)", border: "1px solid var(--color-danger)", borderRadius: 8, padding: 8, fontWeight: 700, color: "var(--color-text)" };

  function layout(cond: ConditionGroup, depth: number): string {
    const id = `n${idCounter++}`;
    if (cond.all || cond.any) {
      const kind = cond.all ? "ALL" : "ANY";
      const children = (cond.all ?? cond.any ?? []) as ConditionGroup[];
      const childIds = children.map((c) => layout(c, depth + 1));
      const xs = childIds.map((cid) => posById[cid]);
      const x = xs.length > 0 ? xs.reduce((a, b) => a + b, 0) / xs.length : leafCounter * LEAF_GAP;
      posById[id] = x;
      nodes.push({ id, data: { label: kind }, position: { x, y: depth * ROW_HEIGHT }, style: groupStyle });
      childIds.forEach((cid) => edges.push({ id: `e-${id}-${cid}`, source: id, target: cid }));
      return id;
    }
    const label = `${cond.fact ?? "?"} ${cond.operator ?? ""} ${JSON.stringify(cond.value)}`;
    const x = leafCounter * LEAF_GAP;
    leafCounter++;
    posById[id] = x;
    nodes.push({ id, data: { label }, position: { x, y: depth * ROW_HEIGHT }, style: leafStyle });
    return id;
  }

  const rootId = layout(rule.conditions, 0);
  const maxDepth = Math.max(1, ...nodes.map((n) => n.position.y / ROW_HEIGHT)) + 1;
  const eventId = `n${idCounter++}`;
  nodes.push({
    id: eventId,
    data: { label: `THEN ${rule.event?.type ?? "(event)"}` },
    position: { x: posById[rootId], y: maxDepth * ROW_HEIGHT },
    style: eventStyle,
  });
  edges.push({ id: `e-${rootId}-${eventId}`, source: rootId, target: eventId });

  return { nodes, edges };
}

export function RulesView() {
  const [text, setText] = useState(SAMPLE_RULE);
  const [parsed, setParsed] = useState<unknown>(() => JSON.parse(SAMPLE_RULE));
  const [error, setError] = useState("");

  function handleChange(value: string) {
    setText(value);
    try {
      setParsed(JSON.parse(value));
      setError("");
    } catch (e) {
      setError(String(e));
    }
  }

  const graph = useMemo(() => {
    if (!parsed || typeof parsed !== "object" || !("conditions" in (parsed as object))) return null;
    try {
      return buildGraph(parsed as { conditions: ConditionGroup; event?: { type?: string } });
    } catch {
      return null;
    }
  }, [parsed]);

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">Rules Visualizer</h1>
      </div>
      <p className="rules-hint">
        Paste a json-rules-engine ruleset (a <span className="mono">conditions</span> tree of <span className="mono">all</span>/
        <span className="mono">any</span> groups plus an <span className="mono">event</span>) to see it as a tree and a flowchart —
        useful for debugging why a rule like an overtime multiplier didn't fire the way you expected.
      </p>

      <textarea className="input rules-editor mono" rows={12} value={text} onChange={(e) => handleChange(e.target.value)} />
      {error && <div className="query-error">{error}</div>}

      <div className="rules-panels">
        <div className="rules-panel">
          <div className="rules-panel-title">Tree</div>
          <JsonTree json={text} />
        </div>
        <div className="rules-panel rules-flow-panel">
          <div className="rules-panel-title">Flowchart</div>
          {graph ? (
            <div className="rules-flow-canvas">
              <ReactFlow nodes={graph.nodes} edges={graph.edges} fitView proOptions={{ hideAttribution: true }}>
                <Background />
                <Controls />
              </ReactFlow>
            </div>
          ) : (
            <EmptyState icon={<Workflow size={28} />} title="No conditions tree to visualize" description="Paste a ruleset with a conditions field above." />
          )}
        </div>
      </div>
    </div>
  );
}
