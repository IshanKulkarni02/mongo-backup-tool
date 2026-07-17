// Shared SQL string-building helpers for the frontend's simple,
// client-generated statements (TableView's inline cell edits, the
// webhook-payload-to-SQL-insert mapping) — not a full query builder, just
// consistent identifier quoting and literal escaping so this logic lives
// in one place instead of being copy-pasted per view.

export function quoteIdent(engineId: string, name: string): string {
  if (engineId === "mysql") return "`" + name.replace(/`/g, "``") + "`";
  return '"' + name.replace(/"/g, '""') + '"';
}

// sqlLiteral renders a value for interpolation into a SQL statement. This
// is defensive escaping (quotes doubled), not parameterized-query safety —
// acceptable here because every caller runs the resulting statement
// against the user's own connection via the same path Safe Mode already
// gates, not against untrusted input from someone else.
export function sqlLiteral(value: string, type: string): string {
  if (value.trim().toUpperCase() === "NULL") return "NULL";
  if (type === "number" && /^-?\d+(\.\d+)?$/.test(value.trim())) return value.trim();
  return "'" + value.replace(/'/g, "''") + "'";
}

// isGeoType reports whether a Postgres column's data type is a PostGIS
// geometry/geography column — these come back as opaque WKB text unless
// explicitly wrapped in ST_AsGeoJSON, which buildSelectList below does.
export function isGeoType(dataType: string): boolean {
  const t = dataType.toLowerCase();
  return t === "geometry" || t === "geography";
}

// buildSelectList renders a table's columns for a SELECT, wrapping Postgres
// geometry/geography columns in ST_AsGeoJSON so they arrive as renderable
// GeoJSON text instead of raw WKB — the DataGrid's right-click menu then
// offers "Send to Geo Viewer" on the result automatically, since it goes
// by content shape, not column type. Falls back to "*" when no schema is
// available yet (e.g. the very first paint before introspection resolves).
export function buildSelectList(engineId: string, columns: { name: string; dataType: string }[] | undefined): string {
  if (!columns || columns.length === 0) return "*";
  return columns
    .map((c) => {
      const ident = quoteIdent(engineId, c.name);
      if (engineId === "postgres" && isGeoType(c.dataType)) {
        return `ST_AsGeoJSON(${ident}) AS ${ident}`;
      }
      return ident;
    })
    .join(", ");
}
