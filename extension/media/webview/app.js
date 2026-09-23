// DBHelm webview. One script for both surfaces: the sidebar ("ops", read-only operations) and the
// editor tab ("config", connections and agent access). All host communication is postMessage.
// The helper block below (el, ic, iconBtn, info, labelText, field, labeledInput, labeledSelect,
// section, notice, link, kv, summaryField) is copied verbatim from FTPilot's panel.ts.
"use strict";
const vscode = acquireVsCodeApi();
function post(msg) { vscode.postMessage(msg); }
const MODE = { sidebar: "ops", config: "config", chart: "chart" }[window.SURFACE] || "ops";

let st = null;                       // latest state from the extension host
let view = { name: "main" };         // main | request | restore | autopilot | remove | backupRestore
let sel = { conn: "", db: "" };      // snapshot picker
let form = { name: "", engine: "mongodb", uri: "", environment: "", agentAccess: "off", readOnly: false };
let pendingFocus = null;
let tick = null;
let chartSel = { conn: "", db: "" };
let chartTab = "data";
let chartFilter = null;  // null = untouched: show the filter the host last applied
let tableFilter = "";

// Per-view UI conveniences (collapsed sections). Safe to lose; never holds anything sensitive.
let ui = { sections: {}, cards: {} };
try { const s = vscode.getState(); if (s && s.ui) ui = s.ui; } catch (e) {}
function saveUi() { try { vscode.setState({ ui }); } catch (e) {} }

function el(tag, attrs, children) {
  const node = document.createElement(tag);
  for (const k in attrs || {}) {
    if (k === "class") node.className = attrs[k];
    else if (k.startsWith("on")) node.addEventListener(k.slice(2), attrs[k]);
    else node.setAttribute(k, attrs[k]);
  }
  for (const c of children || []) {
    if (c === null || c === undefined || c === false) continue;
    node.appendChild(typeof c === "string" ? document.createTextNode(c) : c);
  }
  return node;
}

function ic(name, extra) {
  return el("i", { class: "codicon codicon-" + name + (extra ? " " + extra : ""), "aria-hidden": "true" }, []);
}

function iconBtn(icon, title, onclick, disabled) {
  return el("button", { class: "icon", title, "aria-label": title, ...(disabled ? { disabled: "disabled" } : {}),
    onclick: (e) => { e.stopPropagation(); onclick(e); } }, [ic(icon)]);
}


function info(text) {
  return el("span", { class: "info", title: text, "aria-label": text, role: "img" }, [ic("info")]);
}

function labelText(text, opts) {
  return el("span", { class: "label-text" }, [text, opts && opts.info ? info(opts.info) : null]);
}


function field(labelEl, opts) {
  return opts && opts.help ? el("div", { class: "field" }, [labelEl, el("p", { class: "hint" }, [opts.help])]) : labelEl;
}

function labeledInput(text, value, onInput, opts) {
  opts = opts || {};
  const input = el("input", {
    type: opts.type || "text",
    value: value || "",
    placeholder: opts.placeholder || "",
    oninput: (e) => onInput(e.target.value),
  });
  return field(el("label", {}, [labelText(text, opts), input]), opts);
}

function labeledSelect(text, value, options, onChange, opts) {
  const select = el("select", { onchange: (e) => onChange(e.target.value) },
    options.map((o) => el("option", { value: o.value, ...(o.value === value ? { selected: "selected" } : {}) }, [o.label])));
  return field(el("label", {}, [labelText(text, opts), select]), opts);
}

// A <select> of real, on-disk folders, falling back to a free-text box for "Custom" or

function section(key, title, children, opts) {
  opts = opts || {};
  const collapsed = !!ui.sections[key];
  const toggle = () => { ui.sections[key] = !collapsed; saveUi(); render(); };
  const head = el("div", {
    class: "section-head", role: "button", tabindex: "0", "aria-expanded": String(!collapsed),
    onclick: toggle, onkeydown: (e) => { if (e.target === e.currentTarget && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); toggle(); } },
  }, [
    ic(collapsed ? "chevron-right" : "chevron-down"),
    el("span", { class: "title" }, [title, opts.count !== undefined ? el("span", { class: "count" }, [" (" + opts.count + ")"]) : null]),
    opts.actions ? el("span", { class: "acts", ...(opts.actionsId ? { id: opts.actionsId } : {}) }, opts.actions) : null,
  ]);
  return el("div", { class: "section" + (collapsed ? " collapsed" : "") }, [head, el("div", { class: "section-body" }, children)]);
}

function notice(kind, icon, children, actions) {
  return el("div", { class: "notice " + kind, role: kind === "error" ? "alert" : "status" }, [
    ic(icon), el("span", { class: "grow" }, children), actions ? el("span", { class: "actions" }, actions) : null,
  ]);
}

function link(text, onclick, icon) {
  return el("button", { class: "link", onclick }, [icon ? ic(icon) : null, text]);
}

function kv(icon, label, value, extra) {
  return el("div", { class: "kv" }, [ic(icon), el("span", { class: "k" }, [label]), el("span", { class: "v" }, [el("span", { class: "vt" }, value), extra || null])]);
}

function summaryField(label, value) {
  return el("div", { class: "field-row" }, [el("span", {}, [label]), el("span", {}, [value || "-"])]);
}


// ---------------------------------------------------------------------------------------------
// Small local helpers
// ---------------------------------------------------------------------------------------------

