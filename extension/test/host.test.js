// Drives the compiled extension host code (Controller + Broker + binary install) against the real
// `dbhelm` Go binary, with `vscode` mocked. Needs `npm run build:binary && npm run compile` first.
// Run: node --test test/
const test = require("node:test");
const assert = require("node:assert");
const Module = require("module");
const path = require("path");
const fs = require("fs");
const os = require("os");
const { spawnSync } = require("child_process");

const ext = path.join(__dirname, "..");
const binDir = path.join(ext, "bin", `${process.platform}-${process.arch}`);
const bundled = path.join(binDir, process.platform === "win32" ? "dbhelm.exe" : "dbhelm");
const skip = !fs.existsSync(bundled) || !fs.existsSync(path.join(ext, "out", "controller.js")) ? "build the binary and compile first" : false;

class Emitter {
  constructor() { this.l = []; this.event = (fn) => { this.l.push(fn); return { dispose() {} }; }; }
  fire(v) { for (const f of this.l) f(v); }
  dispose() {}
}
const settings = { requireAuth: false, safetySnapshot: true, autopilotAllowDangerous: false, autopilotMinutes: 0, binaryPath: "" };
const shown = [];
let workspaceRoot;
const vscodeMock = {
  EventEmitter: Emitter,
  Uri: { file: (p) => ({ fsPath: p }), joinPath: (...a) => ({ fsPath: path.join(...a.map((x) => x.fsPath || x)) }) },
  workspace: {
    get workspaceFolders() { return [{ uri: { fsPath: workspaceRoot }, name: "ws" }]; },
    getConfiguration: () => ({ get: (k, d) => (k in settings ? settings[k] : d), update: async () => {} }),
  },
  window: {
    showInformationMessage: async (m) => { shown.push(m); },
    showWarningMessage: async (m) => { shown.push(m); },
    showErrorMessage: async (m) => { shown.push(m); },
    showInputBox: async () => undefined,
    showTextDocument: async () => {},
  },
  commands: { executeCommand: async () => {} },
  ConfigurationTarget: { Global: 1 },
};
const load = Module._load;
Module._load = (req, ...rest) => (req === "vscode" ? vscodeMock : load(req, ...rest));

const ctx = () => ({ extensionPath: ext, secrets: { get: async () => undefined, store: async () => {}, delete: async () => {} }, globalState: { get: () => true, update: async () => {} } });
const output = { appendLine() {} };
const waitFor = async (fn, what, ms = 8000) => {
  const end = Date.now() + ms;
  while (Date.now() < end) { const v = await fn(); if (v) return v; await new Promise((r) => setTimeout(r, 30)); }
  throw new Error(`timed out waiting for ${what}`);
};

