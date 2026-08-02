import { useState } from "react";
import { ChevronRight, ChevronDown } from "lucide-react";
import "./JsonTree.css";

// JsonTree renders a MongoDB Extended JSON document as a collapsible tree,
// recognizing the common wrapper types (ObjectId, dates, longs, decimals,
// binary) instead of showing their raw {"$oid": "..."} shape.
export function JsonTree({ json }: { json: string }) {
  let parsed: unknown;
  try {
    parsed = JSON.parse(json);
  } catch {
    return <pre className="doc-json mono">{json}</pre>;
  }
  return (
    <div className="json-tree mono">
      <JsonNode value={parsed} depth={0} />
    </div>
  );
}

// formatDateValue converts a $date value to a readable ISO timestamp.
// The relaxed Extended JSON form is already a plain ISO date string
// (e.g. "2023-11-14T22:13:20Z") — nothing to convert there. The
// canonical form (what `mongoexport --jsonFormat=canonical` and
// `EJSON.stringify(doc, {relaxed:false})` produce) wraps the millisecond
// epoch in a $numberLong, e.g. {"$date": {"$numberLong":
// "1700000000000"}} — without unwrapping that, the raw large integer was
// shown instead of a date. A bare number form is handled the same way
// defensively, in case some producer emits {"$date": 1700000000000}
// unwrapped. new Date(NaN).toISOString() throws, so an unparseable value
// falls back to its plain string form rather than crashing the tree.
function formatDateValue(v: unknown): string {
  let ms: number | undefined;
  if (typeof v === "object" && v !== null && "$numberLong" in v) {
    const n = Number((v as Record<string, unknown>)["$numberLong"]);
    if (Number.isFinite(n)) ms = n;
  } else if (typeof v === "number" && Number.isFinite(v)) {
    ms = v;
  }
  if (ms !== undefined) {
    const d = new Date(ms);
    if (!Number.isNaN(d.getTime())) return d.toISOString();
  }
  return String(v);
}

function extJSONBadge(value: unknown): { label: string; text: string } | null {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return null;
  const obj = value as Record<string, unknown>;
  const keys = Object.keys(obj);
  if (keys.length !== 1) return null;
  const k = keys[0];
  switch (k) {
    case "$oid":
      return { label: "ObjectId", text: String(obj[k]) };
    case "$numberLong":
      return { label: "Long", text: String(obj[k]) };
    case "$numberDecimal":
      return { label: "Decimal128", text: String(obj[k]) };
    case "$date":
      return { label: "Date", text: formatDateValue(obj[k]) };
    case "$binary":
      return { label: "Binary", text: "<binary data>" };
    default:
      return null;
  }
}

function JsonNode({ value, depth }: { value: unknown; depth: number }) {
  const [open, setOpen] = useState(depth < 2);

  const badge = extJSONBadge(value);
  if (badge) {
    return (
      <span className="json-ext-badge" title={badge.label}>
        <span className="json-ext-label">{badge.label}</span>
        {badge.text}
      </span>
    );
  }

  if (value === null) return <span className="json-null">null</span>;
  if (typeof value === "boolean") return <span className="json-bool">{String(value)}</span>;
  if (typeof value === "number") return <span className="json-number">{value}</span>;
  if (typeof value === "string") return <span className="json-string">"{value}"</span>;

  if (Array.isArray(value)) {
    if (value.length === 0) return <span className="json-punct">[]</span>;
    return (
      <span className="json-node">
        <button className="json-toggle" onClick={() => setOpen(!open)}>
          {open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
          <span className="json-punct">[{!open && `${value.length}`}]</span>
        </button>
        {open && (
          <div className="json-children">
            {value.map((v, i) => (
              <div className="json-row" key={i}>
                <span className="json-index">{i}:</span> <JsonNode value={v} depth={depth + 1} />
              </div>
            ))}
          </div>
        )}
      </span>
    );
  }

  if (typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>);
    if (entries.length === 0) return <span className="json-punct">{"{}"}</span>;
    return (
      <span className="json-node">
        <button className="json-toggle" onClick={() => setOpen(!open)}>
          {open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
          <span className="json-punct">{"{"}{!open && `${entries.length}`}{"}"}</span>
        </button>
        {open && (
          <div className="json-children">
            {entries.map(([k, v]) => (
              <div className="json-row" key={k}>
                <span className="json-key">{k}:</span> <JsonNode value={v} depth={depth + 1} />
              </div>
            ))}
          </div>
        )}
      </span>
    );
  }

  return <span>{String(value)}</span>;
}