const ENGINES = [
  { value: "mongodb", label: "MongoDB" }, { value: "postgres", label: "PostgreSQL" },
  { value: "mysql", label: "MySQL" }, { value: "sqlite", label: "SQLite" },
];
const ENGINE_LABEL = { mongodb: "MongoDB", postgres: "PostgreSQL", mysql: "MySQL", sqlite: "SQLite" };
const URI_HINT = {
  mongodb: "mongodb://user@localhost:27017",
  postgres: "postgres://user@localhost:5432/mydb?sslmode=disable",
  mysql: "user@tcp(localhost:3306)/mydb",
  sqlite: "/path/to/database.db",
};
const ACCESS_HELP = {
  off: "Agents cannot see this connection.",
  read: "Agents can read. They cannot change data.",
  write: "Agents can read and ask to change data. You approve each change, or Autopilot does.",
};
const ACCESS_LABEL = { off: "Off", read: "Read", write: "Read and ask to write" };
const RISK_LABEL = { none: "Low risk", confirm: "Changes data", dangerous: "Dangerous" };

function conn(name) { return st.connections.find((c) => c.name === name); }
function pendingApprovals() { return st.approvals.filter((a) => a.status === "pending"); }
function short(id) { return String(id || "").slice(0, 8); }
function fmtBytes(n) {
  if (!n) return "0 B";
  const u = ["B", "KB", "MB", "GB", "TB"]; let i = 0; let v = n;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return (i === 0 ? v : v.toFixed(1)) + " " + u[i];
}
function relTime(iso) {
  const t = Date.parse(iso); if (!t) return "";
  const s = Math.max(0, Math.round((Date.now() - t) / 1000));
  if (s < 60) return "just now";
  if (s < 3600) return Math.round(s / 60) + " min ago";
  if (s < 86400) return Math.round(s / 3600) + " h ago";
  if (s < 86400 * 30) return Math.round(s / 86400) + " d ago";
  return new Date(t).toLocaleDateString();
}
function fmtElapsed(ms) {
  const s = Math.floor(ms / 1000); return (s >= 60 ? Math.floor(s / 60) + "m " : "") + (s % 60) + "s";
}

/** labeledInput plus a stable key, so a re-render (state pushes arrive at any time) keeps focus. */
function textField(key, text, value, onInput, opts) {
  const node = labeledInput(text, value, onInput, opts);
  const input = node.querySelector("input"); if (input) input.setAttribute("data-k", key);
  return node;
}
function switchRow(label, on, onToggle, help) {
  const sw = el("span", { class: "switch", role: "switch", tabindex: "0", "aria-checked": String(!!on), "aria-label": label,
    onclick: () => onToggle(!on), onkeydown: (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onToggle(!on); } } }, []);
  return el("div", {}, [el("div", { class: "switch-row" }, [sw, el("span", {}, [label])]), help ? el("p", { class: "hint" }, [help]) : null]);
}
function chip(text, kind) { return el("span", { class: "chip" + (kind ? " " + kind : "") }, [text]); }
function pre(text) { return el("pre", { class: "stmt" }, [text]); }

function go(next) { view = next; render(); }
function back() { go({ name: "main" }); }

/** A confirm screen: read-only summary, then explicit verb buttons (never "OK"). */
function confirmView(title, body, primary, secondary) {
  const root = document.getElementById("root");
  root.innerHTML = "";
  const btns = [el("button", { class: "block", onclick: primary.onclick }, [ic(primary.icon), primary.label])];
  for (const s of secondary || []) btns.push(el("button", { class: "secondary block", onclick: s.onclick }, [ic(s.icon), s.label]));
  btns.push(el("button", { class: "secondary block", onclick: back }, [ic("arrow-left"), "Back"]));
  root.appendChild(el("div", { class: "confirm" }, [el("h2", {}, [title]), ...body, el("div", { class: "btns" }, btns)]));
}

// ---------------------------------------------------------------------------------------------
// Shared cards
// ---------------------------------------------------------------------------------------------

function topNotices() {
  const out = [];
  if (st.broker.status === "starting") out.push(notice("busy", "loading codicon-modifier-spin", ["Starting the DBHelm broker…"]));
  if (st.broker.status === "error") {
    out.push(notice("error", "error", [st.broker.error || "The DBHelm broker is not running."],
      [link("Restart Broker", () => post({ type: "restartBroker" }))]));
  }
  if (st.notice) out.push(notice(st.notice.kind, { info: "info", ok: "pass", warn: "warning", error: "error" }[st.notice.kind] || "info", [st.notice.text], [link("Dismiss", () => post({ type: "clearNotice" }))]));
  return out;
}

function jobCard() {
  if (!st.job) return null;
  return notice("busy", "loading codicon-modifier-spin",
    [el("span", {}, [st.job.label + "… "]), el("span", { class: "muted", id: "job-elapsed" }, [fmtElapsed(Date.now() - st.job.startedAt)])],
    [link("Cancel", () => post({ type: "cancelJob" }))]);
}

function resultCard() {
  if (!st.result) return null;
  const actions = [];
  if (st.result.undo) {
    const u = st.result.undo;
    actions.push(link("Undo", () => go({ name: "restore", connection: u.connection, database: u.database, snapshotId: u.snapshotId, undo: true })));
  }
  actions.push(link("Dismiss", () => post({ type: "dismissResult" })));
  return notice(st.result.ok ? "ok" : "error", st.result.ok ? "pass" : "error", [st.result.text], actions);
}

