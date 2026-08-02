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
// is defensive escaping (quotes doubled, plus backslashes doubled for
// MySQL), not parameterized-query safety — acceptable here because every
// caller runs the resulting statement against the user's own connection via
// the same path Safe Mode already gates, not against untrusted input from
// someone else... except the webhook-payload-to-SQL mapping (WebhookView),
// where `value` genuinely can be attacker-controlled network input, which
// is exactly why the backslash case matters: MySQL treats `\` as a string
// escape character by default (no NO_BACKSLASH_ESCAPES set anywhere in
// this codebase's MySQL connections), so a value ending in an unescaped
// backslash lets the *next* character — including the quote that's about
// to close the literal, or one from the "''"-doubling below — be consumed
// as an escaped character instead of terminating the string, letting
// attacker-supplied text break out into live SQL. Backslashes must be
// escaped first, before quote-doubling, so the two passes stay
// independent: quote-doubling never introduces a backslash for the first
// pass to (incorrectly) re-escape, and backslash-escaping never introduces
// a quote for the second pass to double.
export function sqlLiteral(value: string, type: string, engineId?: string): string {
  if (type === "number" && /^-?\d+(\.\d+)?$/.test(value.trim())) return value.trim();
  let escaped = value;
  if (engineId === "mysql") escaped = escaped.replace(/\\/g, "\\\\");
  escaped = escaped.replace(/'/g, "''");
  return "'" + escaped + "'";
}

// sqlValueForCell renders a query-result Cell for interpolation into a
// WHERE clause, using the cell's own `type` (the actual, structural
// signal for a NULL SQL value — see engine.CellNull) to decide NULL vs. a
// literal — never inferring nullness from what the cell's `display` text
// happens to say. sqlLiteral used to do exactly that (treating the
// literal text "NULL", any case, as the SQL keyword), which meant a text
// column that legitimately stores the string "null" got silently nulled
// out instead of matched/set as that string. A cell that's genuinely
// NULL always has type "null" regardless of its display text, so keying
// off the type here is both correct and unambiguous.
export function sqlValueForCell(cell: { display: string; type: string }, engineId?: string): string {
  if (cell.type === "null") return "NULL";
  return sqlLiteral(cell.display, cell.type, engineId);
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