test("operator + agent hand in hand", { skip }, async (t) => {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "dbhelm-ext-"));
  workspaceRoot = path.join(tmp, "project");
  fs.mkdirSync(workspaceRoot);
  process.env.DBHELM_CONFIG_DIR = path.join(tmp, "cfg");
  delete process.env.DBHELM_RUN_DIR;
  const db = path.join(tmp, "app.db");
  spawnSync("sqlite3", [db, "create table users(id integer primary key, name text); insert into users(name) values('ann'),('bob');"]);
  const count = () => Number(spawnSync("sqlite3", [db, "select count(*) from users"]).stdout.toString().trim());
  assert.strictEqual(count(), 2, "sqlite3 seed failed");

  const { Controller } = require("../out/controller.js");
  const { installedBinaryPath } = require("../out/binary.js");
  const ctl = new Controller(ctx(), output);
  t.after(async () => { await ctl.broker.stop(); });

  await ctl.start();
  assert.strictEqual(ctl.state.broker.status, "ready", ctl.state.broker.error);
  assert.ok(fs.existsSync(installedBinaryPath()), "bundled binary was not installed to the stable path");
  assert.ok(fs.readFileSync(path.join(workspaceRoot, ".gitignore"), "utf8").includes(".dbhelm/run/"), ".dbhelm/run/ not gitignored");

  // Add a connection with write access for agents (no password in the URI).
  const ok = await ctl.saveConnection({ name: "lite", engine: "sqlite", uri: db, environment: "dev", readOnly: false, agentAccess: "write" });
  assert.ok(ok, JSON.stringify(ctl.state.notice));
  assert.strictEqual(ctl.state.connections.find((c) => c.name === "lite").access, "write");
  // A URI carrying a password is refused before anything is sent.
  assert.strictEqual(await ctl.saveConnection({ name: "x", engine: "postgres", uri: "postgres://u:secret@h/db", environment: "", readOnly: false, agentAccess: "off" }), false);
  assert.match(ctl.state.notice.text, /Remove the password/);

  const agent = (...args) => new Promise((resolve) => {
    const p = require("child_process").spawn(installedBinaryPath(), ["agent", ...args], { cwd: workspaceRoot });
    let out = ""; p.stdout.on("data", (d) => (out += d)); p.on("close", (code) => resolve({ code, env: out ? JSON.parse(out) : null }));
  });

  // The agent sees the connection and can read, but never a URI.
  const conns = await agent("connections");
  assert.ok(conns.env.ok && conns.env.data.some((c) => c.name === "lite"));
  assert.ok(!JSON.stringify(conns.env).includes(db), "connection list leaked the file path");
  const read = await agent("query", "--connection", "lite", "--db", "main", "--sql", "select count(*) n from users");
  assert.strictEqual(read.env.data.rows[0].n.display, "2");

  // A write request appears in the operator's state; the agent waits.
  const pending = agent("write", "--connection", "lite", "--db", "main", "--sql", "insert into users(name) values('cy')", "--reason", "test", "--wait", "30");
  const req = await waitFor(() => ctl.state.approvals.find((a) => a.status === "pending"), "pending approval");
  assert.strictEqual(req.statement, "insert into users(name) values('cy')");
  assert.strictEqual(count(), 2, "ran before approval");
  await ctl.decide(req.id, true);
  const done = await pending;
  assert.strictEqual(done.code, 0, JSON.stringify(done.env));
  assert.strictEqual(count(), 3);
  assert.ok(done.env.data.result.safetySnapshotId, "no safety snapshot id");
  await waitFor(() => ctl.state.activity.some((e) => e.event === "write.done"), "ship's log entry");

  // Refusing leaves data alone and the agent gets DENIED (exit 4).
  const denied = agent("write", "--connection", "lite", "--db", "main", "--sql", "delete from users where id = 1", "--wait", "30");
  const req2 = await waitFor(() => ctl.state.approvals.find((a) => a.status === "pending"), "second pending");
  await ctl.decide(req2.id, false);
  const d = await denied;
  assert.strictEqual(d.code, 4);
  assert.strictEqual(d.env.error.code, "DENIED");
  assert.strictEqual(count(), 3);

  // Autopilot: ordinary change runs unasked; a dangerous one still waits.
  await ctl.setAutopilot(true);
  assert.strictEqual(ctl.state.autopilot.on, true);
  const auto = await agent("write", "--connection", "lite", "--db", "main", "--sql", "insert into users(name) values('di')", "--wait", "30");
  assert.strictEqual(auto.code, 0, JSON.stringify(auto.env));
  assert.strictEqual(count(), 4);
  const danger = await agent("write", "--connection", "lite", "--db", "main", "--sql", "delete from users", "--wait", "1");
  assert.strictEqual(danger.code, 3);
  assert.strictEqual(danger.env.error.code, "PENDING_APPROVAL");
  assert.strictEqual(count(), 4);
  const req3 = await waitFor(() => ctl.state.approvals.find((a) => a.status === "pending" && a.risk === "dangerous"), "dangerous pending");
  await ctl.decide(req3.id, false);
  await ctl.setAutopilot(false);

  // Skill install: writes the skill with the exact binary path, a plain twin, and refreshes pointers.
  fs.writeFileSync(path.join(workspaceRoot, "AGENTS.md"), "# Project notes\n");
  const msg = await ctl.installSkill(false);
  assert.match(msg, /installed/);
  const skill = fs.readFileSync(path.join(workspaceRoot, ".claude", "skills", "dbhelm", "SKILL.md"), "utf8");
  assert.ok(skill.startsWith("---\nname: dbhelm"), "frontmatter must stay first");
  assert.ok(skill.includes(installedBinaryPath()), "skill does not name the binary");
  assert.ok(fs.existsSync(path.join(workspaceRoot, ".claude", "skills", "dbhelm", "references", "safety.md")));
  assert.ok(fs.readFileSync(path.join(workspaceRoot, ".dbhelm", "AGENTS.md"), "utf8").includes("dbhelm agent"));
  assert.ok(fs.readFileSync(path.join(workspaceRoot, "AGENTS.md"), "utf8").includes("dbhelm:agent-instructions:start"));
  assert.match(await ctl.installSkill(false), /up to date/);
  fs.appendFileSync(path.join(workspaceRoot, ".claude", "skills", "dbhelm", "SKILL.md"), "\nmy edit\n");
  assert.match(await ctl.installSkill(false), /Kept your edited/);
  assert.match(await ctl.installSkill(true), /installed/);

  // Chart: tables, structure, paged data, a filter; a hostile filter cannot write.
  spawnSync("sqlite3", [db, "create table big(id integer primary key, v text); with recursive c(x) as (select 1 union all select x+1 from c where x<120) insert into big(v) select 'row'||x from c;"]);
  await ctl.chartSelect("lite", "main");
  assert.ok(ctl.state.chart.tables.some((t) => t.name === "big"), JSON.stringify(ctl.state.chart));
  await ctl.chartTable("big");
  let ch = ctl.state.chart;
  assert.deepStrictEqual(ch.describe.columns.map((c) => c.name), ["id", "v"]);
  assert.strictEqual(ch.grid.rows.length, 50);
  assert.strictEqual(ch.grid.hasMore, true);
  await ctl.chartPage(2, "");
  ch = ctl.state.chart;
  assert.strictEqual(ch.grid.rows.length, 20);
  assert.strictEqual(ch.grid.hasMore, false);
  assert.strictEqual(ch.grid.rows[0][1].v, "row101");
  await ctl.chartPage(0, "id > 115");
  assert.strictEqual(ctl.state.chart.grid.rows.length, 5);
  await ctl.chartPage(0, "1=1; DROP TABLE big");
  assert.ok(ctl.state.chart.error, "hostile filter was not refused");
  assert.strictEqual(Number(spawnSync("sqlite3", [db, "select count(*) from big"]).stdout.toString().trim()), 120, "table was changed through the Chart");

  // MCP: the snippet names the binary and pins the run dir; .mcp.json merges without clobbering.
  const snippet = JSON.parse(ctl.mcpConfigText());
  assert.deepStrictEqual(snippet.mcpServers.dbhelm.args, ["mcp"]);
  assert.strictEqual(snippet.mcpServers.dbhelm.env.DBHELM_RUN_DIR, path.join(workspaceRoot, ".dbhelm", "run"));
  fs.writeFileSync(path.join(workspaceRoot, ".mcp.json"), JSON.stringify({ mcpServers: { other: { command: "x" } }, keep: true }));
  assert.strictEqual(ctl.addMcpToProject(false).action, "updated");
  const mcp = JSON.parse(fs.readFileSync(path.join(workspaceRoot, ".mcp.json"), "utf8"));
  assert.ok(mcp.mcpServers.other && mcp.keep && mcp.mcpServers.dbhelm, "merge lost existing config");
  assert.strictEqual(ctl.addMcpToProject(false).action, "unchanged");
  mcp.mcpServers.dbhelm.args = ["different"];
  fs.writeFileSync(path.join(workspaceRoot, ".mcp.json"), JSON.stringify(mcp));
  assert.strictEqual(ctl.addMcpToProject(false).action, "skipped-differs");

  // Stopping the broker (extension deactivating) closes the operator: writes fail closed.
  await ctl.broker.stop();
  const headless = await agent("write", "--connection", "lite", "--db", "main", "--sql", "insert into users(name) values('ed')", "--wait", "1");
  assert.strictEqual(headless.env.error.code, "NO_OPERATOR");
  assert.strictEqual(count(), 4);
  await agent("status"); // autostarted headless broker; stop it so the test leaves nothing behind
  const info = JSON.parse(fs.readFileSync(path.join(workspaceRoot, ".dbhelm", "run", "broker.json"), "utf8"));
  assert.strictEqual(info.headless, true);
  await fetch(`http://127.0.0.1:${info.port}/rpc`, { method: "POST", headers: { Authorization: `Bearer ${info.agentToken}` }, body: JSON.stringify({ method: "shutdown" }) });
});
