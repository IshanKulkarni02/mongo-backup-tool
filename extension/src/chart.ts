/**
 * The Chart: a read-only schema browser and data grid. Everything here is pure (no vscode, no
 * network): it builds the read queries the operator sends to the broker, and shapes what comes
 * back into something a grid can show. The broker still runs every query through the read-only
 * guard, so a filter someone types can never write.
 */

export interface GridCell { v: string; t: "null" | "num" | "text" | "json" | "bin" | "date" | "bool" }
export interface Grid { columns: string[]; rows: GridCell[][] }

export const PAGE_SIZE = 50;

export function quoteIdent(engine: string, name: string): string {
  return engine === "mysql" ? "`" + name.replace(/`/g, "``") + "`" : '"' + name.replace(/"/g, '""') + '"';
}

/** Postgres tables must be schema-qualified (its "database" is the schema); MySQL selects the database itself. */
export function tableRef(engine: string, database: string, table: string): string {
  return engine === "postgres" ? `${quoteIdent(engine, database)}.${quoteIdent(engine, table)}` : quoteIdent(engine, table);
}

/** One page of a table, asking for one row more than shown so "has more" needs no count query. */
export function pageSql(engine: string, database: string, table: string, filter: string, page: number): string {
  const where = filter.trim() ? ` WHERE ${filter.trim().replace(/\s*;+\s*$/, "")}` : "";
  return `SELECT * FROM ${tableRef(engine, database, table)}${where} LIMIT ${PAGE_SIZE + 1} OFFSET ${Math.max(0, page) * PAGE_SIZE}`;
}

const TYPE_MAP: Record<string, GridCell["t"]> = { null: "null", number: "num", string: "text", json: "json", binary: "bin", date: "date", bool: "bool" };

/** broker SQLResult -> grid. Extra rows beyond a page (the "has more" probe) are dropped by the caller. */
export function sqlToGrid(result: { columns: string[]; rows: Record<string, { type: string; display: string }>[] }): Grid {
  return {
    columns: result.columns,
    rows: result.rows.map((r) => result.columns.map((c) => ({ v: r[c]?.display ?? "", t: TYPE_MAP[r[c]?.type] ?? "text" }))),
  };
}

function unwrap(v: unknown): unknown {
  if (v && typeof v === "object" && !Array.isArray(v)) {
    const keys = Object.keys(v as object);
    if (keys.length === 1) {
      const k = keys[0];
      const inner = (v as Record<string, unknown>)[k];
      if (k === "$oid" || k === "$numberLong" || k === "$numberInt" || k === "$numberDouble" || k === "$numberDecimal") return String(inner);
      if (k === "$date") return typeof inner === "object" ? JSON.stringify(inner) : String(inner);
    }
  }
  return v;
}

/** Extended-JSON documents -> grid: columns are the union of top-level keys (first 30, in order seen). */
export function docsToGrid(docs: string[]): Grid {
  const parsed: Record<string, unknown>[] = [];
  for (const d of docs) {
    try { const o = JSON.parse(d); if (o && typeof o === "object") parsed.push(o); } catch { /* skip an unreadable document */ }
  }
  const columns: string[] = [];
  for (const o of parsed) for (const k of Object.keys(o)) if (!columns.includes(k) && columns.length < 30) columns.push(k);
  const rows = parsed.map((o) => columns.map((c): GridCell => {
    if (!(c in o)) return { v: "", t: "null" };
    const raw = unwrap(o[c]);
    if (raw === null) return { v: "null", t: "null" };
    if (typeof raw === "number") return { v: String(raw), t: "num" };
    if (typeof raw === "boolean") return { v: String(raw), t: "bool" };
    if (typeof raw === "object") return { v: JSON.stringify(raw), t: "json" };
    return { v: String(raw), t: "text" };
  }));
  return { columns, rows };
}