// ---------------------------------------------------------------------------------------------
// The Bridge: requests from agents, Autopilot, recent activity
// ---------------------------------------------------------------------------------------------

function requestCard(a) {
  const running = a.status === "running";
  return el("div", { class: "req risk-" + a.risk }, [
    el("div", { class: "req-head" }, [chip(RISK_LABEL[a.risk] || a.risk, a.risk), el("span", { class: "req-target" }, [a.connection + "." + a.database]), el("span", { class: "muted req-time" }, [relTime(a.createdAt)])]),
    pre(a.statement),
    a.reason ? el("p", { class: "hint" }, [a.reason]) : null,
    running
      ? el("div", { class: "muted" }, [ic("loading", "codicon-modifier-spin"), " Running"])
      : el("div", { class: "req-actions" }, [
          el("button", { onclick: () => go({ name: "request", id: a.id }) }, [ic("eye"), "Review…"]),
          el("button", { class: "secondary", onclick: () => post({ type: "decide", id: a.id, approve: false }) }, [ic("close"), "Refuse"]),
        ]),
  ]);
}

const OUTCOME_ICON = { ok: "check", approved: "check", refused: "circle-slash", failed: "error", pending: "clock" };
function activityRow(e) {
  const target = e.connection ? e.connection + (e.database ? "." + e.database : "") : "";
  return el("div", { class: "act" }, [
    ic(OUTCOME_ICON[e.outcome] || "circle-small"),
    el("span", { class: "act-text" }, [e.actor + " " + e.event + (target ? " " + target : "")]),
    el("span", { class: "muted" }, [relTime(e.ts)]),
  ]);
}

function bridgeSection() {
  const pending = pendingApprovals();
  const running = st.approvals.filter((a) => a.status === "running");
  const recent = st.approvals.filter((a) => a.status !== "pending" && a.status !== "running").slice(0, 2);
  const auto = st.autopilot.on;
  const body = [
    kv(auto ? "compass" : "shield", "Mode", [auto ? "Autopilot" : "Manual Helm"]),
    el("p", { class: "hint" }, [auto
      ? "DBHelm approves agent changes for you. Dangerous ones " + (st.autopilot.allowDangerous ? "included." : "still wait for you.")
      : "Agents ask, DBHelm asks you. Nothing changes without your say."]),
    el("button", { class: "secondary block" + (auto ? "" : ""), onclick: () => auto ? post({ type: "setAutopilot", on: false }) : go({ name: "autopilot" }) },
      [ic(auto ? "shield" : "compass"), auto ? "Return to Manual Helm" : "Switch on Autopilot…"]),
  ];
  for (const a of [...pending, ...running]) body.push(requestCard(a));
  if (!pending.length && !running.length) body.push(el("p", { class: "empty-note" }, ["No requests. Agents ask here before changing data."]));
  for (const a of recent) body.push(el("div", { class: "act" }, [ic(a.status === "done" ? "check" : a.status === "denied" ? "circle-slash" : "warning"), el("span", { class: "act-text" }, [a.status + ": " + a.statement.slice(0, 48)]), el("span", { class: "muted" }, [relTime(a.decidedAt || a.createdAt)])]));
  if (st.activity.length) {
    body.push(el("div", { class: "sub" }, ["Recent activity"]));
    for (const e of st.activity.slice(0, 5)) body.push(activityRow(e));
  }
  return section("bridge", "Bridge", body, { count: pending.length || undefined });
}

// ---------------------------------------------------------------------------------------------
// Sidebar sections
// ---------------------------------------------------------------------------------------------

function connectionsSection() {
  const body = [];
  if (!st.connections.length) {
    body.push(el("p", { class: "empty-note" }, ["No connections yet."]));
    body.push(el("button", { class: "block", onclick: () => post({ type: "openInEditor", focus: "add" }) }, [ic("add"), "Add Connection…"]));
  }
  for (const c of st.connections) {
    body.push(el("div", { class: "row" }, [
      el("span", { class: "row-main" }, [el("span", { class: "row-title" }, [c.name]), chip(ENGINE_LABEL[c.engine] || c.engine), c.environment ? chip(c.environment, c.environment === "prod" ? "dangerous" : "") : null,
        c.access !== "off" ? chip("agents: " + c.access, "agent") : null]),
      el("span", { class: "row-acts" }, [
        iconBtn("pulse", "Test connection", () => post({ type: "testConnection", name: c.name })),
        iconBtn("table", "Browse schema and data", () => post({ type: "chartOpen", connection: c.name })),
        c.capabilities.snapshots ? iconBtn("device-camera", "Pick for snapshots", () => { sel = { conn: c.name, db: "" }; post({ type: "loadDatabases", connection: c.name }); render(); }) : null,
      ]),
    ]));
  }
  return section("connections", "Connections", body, { count: st.connections.length,
    actions: [iconBtn("add", "Add connection", () => post({ type: "openInEditor", focus: "add" }))] });
}

