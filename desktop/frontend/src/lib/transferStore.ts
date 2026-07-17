// transferStore hands a grid cell's value to another view without
// prop-drilling a "current connection/selection" concept through every
// view (this app's views each own an independent connection picker — see
// App.tsx's nav-visibility comment — there's no single global "selected
// row" to read). A right-click on a cell writes here; the target view
// (Vector Compare, Geo Viewer) checks on mount and reacts to en-route
// updates the same module-scoped-listener way useJobs.ts fans out
// job:update.
export type Transfer = { kind: "vector"; slot: "a" | "b"; value: string } | { kind: "geojson"; value: string };

let pending: Transfer | null = null;
const listeners = new Set<(t: Transfer) => void>();

export function sendTransfer(t: Transfer) {
  pending = t;
  listeners.forEach((fn) => fn(t));
}

/** Reads and clears the pending transfer — call once on mount so a stale
 * value from a previous visit isn't silently reapplied. */
export function takePendingTransfer(kind: Transfer["kind"]): Transfer | null {
  if (pending && pending.kind === kind) {
    const t = pending;
    pending = null;
    return t;
  }
  return null;
}

/** Subscribes to transfers sent while already mounted (the target view is
 * open in another... well, this app has one visible view at a time, but a
 * subscription still covers "user right-clicks, toast says done, user
 * then navigates" without needing a mount-order dependency). */
export function subscribeTransfer(kind: Transfer["kind"], onTransfer: (t: Transfer) => void): () => void {
  const wrapped = (t: Transfer) => {
    if (t.kind === kind) onTransfer(t);
  };
  listeners.add(wrapped);
  return () => listeners.delete(wrapped);
}

// looksLikeVector reports whether a cell's display text is plausibly an
// embedding vector — a JSON array of numbers, pgvector's "[1,2,3]" text
// format (same shape), or a bare comma-separated numeric list.
export function looksLikeVector(text: string): boolean {
  const t = text.trim();
  if (!t) return false;
  const stripped = t.startsWith("[") && t.endsWith("]") ? t.slice(1, -1) : t;
  const parts = stripped.split(",").map((p) => p.trim());
  if (parts.length < 2) return false;
  return parts.every((p) => p !== "" && !isNaN(Number(p)));
}

// looksLikeGeoJSON reports whether a cell's display text parses as JSON
// with a "type" field GeoJSON defines — covers both a JSON/JSONB column's
// native content and a SQL text value (e.g. Postgres' ST_AsGeoJSON, which
// returns type text, not json).
export function looksLikeGeoJSON(text: string): boolean {
  const GEOJSON_TYPES = new Set([
    "Point",
    "MultiPoint",
    "LineString",
    "MultiLineString",
    "Polygon",
    "MultiPolygon",
    "GeometryCollection",
    "Feature",
    "FeatureCollection",
  ]);
  try {
    const parsed = JSON.parse(text);
    return !!parsed && typeof parsed === "object" && GEOJSON_TYPES.has(parsed.type);
  } catch {
    return false;
  }
}
