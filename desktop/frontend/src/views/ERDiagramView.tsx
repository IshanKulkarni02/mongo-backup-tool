import { useEffect, useMemo, useState } from "react";
import { ReactFlow, Background, Controls, type Node, type Edge } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { Database, Waypoints } from "lucide-react";
import { ListConnections, TestConnection, ListTables, GetTableSchema } from "../../wailsjs/go/main/App";
import { main, engine } from "../../wailsjs/go/models";
import { EmptyState } from "../components/EmptyState";
import { Skeleton } from "../components/Skeleton";
import "./ERDiagramView.css";

interface TableWithSchema {
  name: string;
  schema: engine.TableSchema;
}

const NODE_WIDTH = 220;
const NODE_GAP_X = 60;
const NODE_GAP_Y = 60;
const ROW_HEADER_HEIGHT = 32;
const ROW_HEIGHT = 20;
const MAX_PER_ROW = 4;

// buildGraph groups tables into connected components via their FK
// relationships (undirected — a reference either way links two tables),
// then lays each component out in rows of MAX_PER_ROW, stacking
// components vertically. No layout library (dagre/elkjs) — same hand-
// rolled approach as RulesView's flowchart, just grouped instead of
// depth-nested since an ER graph doesn't have a natural "depth" the way a
// conditions tree does.
function buildGraph(tables: TableWithSchema[]): { nodes: Node[]; edges: Edge[] } {
  const byName = new Map(tables.map((t) => [t.name, t]));
  const adjacency = new Map<string, Set<string>>();
  tables.forEach((t) => adjacency.set(t.name, new Set()));
  for (const t of tables) {
    for (const fk of t.schema.foreignKeys) {
      if (!byName.has(fk.refTable)) continue;
      adjacency.get(t.name)!.add(fk.refTable);
      adjacency.get(fk.refTable)!.add(t.name);
    }
  }

  const visited = new Set<string>();
  const components: string[][] = [];
  for (const t of tables) {
    if (visited.has(t.name)) continue;
    const component: string[] = [];
    const queue = [t.name];
    visited.add(t.name);
    while (queue.length > 0) {
      const current = queue.shift()!;
      component.push(current);
      for (const neighbor of adjacency.get(current) ?? []) {
        if (!visited.has(neighbor)) {
          visited.add(neighbor);
          queue.push(neighbor);
        }
      }
    }
    components.push(component);
  }

  const nodes: Node[] = [];
  let y = 0;
  for (const component of components) {
    let col = 0;
    let rowMaxHeight = 0;
    let rowY = y;
    for (const name of component) {
      const table = byName.get(name)!;
      const height = ROW_HEADER_HEIGHT + table.schema.columns.length * ROW_HEIGHT;
      rowMaxHeight = Math.max(rowMaxHeight, height);
      nodes.push({
        id: name,
        data: { label: <TableNodeLabel table={table} /> },
        position: { x: col * (NODE_WIDTH + NODE_GAP_X), y: rowY },
        style: tableNodeStyle,
      });
      col++;
      if (col >= MAX_PER_ROW) {
        col = 0;
        y = rowY + rowMaxHeight + NODE_GAP_Y;
        rowY = y;
        rowMaxHeight = 0;
      }
    }
    y = rowY + rowMaxHeight + NODE_GAP_Y;
  }

  const edges: Edge[] = [];
  for (const t of tables) {
    for (const fk of t.schema.foreignKeys) {
      if (!byName.has(fk.refTable) || fk.refTable === t.name) continue;
      edges.push({
        id: `e-${t.name}.${fk.column}-${fk.refTable}`,
        source: t.name,
        target: fk.refTable,
        label: fk.column,
        style: { stroke: "var(--color-border-strong)" },
      });
    }
  }

  return { nodes, edges };
}

const tableNodeStyle: React.CSSProperties = {
  background: "var(--color-surface)",
  border: "1px solid var(--color-border-strong)",
  borderRadius: 8,
  padding: 0,
  width: NODE_WIDTH,
  fontSize: 12,
  textAlign: "left",
};

function TableNodeLabel({ table }: { table: TableWithSchema }) {
  return (
    <div>
      <div
        style={{
          fontWeight: 600,
          padding: "6px 10px",
          borderBottom: "1px solid var(--color-border)",
          color: "var(--color-accent)",
        }}
      >
        {table.name}
      </div>
      <div style={{ padding: "4px 0" }}>
        {table.schema.columns.map((c) => (
          <div
            key={c.name}
            style={{
              display: "flex",
              justifyContent: "space-between",
              gap: 8,
              padding: "1px 10px",
              color: c.isPk ? "var(--color-text)" : "var(--color-text-muted)",
              fontWeight: c.isPk ? 600 : 400,
            }}
          >
            <span>
              {c.isPk ? "PK " : ""}
              {c.name}
            </span>
            <span>{c.dataType}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

export function ERDiagramView() {
  const [connections, setConnections] = useState<main.ConnectionInfo[]>([]);
  const [connection, setConnection] = useState("");
  const [databases, setDatabases] = useState<string[]>([]);
  const [database, setDatabase] = useState("");
  const [tables, setTables] = useState<TableWithSchema[] | null>(null);
  const [loading, setLoading] = useState(false);

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
    TestConnection(connection).then((dbs) => {
      setDatabases(dbs);
      if (dbs.length > 0) setDatabase(dbs[0]);
    });
  }, [connection]);

  useEffect(() => {
    if (!connection || !database) {
      setTables(null);
      return;
    }
    setLoading(true);
    ListTables(connection, database)
      .then(async (infos) => {
        const withSchema = await Promise.all(
          infos.map(async (info) => ({ name: info.name, schema: await GetTableSchema(connection, database, info.name) }))
        );
        setTables(withSchema);
      })
      .catch(() => setTables([]))
      .finally(() => setLoading(false));
  }, [connection, database]);

  const graph = useMemo(() => (tables && tables.length > 0 ? buildGraph(tables) : null), [tables]);

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">ER Diagram</h1>
      </div>

      {connections.length === 0 ? (
        <EmptyState icon={<Database size={32} />} title="No SQL connections yet" description="Add a PostgreSQL, MySQL, or SQLite connection to diagram its schema here." />
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

          {loading && <Skeleton height={320} />}
          {!loading && tables && tables.length === 0 && <EmptyState icon={<Waypoints size={28} />} title="No tables in this database" />}
          {!loading && graph && (
            <div className="erdiagram-canvas">
              <ReactFlow nodes={graph.nodes} edges={graph.edges} fitView proOptions={{ hideAttribution: true }}>
                <Background />
                <Controls />
              </ReactFlow>
            </div>
          )}
        </>
      )}
    </div>
  );
}