function snapshotsSection() {
  const snapConns = st.connections.filter((c) => c.capabilities.snapshots);
  if (!snapConns.length) return null;
  if (!sel.conn || !conn(sel.conn)) sel.conn = snapConns[0].name;
  const dbs = st.databases[sel.conn] || [];
  if (!sel.db && dbs.length === 1) sel.db = dbs[0];
  const key = sel.conn + "/" + sel.db;
  const snaps = st.snapshots[key];
  const body = [
    labeledSelect("Connection", sel.conn, snapConns.map((c) => ({ value: c.name, label: c.name })), (v) => { sel = { conn: v, db: "" }; post({ type: "loadDatabases", connection: v }); render(); }),
    dbs.length
      ? labeledSelect(st.connections.find((c) => c.name === sel.conn)?.engine === "postgres" ? "Schema" : "Database", sel.db, [{ value: "", label: "Choose…" }, ...dbs.map((d) => ({ value: d, label: d }))], (v) => { sel.db = v; if (v) post({ type: "loadSnapshots", connection: sel.conn, database: v }); render(); })
      : el("p", { class: "hint" }, [link("Load databases", () => post({ type: "loadDatabases", connection: sel.conn }))]),
  ];
  if (sel.db) {
    const busy = !!st.job;
    body.push(el("div", { class: "btn-row" }, [
      el("button", { ...(busy ? { disabled: "disabled" } : {}), onclick: () => post({ type: "snapshotCreate", connection: sel.conn, database: sel.db }) }, [ic("device-camera"), "Snapshot Now"]),
      conn(sel.conn).engine === "mongodb" ? el("button", { class: "secondary", ...(busy ? { disabled: "disabled" } : {}), onclick: () => post({ type: "backupCreate", connection: sel.conn, database: sel.db }) }, [ic("archive"), "Back Up"]) : null,
    ]));
    if (snaps === undefined) body.push(el("p", { class: "hint" }, ["Loading…"]));
    else if (!snaps.length) body.push(el("p", { class: "empty-note" }, ["No snapshots yet."]));
    else for (const s of snaps.slice(0, 12)) {
      body.push(el("div", { class: "snap" }, [
        el("span", { class: "dot" }, []),
        el("span", { class: "snap-main" }, [
          el("span", { class: "row-title" }, [s.message || "(no message)"]),
          el("span", { class: "muted" }, [short(s.id) + " · " + relTime(s.createdAt) + " · " + s.docCount + " rows"]),
          s.tags && s.tags.length ? el("span", {}, s.tags.map((t) => chip(t))) : null,
        ]),
        iconBtn("history", "Restore this snapshot…", () => go({ name: "restore", connection: sel.conn, database: sel.db, snapshotId: s.id, message: s.message, createdAt: s.createdAt })),
      ]));
    }
    if (snaps && snaps.length > 12) body.push(el("p", { class: "hint" }, ["Showing the newest 12 of " + snaps.length + "."]));
  }
  return section("snapshots", "Snapshots", body);
}

function backupsSection() {
  if (!st.backups.length) return null;
  const body = st.backups.slice().reverse().slice(0, 8).map((b) => el("div", { class: "row" }, [
    el("span", { class: "row-main" }, [el("span", { class: "row-title" }, [b.connection + "." + (b.database || "all")]), el("span", { class: "muted" }, [fmtBytes(b.sizeBytes) + " · " + relTime(b.createdAt)])]),
    el("span", { class: "row-acts" }, [
      iconBtn("history", "Restore this backup…", () => go({ name: "backupRestore", backup: b })),
      iconBtn("trash", "Delete this backup", () => post({ type: "backupDelete", id: b.id })),
    ]),
  ]));
  return section("backups", "Backups", body, { count: st.backups.length });
}

function renderOps() {
  const root = document.getElementById("root");
  const kids = [];
  if (!st.workspaceOpen) {
    kids.push(el("div", { class: "empty" }, ["Open a project folder to use DBHelm. Its settings and ship's log live in that folder's .dbhelm folder."]));
  } else {
    const tops = [...topNotices(), jobCard(), resultCard()].filter(Boolean);
    kids.push(...tops);
    if (st.broker.status !== "ready") {
      // Everything below comes from the broker; without it an empty list would be misleading.
      kids.push(el("div", { class: "footer-links" }, [link("Ship's Log", () => post({ type: "openLog" }), "book")]));
      root.appendChild(el("div", {}, kids));
      return;
    }
    if (st.autopilot.on) kids.push(notice("warn", "compass", ["Autopilot is on. DBHelm approves agent changes for you."], [link("Manual Helm", () => post({ type: "setAutopilot", on: false }))]));
    if (!st.skillInstalled && st.connections.some((c) => c.access !== "off")) {
      kids.push(notice("info", "sparkle", ["Agents do not know about DBHelm in this project yet."], [link("Set Up Agent Skill…", () => post({ type: "setupSkill" }))]));
    }
    for (const s of [bridgeSection(), connectionsSection(), snapshotsSection(), backupsSection()]) if (s) kids.push(s);
    kids.push(el("div", { class: "footer-links" }, [
      link("Connections and Agent Access", () => post({ type: "openInEditor" }), "link-external"),
      link("Chart: Schema and Data", () => post({ type: "chartOpen" }), "table"),
      link("Ship's Log", () => post({ type: "openLog" }), "book"),
    ]));
  }
  root.appendChild(el("div", {}, kids));
}

// ---------------------------------------------------------------------------------------------
// Editor tab: connections and agent access
// ---------------------------------------------------------------------------------------------

