const test = require("node:test");
const assert = require("node:assert");
const { quoteIdent, tableRef, pageSql, sqlToGrid, docsToGrid, PAGE_SIZE } = require("../out/chart.js");

test("identifiers are quoted per engine and cannot break out", () => {
  assert.strictEqual(quoteIdent("postgres", 'a"b'), '"a""b"');
  assert.strictEqual(quoteIdent("mysql", "a`b"), "`a``b`");
  assert.strictEqual(tableRef("postgres", "public", "users"), '"public"."users"');
  assert.strictEqual(tableRef("mysql", "shop", "users"), "`users`");
  assert.strictEqual(tableRef("sqlite", "main", "users"), '"users"');
});

test("page SQL asks for one extra row and offsets by page", () => {
  assert.strictEqual(pageSql("sqlite", "main", "t", "", 0), `SELECT * FROM "t" LIMIT ${PAGE_SIZE + 1} OFFSET 0`);
  assert.strictEqual(pageSql("sqlite", "main", "t", "  id > 3 ; ", 2), `SELECT * FROM "t" WHERE id > 3 LIMIT ${PAGE_SIZE + 1} OFFSET ${2 * PAGE_SIZE}`);
});

test("SQL results become typed grid cells", () => {
  const g = sqlToGrid({ columns: ["id", "note"], rows: [{ id: { type: "number", display: "1" }, note: { type: "null", display: "null" } }] });
  assert.deepStrictEqual(g.rows[0], [{ v: "1", t: "num" }, { v: "null", t: "null" }]);
});

test("documents become a grid with unwrapped extended JSON and a column cap", () => {
  const g = docsToGrid([
    JSON.stringify({ _id: { $oid: "abc" }, n: 1, tags: ["x"], on: true, gone: null }),
    JSON.stringify({ _id: { $oid: "def" }, extra: "e" }),
    "not json",
  ]);
  assert.deepStrictEqual(g.columns, ["_id", "n", "tags", "on", "gone", "extra"]);
  assert.strictEqual(g.rows.length, 2);
  assert.deepStrictEqual(g.rows[0][0], { v: "abc", t: "text" });
  assert.deepStrictEqual(g.rows[0][2], { v: '["x"]', t: "json" });
  assert.deepStrictEqual(g.rows[1][1], { v: "", t: "null" });
  const wide = docsToGrid([JSON.stringify(Object.fromEntries(Array.from({ length: 50 }, (_, i) => ["k" + i, i])))]);
  assert.strictEqual(wide.columns.length, 30);
});
