import { useState } from "react";
import { ChevronRight, ChevronDown } from "lucide-react";
import "./FriendlyDoc.css";

// FriendlyDoc renders a MongoDB Extended JSON document as plain
// label/value rows instead of raw JSON syntax — no braces, quotes, or
// $oid/$date wrapper objects — so someone who has never seen a MongoDB
// document before can still read it. JsonTree (the {}/[] tree view) stays
// available as a toggle for people who want the exact wire format.
export function FriendlyDoc({ json }: { json: string }) {
  let parsed: unknown;
  try {
    parsed = JSON.parse(json);
  } catch {
    return <pre className="doc-json mono">{json}</pre>;
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    return (
      <div className="friendly-doc">
        <FriendlyScalar value={parsed} />
      </div>
    );
  }
  return (
    <div className="friendly-doc">
      <FriendlyFields entries={Object.entries(parsed as Record<string, unknown>)} depth={0} />
    </div>
  );
}

// humanizeLabel turns a raw field name into a readable label:
// "personalInfo" -> "Personal Info", "created_at" -> "Created At".
function humanizeLabel(key: string): string {
  if (key === "_id") return "ID";
  const spaced = key.replace(/([a-z0-9])([A-Z])/g, "$1 $2").replace(/_/g, " ");
  return spaced.replace(/\b\w/g, (c) => c.toUpperCase());
}

type ExtValue = { kind: "id" | "date" | "long" | "decimal" | "binary"; text: string };

// extJSONValue mirrors JsonTree's wrapper detection ($oid, $date, etc.) so
// both views agree on what counts as an extended-JSON scalar.
function extJSONValue(value: unknown): ExtValue | null {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return null;
  const obj = value as Record<string, unknown>;
  const keys = Object.keys(obj);
  if (keys.length !== 1) return null;
  const k = keys[0];
  switch (k) {
    case "$oid":
      return { kind: "id", text: String(obj[k]) };
    case "$numberLong":
      return { kind: "long", text: String(obj[k]) };
    case "$numberDecimal":
      return { kind: "decimal", text: String(obj[k]) };
    case "$date": {
      const v = obj[k];
      const text = typeof v === "object" && v !== null ? String((v as Record<string, unknown>)["$numberLong"]) : String(v);
      return { kind: "date", text };
    }
    case "$binary":
      return { kind: "binary", text: "<binary data>" };
    default:
      return null;
  }
}

function formatExtValue(ext: ExtValue): string {
  if (ext.kind !== "date") return ext.text;
  const ms = /^\d+$/.test(ext.text) ? Number(ext.text) : Date.parse(ext.text);
  if (Number.isNaN(ms)) return ext.text;
  return new Date(ms).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

function FriendlyFields({ entries, depth }: { entries: [string, unknown][]; depth: number }) {
  return (
    <div className="friendly-fields">
      {entries.map(([k, v]) => (
        <FriendlyRow key={k} label={k} value={v} depth={depth} />
      ))}
    </div>
  );
}

function FriendlyRow({ label, value, depth }: { label: string; value: unknown; depth: number }) {
  const ext = extJSONValue(value);
  if (ext) {
    return (
      <div className="friendly-row">
        <span className="friendly-label">{humanizeLabel(label)}</span>
        <span className={`friendly-value ${ext.kind === "id" ? "mono friendly-value-muted" : ""}`}>
          {formatExtValue(ext)}
        </span>
      </div>
    );
  }

  if (value === null || value === undefined) {
    return (
      <div className="friendly-row">
        <span className="friendly-label">{humanizeLabel(label)}</span>
        <span className="friendly-value friendly-value-empty">Not set</span>
      </div>
    );
  }

  if (Array.isArray(value)) {
    return <FriendlyArrayRow label={label} value={value} depth={depth} />;
  }

  if (typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>);
    if (entries.length === 0) {
      return (
        <div className="friendly-row">
          <span className="friendly-label">{humanizeLabel(label)}</span>
          <span className="friendly-value friendly-value-empty">Empty</span>
        </div>
      );
    }
    return (
      <div className="friendly-section">
        <div className="friendly-section-label">{humanizeLabel(label)}</div>
        <div className="friendly-nest">
          <FriendlyFields entries={entries} depth={depth + 1} />
        </div>
      </div>
    );
  }

  if (typeof value === "boolean") {
    return (
      <div className="friendly-row">
        <span className="friendly-label">{humanizeLabel(label)}</span>
        <span className={value ? "friendly-value friendly-bool-true" : "friendly-value friendly-bool-false"}>
          {value ? "Yes" : "No"}
        </span>
      </div>
    );
  }

  return (
    <div className="friendly-row">
      <span className="friendly-label">{humanizeLabel(label)}</span>
      <span className="friendly-value">{String(value)}</span>
    </div>
  );
}

function FriendlyArrayRow({ label, value, depth }: { label: string; value: unknown[]; depth: number }) {
  const [open, setOpen] = useState(value.length <= 2 && depth < 2);

  if (value.length === 0) {
    return (
      <div className="friendly-row">
        <span className="friendly-label">{humanizeLabel(label)}</span>
        <span className="friendly-value friendly-value-empty">None</span>
      </div>
    );
  }

  const allPrimitive = value.every((v) => extJSONValue(v) === null && (v === null || typeof v !== "object"));
  if (allPrimitive) {
    return (
      <div className="friendly-row">
        <span className="friendly-label">{humanizeLabel(label)}</span>
        <span className="friendly-value">{value.map((v) => (v === null ? "—" : String(v))).join(", ")}</span>
      </div>
    );
  }

  return (
    <div className="friendly-section">
      <button className="friendly-section-label friendly-array-toggle" onClick={() => setOpen(!open)}>
        {open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        {humanizeLabel(label)}
        <span className="friendly-count">{value.length}</span>
      </button>
      {open && (
        <div className="friendly-nest friendly-array-items">
          {value.map((item, i) => (
            <div className="friendly-array-item" key={i}>
              <div className="friendly-array-index">#{i + 1}</div>
              {typeof item === "object" && item !== null && !Array.isArray(item) && !extJSONValue(item) ? (
                <FriendlyFields entries={Object.entries(item as Record<string, unknown>)} depth={depth + 1} />
              ) : (
                <FriendlyScalar value={item} />
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function FriendlyScalar({ value }: { value: unknown }) {
  const ext = extJSONValue(value);
  if (ext) return <span className={ext.kind === "id" ? "mono friendly-value-muted" : ""}>{formatExtValue(ext)}</span>;
  if (value === null || value === undefined) return <span className="friendly-value-empty">Not set</span>;
  if (typeof value === "boolean") return <span>{value ? "Yes" : "No"}</span>;
  return <span>{String(value)}</span>;
}