function addForm() {
  const isFile = form.engine === "sqlite";
  const uriField = isFile
    ? el("div", { class: "with-btn" }, [
        textField("uri", "Database file", form.uri, (v) => (form.uri = v), { placeholder: URI_HINT.sqlite }),
        el("button", { class: "secondary square", title: "Browse for a file", "aria-label": "Browse for a file", onclick: (e) => { e.preventDefault(); post({ type: "pickSqliteFile" }); } }, [ic("folder-opened")]),
      ])
    : textField("uri", "Connection URI", form.uri, (v) => (form.uri = v), { placeholder: URI_HINT[form.engine], help: "Leave the password out. DBHelm asks for it next and keeps it in your system keychain." });
  return [
    textField("name", "Name", form.name, (v) => (form.name = v), { placeholder: "local-dev" }),
    labeledSelect("Engine", form.engine, ENGINES, (v) => { form.engine = v; render(); }),
    uriField,
    el("div", { class: "grid2" }, [
      labeledSelect("Environment", form.environment, [{ value: "", label: "Not set" }, { value: "dev", label: "dev" }, { value: "staging", label: "staging" }, { value: "prod", label: "prod" }], (v) => (form.environment = v)),
      labeledSelect("Agent access", form.agentAccess, Object.keys(ACCESS_LABEL).map((k) => ({ value: k, label: ACCESS_LABEL[k] })), (v) => { form.agentAccess = v; render(); }),
    ]),
    el("p", { class: "hint" }, [ACCESS_HELP[form.agentAccess]]),
    switchRow("Read-only connection", form.readOnly, (v) => { form.readOnly = v; render(); }, "DBHelm refuses every write on it, from you and from agents."),
    el("div", { class: "actions-row single" }, [el("button", { class: "block", onclick: () => post({ type: "saveConnection", form }) }, [ic("save"), "Save Connection"])]),
  ];
}

function connectionCard(c) {
  return el("div", { class: "card" }, [
    el("div", { class: "card-head" }, [el("span", { class: "title" }, [c.name]), chip(ENGINE_LABEL[c.engine] || c.engine)]),
    el("div", { class: "card-body" }, [
      el("div", { class: "grid2" }, [
        labeledSelect("Environment", c.environment || "", [{ value: "", label: "Not set" }, { value: "dev", label: "dev" }, { value: "staging", label: "staging" }, { value: "prod", label: "prod" }], (v) => post({ type: "updateConnection", name: c.name, patch: { environment: v } })),
        labeledSelect("Agent access", c.access, Object.keys(ACCESS_LABEL).map((k) => ({ value: k, label: ACCESS_LABEL[k] })), (v) => post({ type: "updateConnection", name: c.name, patch: { agentAccess: v } })),
      ]),
      el("p", { class: "hint" }, [ACCESS_HELP[c.access]]),
      switchRow("Read-only connection", !!c.readOnly, (v) => post({ type: "updateConnection", name: c.name, patch: { readOnly: v } })),
      el("div", { class: "btn-row" }, [
        c.engine !== "sqlite" ? el("button", { class: "secondary", onclick: () => post({ type: "setPassword", name: c.name }) }, [ic("key"), "Set Password…"]) : null,
        el("button", { class: "secondary", onclick: () => post({ type: "testConnection", name: c.name }) }, [ic("pulse"), "Test"]),
        el("button", { class: "secondary", onclick: () => go({ name: "remove", connection: c.name }) }, [ic("trash"), "Remove…"]),
      ]),
    ]),
  ]);
}

function agentsSection() {
  return section("agents", "Agents", [
    kv(st.skillInstalled ? "pass" : "circle-outline", "Skill", [st.skillInstalled ? "Installed in this project" : "Not installed"]),
    el("p", { class: "hint" }, ["The skill teaches coding agents to reach databases only through DBHelm: read-only reads, and changes that wait for you."]),
    el("button", { class: "secondary block", onclick: () => post({ type: "setupSkill" }) }, [ic("sparkle"), st.skillInstalled ? "Update Agent Skill…" : "Set Up Agent Skill…"]),
    el("div", { class: "btn-row" }, [
      el("button", { class: "secondary", onclick: () => post({ type: "mcpConfig", action: "project" }) }, [ic("plug"), "Add MCP to Project…"]),
      el("button", { class: "secondary", onclick: () => post({ type: "mcpConfig", action: "copy" }) }, [ic("copy"), "Copy MCP Config"]),
    ]),
    el("p", { class: "hint" }, ["For Cursor, Codex, Claude Desktop and other MCP clients. Same rules as the skill: reads only, changes wait for you."]),
    kv("terminal", "Binary", [st.binaryPath || "dbhelm"]),
    kv("shield", "App Lock", [st.auth.required ? (st.auth.hasPassword ? "On, with an app password" : "On") : "Off"]),
    el("div", { class: "btn-row" }, [
      el("button", { class: "secondary", onclick: () => post({ type: "configureAppLock" }) }, [ic("shield"), "App Lock…"]),
      el("button", { class: "secondary", onclick: () => post({ type: "openSettings" }) }, [ic("settings-gear"), "Settings"]),
    ]),
  ]);
}

function logSection() {
  const rows = st.activity.length ? st.activity.map(activityRow) : [el("p", { class: "empty-note" }, ["Nothing yet."])];
  return section("log", "Ship's Log", [...rows, el("p", { class: "hint" }, [link("Open the log file", () => post({ type: "openLog" }), "go-to-file")])], { count: st.activity.length || undefined });
}

function renderConfig() {
  const root = document.getElementById("root");
  const kids = [el("h1", { class: "page-title" }, ["Connections and Agent Access"])];
  if (!st.workspaceOpen) {
    kids.push(el("div", { class: "empty" }, ["Open a project folder to use DBHelm."]));
  } else {
    kids.push(...[...topNotices(), jobCard(), resultCard()].filter(Boolean));
    kids.push(section("add", "Add connection", addForm()));
    kids.push(section("saved", "Saved connections", st.connections.length ? st.connections.map(connectionCard) : [el("p", { class: "empty-note" }, ["No connections yet."])], { count: st.connections.length }));
    kids.push(agentsSection());
    kids.push(logSection());
  }
  root.appendChild(el("div", {}, kids));
}


// ---------------------------------------------------------------------------------------------
// Chart: read-only schema browser and data grid
// ---------------------------------------------------------------------------------------------

function fmtCount(n) { return n >= 1e6 ? (n / 1e6).toFixed(1) + "M" : n >= 1e3 ? (n / 1e3).toFixed(1) + "k" : String(n); }

function chartPicker() {
  const conns = st.connections;
  if (!chartSel.conn || !conn(chartSel.conn)) chartSel.conn = (st.chart && conn(st.chart.connection) ? st.chart.connection : conns[0] && conns[0].name) || "";
  const dbs = st.databases[chartSel.conn] || [];
  if (st.chart && st.chart.connection === chartSel.conn && !chartSel.db) chartSel.db = st.chart.database;
  if (!chartSel.db && dbs.length === 1) { chartSel.db = dbs[0]; post({ type: "chartSelect", connection: chartSel.conn, database: chartSel.db }); }
  return el("div", { class: "grid2 chart-picker" }, [
    labeledSelect("Connection", chartSel.conn, conns.map((c) => ({ value: c.name, label: c.name })), (v) => { chartSel = { conn: v, db: "" }; post({ type: "loadDatabases", connection: v }); render(); }),
    dbs.length
      ? labeledSelect(conn(chartSel.conn) && conn(chartSel.conn).engine === "postgres" ? "Schema" : "Database", chartSel.db, [{ value: "", label: "Choose…" }, ...dbs.map((d) => ({ value: d, label: d }))],
          (v) => { chartSel.db = v; if (v) post({ type: "chartSelect", connection: chartSel.conn, database: v }); render(); })
      : el("div", { class: "field" }, [el("label", {}, [labelText("Database"), el("div", { class: "with-btn tight" }, [el("span", { class: "muted" }, ["Not loaded"]), el("button", { class: "secondary", onclick: (e) => { e.preventDefault(); post({ type: "loadDatabases", connection: chartSel.conn }); } }, [ic("refresh"), "Load"])])])]),
  ]);
}

function chartTables(ch) {
  const isDoc = ch.engine === "mongodb";
  const q = tableFilter.trim().toLowerCase();
  const tables = (ch.tables || []).filter((t) => !q || t.name.toLowerCase().includes(q));
  const search = el("input", { type: "text", placeholder: "Filter " + (isDoc ? "collections" : "tables"), "aria-label": "Filter tables", "data-k": "tfilter", value: tableFilter, oninput: (e) => { tableFilter = e.target.value; render(); } });
  return el("div", { class: "chart-tables" }, [
    search,
    el("div", { class: "tbl-list", role: "list" }, tables.length ? tables.map((t) => el("button", { class: "tbl" + (ch.table === t.name ? " sel" : ""), role: "listitem", onclick: () => { chartTab = "data"; chartFilter = null; post({ type: "chartTable", table: t.name }); } },
      [ic(isDoc ? "symbol-namespace" : "table"), el("span", { class: "tbl-name" }, [t.name]), el("span", { class: "muted" }, [fmtCount(t.docCount)])])) : [el("p", { class: "empty-note" }, [(ch.tables || []).length ? "No matches." : "Nothing here yet."])]),
  ]);
}

function structureView(ch) {
  const d = ch.describe; if (!d) return null;
  const out = [];
  if (d.columns) {
    const fk = {}; (d.foreignKeys || []).forEach((f) => { fk[f.column] = f.refTable + "." + f.refColumn; });
    out.push(el("div", { class: "grid-wrap" }, [el("table", { class: "grid" }, [
      el("thead", {}, [el("tr", {}, ["Column", "Type", "Null", "Key"].map((h) => el("th", {}, [h])))]),
      el("tbody", {}, d.columns.map((c) => el("tr", {}, [
        el("td", {}, [c.name]), el("td", { class: "muted" }, [c.dataType]), el("td", { class: "muted" }, [c.nullable ? "yes" : "no"]),
        el("td", {}, [c.isPk ? chip("PK") : null, fk[c.name] ? chip("FK " + fk[c.name]) : null]),
      ]))),
    ])]));
  } else if (d.approxCount !== undefined) {
    out.push(el("p", { class: "hint" }, ["About " + fmtCount(d.approxCount) + " documents. Columns are the keys found in the current page."]));
  }
  out.push(el("div", { class: "sub" }, ["Indexes"]));
  out.push(...(d.indexes.length ? d.indexes.map((i) => el("div", { class: "idx" }, [el("span", { class: "row-title" }, [i.name]), i.unique ? chip("unique") : null, pre(i.detail || "")])) : [el("p", { class: "empty-note" }, ["No indexes."])]));
  return el("div", {}, out);
}

function gridView(ch) {
  const g = ch.grid; if (!g) return ch.loading ? el("p", { class: "hint" }, [ic("loading", "codicon-modifier-spin"), " Loading…"]) : null;
  const isDoc = ch.engine === "mongodb";
  const current = () => (chartFilter === null ? g.filter : chartFilter);
  const apply = () => { post({ type: "chartPage", page: 0, filter: current() }); };
  const filter = el("div", { class: "with-btn tight filter-row" }, [
    el("input", { type: "text", value: current(), "data-k": "gfilter", "aria-label": isDoc ? "Filter (JSON)" : "Filter (WHERE clause)", placeholder: isDoc ? 'Filter, e.g. {"active": true}' : "WHERE clause, e.g. total > 100",
      oninput: (e) => { chartFilter = e.target.value; }, onkeydown: (e) => { if (e.key === "Enter") apply(); } }),
    el("button", { class: "secondary", onclick: apply }, [ic("filter"), "Apply"]),
  ]);
  const head = el("thead", {}, [el("tr", {}, g.columns.map((c) => el("th", {}, [c])))]);
  const body = el("tbody", {}, g.rows.map((r) => el("tr", {}, r.map((c) => el("td", { class: c.t, title: c.v.length > 40 ? c.v.slice(0, 600) : "" }, [c.v])))));
  const first = g.page * g.pageSize + 1;
  return el("div", {}, [
    filter,
    ch.error ? notice("error", "error", [ch.error]) : null,
    g.rows.length
      ? el("div", { class: "grid-wrap" }, [el("table", { class: "grid" }, [head, body])])
      : el("p", { class: "empty-note" }, [ch.error ? "" : "No rows."]),
    el("div", { class: "pager" }, [
      el("button", { class: "secondary", ...(g.page === 0 || ch.loading ? { disabled: "disabled" } : {}), onclick: () => post({ type: "chartPage", page: g.page - 1, filter: g.filter }) }, [ic("chevron-left"), "Previous"]),
      el("span", { class: "muted" }, [g.rows.length ? "Rows " + first + "–" + (first + g.rows.length - 1) : "Page " + (g.page + 1), g.ms !== undefined ? " · " + g.ms + " ms" : ""]),
      el("button", { class: "secondary", ...(!g.hasMore || ch.loading ? { disabled: "disabled" } : {}), onclick: () => post({ type: "chartPage", page: g.page + 1, filter: g.filter }) }, ["Next", ic("chevron-right")]),
    ]),
    el("p", { class: "hint" }, ["Read-only. DBHelm runs this in a read-only transaction, so a filter cannot change anything."]),
  ]);
}

function renderChart() {
  const root = document.getElementById("root");
  const kids = [el("h1", { class: "page-title" }, ["Chart"])];
  if (!st.workspaceOpen) { kids.push(el("div", { class: "empty" }, ["Open a project folder to use DBHelm."])); root.appendChild(el("div", {}, kids)); return; }
  kids.push(...topNotices());
  if (st.broker.status !== "ready") { root.appendChild(el("div", {}, kids)); return; }
  if (!st.connections.length) { kids.push(el("p", { class: "empty-note pad" }, ["Add a connection first."])); root.appendChild(el("div", {}, kids)); return; }
  kids.push(el("div", { class: "pad" }, [chartPicker()]));
  const ch = st.chart;
  if (ch && ch.error && !ch.tables) kids.push(notice("error", "error", [ch.error]));
  if (ch && ch.tables) {
    const detail = [];
    if (!ch.table) detail.push(el("p", { class: "empty-note" }, ["Pick a " + (ch.engine === "mongodb" ? "collection" : "table") + " to see its structure and data."]));
    else {
      detail.push(el("div", { class: "chart-title" }, [el("span", { class: "row-title" }, [ch.table]), chip(ENGINE_LABEL[ch.engine] || ch.engine)]));
      detail.push(el("div", { class: "tabs", role: "tablist" }, [["data", "Data"], ["structure", "Structure"]].map(([k, label]) => el("button", { class: "tab" + (chartTab === k ? " sel" : ""), role: "tab", "aria-selected": String(chartTab === k), onclick: () => { chartTab = k; render(); } }, [label]))));
      detail.push(chartTab === "structure" ? structureView(ch) : gridView(ch));
    }
    kids.push(el("div", { class: "chart" }, [chartTables(ch), el("div", { class: "chart-detail" }, detail)]));
  } else if (ch && ch.loading) kids.push(el("p", { class: "hint pad" }, [ic("loading", "codicon-modifier-spin"), " Loading…"]));
  else kids.push(el("p", { class: "empty-note pad" }, ["Choose a database to browse."]));
  root.appendChild(el("div", {}, kids));
}

// ---------------------------------------------------------------------------------------------
// Confirm screens
// ---------------------------------------------------------------------------------------------

function renderRequestConfirm() {
  const a = st.approvals.find((x) => x.id === view.id);
  if (!a || a.status !== "pending") { back(); return; }
  const body = [];
  if (a.risk === "dangerous") body.push(notice("warn", "warning", ["Dangerous: this can remove or rewrite many rows at once. A safety snapshot is taken first, and can undo it."]));
  else if (a.kind === "restore") body.push(notice("warn", "warning", ["This replaces current data with the snapshot's. A safety snapshot is taken first."]));
  body.push(el("div", { class: "summary" }, [
    summaryField("Requested by", "An agent"), summaryField("Connection", a.connection), summaryField("Database", a.database),
    summaryField("Risk", RISK_LABEL[a.risk] || a.risk), summaryField("Reason", a.reason || "None given"),
  ]));
  body.push(pre(a.statement));
  const verb = a.kind === "restore" ? "Restore Snapshot" : a.kind === "doc" ? "Apply Change" : "Run Statement";
  confirmView("Review Change", body,
    { label: verb, icon: "check", onclick: () => { post({ type: "decide", id: a.id, approve: true }); back(); } },
    [{ label: "Refuse", icon: "close", onclick: () => { post({ type: "decide", id: a.id, approve: false }); back(); } }]);
}

function renderRestoreConfirm() {
  confirmView(view.undo ? "Undo Change" : "Restore Snapshot", [
    notice("warn", "warning", ["This replaces the current data in " + view.connection + "." + view.database + " with the snapshot's. A safety snapshot of the current data is taken first, so this can be undone."]),
    el("div", { class: "summary" }, [
      summaryField("Connection", view.connection), summaryField("Database", view.database), summaryField("Snapshot", short(view.snapshotId)),
      summaryField("Message", view.message || (view.undo ? "Safety snapshot taken before the change" : "(none)")), summaryField("Taken", view.createdAt ? relTime(view.createdAt) : "-"),
    ]),
  ], { label: "Restore Snapshot", icon: "history", onclick: () => { post({ type: "snapshotRestore", connection: view.connection, database: view.database, snapshotId: view.snapshotId }); back(); } });
}

function renderBackupRestoreConfirm() {
  const b = view.backup;
  confirmView("Restore Backup", [
    notice("warn", "warning", ["This drops the current data and restores the archive over it."]),
    el("div", { class: "summary" }, [summaryField("Connection", b.connection), summaryField("Database", b.database || "All databases"), summaryField("Size", fmtBytes(b.sizeBytes)), summaryField("Taken", relTime(b.createdAt))]),
  ], { label: "Restore Backup", icon: "history", onclick: () => { post({ type: "backupRestore", connection: b.connection, backupId: b.id }); back(); } });
}

function renderAutopilotConfirm() {
  const allow = st.settings.autopilotAllowDangerous;
  confirmView("Switch on Autopilot", [
    notice("warn", "warning", ["DBHelm will approve changes agents ask for, without asking you."]),
    el("div", { class: "summary" }, [
      summaryField("Safety snapshot", st.settings.safetySnapshot ? "Taken before every change" : "Off (see settings)"),
      summaryField("Logging", "Every change goes in the ship's log"),
      summaryField("Dangerous changes", allow ? "Approved too (your setting)" : "Still wait for you"),
      summaryField("Ends", "When you switch it off or close VS Code"),
    ]),
  ], { label: "Switch On Autopilot", icon: "compass", onclick: () => { post({ type: "setAutopilot", on: true }); back(); } });
}

function renderRemoveConfirm() {
  confirmView("Remove Connection", [
    notice("warn", "warning", ["Removes " + view.connection + " and its saved password. Your database is not touched. Snapshots already taken stay on disk."]),
  ], { label: "Remove Connection", icon: "trash", onclick: () => { post({ type: "removeConnection", name: view.connection }); back(); } });
}

// ---------------------------------------------------------------------------------------------
// Render loop
// ---------------------------------------------------------------------------------------------

function render() {
  const root = document.getElementById("root");
  const active = document.activeElement;
  const focusKey = active && active.getAttribute ? active.getAttribute("data-k") : null;
  const caret = focusKey && active.selectionStart !== undefined ? [active.selectionStart, active.selectionEnd] : null;
  const scroll = window.scrollY;
  root.innerHTML = "";
  if (!st) { root.appendChild(el("div", { class: "empty" }, ["Loading…"])); return; }
  if (view.name === "request") renderRequestConfirm();
  else if (view.name === "restore") renderRestoreConfirm();
  else if (view.name === "backupRestore") renderBackupRestoreConfirm();
  else if (view.name === "autopilot") renderAutopilotConfirm();
  else if (view.name === "remove") renderRemoveConfirm();
  else if (MODE === "config") renderConfig(); else if (MODE === "chart") renderChart(); else renderOps();
  if (focusKey) {
    const again = root.querySelector('[data-k="' + focusKey + '"]');
    if (again) { again.focus(); if (caret) try { again.setSelectionRange(caret[0], caret[1]); } catch (e) {} }
  }
  window.scrollTo(0, scroll);
  if (pendingFocus === "add") {
    pendingFocus = null; ui.sections.add = false; saveUi();
    const n = root.querySelector('[data-k="name"]'); if (n) n.focus();
  }
  // Keep the elapsed timer alive only while a job runs.
  if (st.job && !tick) tick = setInterval(() => { const n = document.getElementById("job-elapsed"); if (n && st.job) n.textContent = fmtElapsed(Date.now() - st.job.startedAt); }, 1000);
  if (!st.job && tick) { clearInterval(tick); tick = null; }
}

window.addEventListener("message", (event) => {
  const msg = event.data;
  if (msg.type === "state") {
    st = msg.state;
    // A confirm screen for a request that is no longer pending closes itself (renderRequestConfirm).
    render();
  } else if (msg.type === "picked") {
    form.uri = msg.path; render();
  } else if (msg.type === "focus" && msg.focus === "add") {
    pendingFocus = "add"; render();
  }
});

post({ type: "ready" });
