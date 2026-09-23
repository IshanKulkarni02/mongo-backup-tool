import * as fs from "fs";
import * as vscode from "vscode";
import { isAgentSkillInstalled, setupAgentSkill } from "./agentSkill";
import { ensureAuthorized, hasAppPassword, configuredAuth } from "./auth";
import { Broker, BrokerEvent, Envelope } from "./broker";
import { installedBinaryPath, resolveBinary } from "./binary";
import { ensureDotFolderIgnored, logPath } from "./config";
import { mcpSnippet, writeProjectMcp } from "./mcpConfig";
import { docsToGrid, Grid, pageSql, PAGE_SIZE, sqlToGrid } from "./chart";

export type Access = "off" | "read" | "write";
export interface Caps { sql: boolean; documents: boolean; aggregation: boolean; foreignKeys: boolean; snapshots: boolean }
export interface ConnView { name: string; engine: string; capabilities: Caps; access: Access; environment?: string; readOnly?: boolean }
export interface Approval {
  id: string; kind: "sql" | "doc" | "restore"; connection: string; database: string; statement: string;
  risk: "none" | "confirm" | "dangerous"; reason?: string; status: string; decidedBy?: string;
  createdAt: string; decidedAt?: string; result?: any; error?: { code: string; message: string; hint?: string };
}
export interface LogEntry { ts: string; actor: string; event: string; connection?: string; database?: string; statement?: string; requestId?: string; outcome?: string; code?: string; detail?: string }
export interface Snap { id: string; message: string; createdAt: string; docCount: number; tags?: string[] }
export interface Backup { id: string; connection: string; database: string; fileName: string; sizeBytes: number; createdAt: string }
export interface Autopilot { on: boolean; allowDangerous: boolean; until?: string }
export interface JobView { kind: string; label: string; startedAt: number }
export interface ResultView {
  ok: boolean; text: string;
  undo?: { connection: string; database: string; snapshotId: string };
}

export interface ChartState {
  connection: string;
  database: string;
  engine: string;
  tables?: { name: string; docCount: number; storageSize: number }[];
  table?: string;
  describe?: {
    columns?: { name: string; dataType: string; nullable: boolean; isPk: boolean }[];
    foreignKeys?: { column: string; refTable: string; refColumn: string }[];
    indexes: { name: string; detail: string; unique?: boolean }[];
    approxCount?: number;
  };
  grid?: Grid & { page: number; pageSize: number; hasMore: boolean; filter: string; ms?: number };
  loading?: boolean;
  error?: string;
}

export interface State {
  workspaceOpen: boolean;
  workspaceName?: string;
  broker: { status: string; error?: string };
  connections: ConnView[];
  approvals: Approval[];
  autopilot: Autopilot;
  activity: LogEntry[];
  backups: Backup[];
  snapshots: Record<string, Snap[]>;
  databases: Record<string, string[]>;
  job?: JobView;
  result?: ResultView;
  chart?: ChartState;
  skillInstalled: boolean;
  binaryPath: string;
  settings: { autopilotAllowDangerous: boolean; safetySnapshot: boolean };
  auth: { required: boolean; hasPassword: boolean };
  notice?: { kind: "info" | "warn" | "error" | "ok"; text: string };
}

export interface ConnectionForm {
  name: string; engine: string; uri: string; environment: string; readOnly: boolean; agentAccess: Access;
}

const PASSWORD_IN_URI = /^[a-z][a-z0-9+.\-]*:\/\/[^/@\s:]+:[^/@\s]+@/i;
const DSN_PASSWORD = /^[^:@/\s]+:[^@\s]+@(?:tcp|unix)\(/i;

export class Controller implements vscode.Disposable {
  readonly broker: Broker;
  private root?: string;
  private abort?: AbortController;
  private lastBinary = "dbhelm";
  private readonly changed = new vscode.EventEmitter<void>();
  readonly onDidChange = this.changed.event;
  panelVisible: () => boolean = () => false;

  state: State = {
    workspaceOpen: false, broker: { status: "stopped" }, connections: [], approvals: [], autopilot: { on: false, allowDangerous: false },
    activity: [], backups: [], snapshots: {}, databases: {}, skillInstalled: false, binaryPath: "",
    settings: { autopilotAllowDangerous: false, safetySnapshot: true }, auth: { required: true, hasPassword: false },
  };

  constructor(private readonly context: vscode.ExtensionContext, private readonly output: vscode.OutputChannel) {
    this.broker = new Broker(output);
    this.broker.onStatus(() => { this.syncBroker(); this.emit(); });
    this.broker.onEvent((e) => void this.onBrokerEvent(e));
  }

  get workspaceRoot(): string | undefined { return this.root; }

  private emit(): void { this.changed.fire(); }

  private syncBroker(): void {
    this.state.broker = { status: this.broker.status, error: this.broker.error };
  }

  private syncSettings(): void {
    const cfg = vscode.workspace.getConfiguration("dbhelm");
    this.state.settings = { autopilotAllowDangerous: cfg.get<boolean>("autopilotAllowDangerous", false), safetySnapshot: cfg.get<boolean>("safetySnapshot", true) };
    this.state.auth.required = configuredAuth().require;
  }

  async start(): Promise<void> {
    const folder = vscode.workspace.workspaceFolders?.[0];
    this.root = folder?.uri.fsPath;
    this.state.workspaceOpen = !!folder;
    this.state.workspaceName = folder?.name;
    this.syncSettings();
    this.state.auth.hasPassword = await hasAppPassword(this.context);
    if (!this.root) { this.emit(); return; }
    ensureDotFolderIgnored(this.root);
    const binary = resolveBinary(this.context);
    this.lastBinary = binary;
    this.state.binaryPath = binary === "dbhelm" ? "dbhelm" : (binary === installedBinaryPath() ? binary : binary);
    this.state.skillInstalled = isAgentSkillInstalled(this.root);
    this.emit();
    await this.broker.start(binary, this.root);
    if (this.broker.status === "ready") await this.refreshAll();
    this.emit();
  }

  async restart(): Promise<void> {
    this.state.notice = undefined;
    await this.start();
  }

  // ---- data ----

  private async fetch<T>(method: string, params?: unknown): Promise<Envelope<T>> {
    return this.broker.call<T>(method, params);
  }

  async refreshAll(): Promise<void> {
    this.syncSettings();
    const [conns, approvals, auto, log, backups] = await Promise.all([
      this.fetch<ConnView[]>("connections.list"),
      this.fetch<Approval[]>("approvals.list"),
      this.fetch<Autopilot>("autopilot.get"),
      this.fetch<LogEntry[]>("logbook.tail", { limit: 40 }),
      this.fetch<Backup[]>("backups.list"),
    ]);
    if (conns.ok) this.state.connections = conns.data ?? [];
    if (approvals.ok) this.state.approvals = approvals.data ?? [];
    if (auto.ok && auto.data) this.state.autopilot = auto.data;
    if (log.ok) this.state.activity = (log.data ?? []).slice().reverse();
    if (backups.ok) this.state.backups = backups.data ?? [];
    if (this.root) this.state.skillInstalled = isAgentSkillInstalled(this.root);
    this.emit();
  }

  private async onBrokerEvent(e: BrokerEvent): Promise<void> {
    switch (e.type) {
      case "approval.pending": {
        await this.refreshApprovals();
        const a = e.data as Approval;
        if (!this.panelVisible()) {
          const pick = await vscode.window.showInformationMessage(`DBHelm: an agent asked to change ${a.connection}.${a.database}.`, "Review");
          if (pick === "Review") void vscode.commands.executeCommand("workbench.view.extension.dbhelm");
        }
        break;
      }
      case "approval.updated":
        await this.refreshApprovals();
        break;
      case "autopilot":
        this.state.autopilot = e.data as Autopilot;
        this.emit();
        break;
      case "activity":
        this.state.activity = [e.data as LogEntry, ...this.state.activity].slice(0, 40);
        this.emit();
        break;
    }
  }

  private async refreshApprovals(): Promise<void> {
    const r = await this.fetch<Approval[]>("approvals.list");
    if (r.ok) { this.state.approvals = r.data ?? []; this.emit(); }
  }

  // ---- notices / results ----

  setNotice(kind: "info" | "warn" | "error" | "ok", text: string): void { this.state.notice = { kind, text }; this.emit(); }
  clearNotice(): void { this.state.notice = undefined; this.emit(); }
  dismissResult(): void { this.state.result = undefined; this.emit(); }
  private failText(env: Envelope): string {
    const e = env.error;
    return e ? `${e.message}${e.hint ? ` ${e.hint}` : ""}` : "Something went wrong.";
  }

  /** One long operation at a time, with progress and Cancel. */
  private async runJob(kind: string, label: string, fn: (signal: AbortSignal) => Promise<Envelope>, onOk: (env: Envelope) => ResultView): Promise<void> {
    if (this.state.job) { void vscode.window.showWarningMessage("DBHelm: another operation is still running."); return; }
    this.abort = new AbortController();
    this.state.job = { kind, label, startedAt: Date.now() };
    this.state.result = undefined;
    this.emit();
    try {
      const env = await fn(this.abort.signal);
      this.state.result = env.ok ? onOk(env) : { ok: false, text: env.error?.code === "CANCELED" ? `${label} was canceled.` : this.failText(env) };
    } finally {
      this.state.job = undefined;
      this.abort = undefined;
      await this.refreshAll();
    }
  }

  cancelJob(): void { this.abort?.abort(); }

  // ---- connections ----

  async saveConnection(form: ConnectionForm): Promise<boolean> {
    const name = form.name.trim();
    const uri = form.uri.trim();
    if (!name || !uri) { this.setNotice("error", "A connection needs a name and a URI (or a file path for SQLite)."); return false; }
    if (PASSWORD_IN_URI.test(uri) || DSN_PASSWORD.test(uri)) {
      this.setNotice("error", "Remove the password from the URI (keep user@host). DBHelm asks for it separately and stores it in your system keychain.");
      return false;
    }
    if (form.agentAccess !== "off" && !(await ensureAuthorized(this.context, "Let agents use a database connection"))) return false;
    const env = await this.fetch("connection.add", { name, uri, engine: form.engine, environment: form.environment, readOnly: form.readOnly, agentAccess: form.agentAccess });
    if (!env.ok) { this.setNotice("error", this.failText(env)); return false; }
    const needsPassword = form.engine !== "sqlite" && (/^[a-z][a-z0-9+.\-]*:\/\/[^/@\s:]+@/i.test(uri) || /^[^:@/\s]+@(?:tcp|unix)\(/i.test(uri));
    if (needsPassword) await this.setPassword(name, `Password for the user in "${name}" (leave empty if it has none)`);
    this.setNotice("ok", `Saved connection "${name}".`);
    await this.refreshAll();
    return true;
  }

  async setPassword(name: string, prompt?: string): Promise<void> {
    if (!(await ensureAuthorized(this.context, `Change the saved login for ${name}`))) return;
    const pw = await vscode.window.showInputBox({ title: `DBHelm: Password for ${name}`, prompt: prompt ?? "Stored in your system keychain, never in a file", password: true, ignoreFocusOut: true });
    if (pw === undefined || pw === "") return;
    const env = await this.fetch("connection.setPassword", { name, password: pw });
    this.setNotice(env.ok ? "ok" : "error", env.ok ? `Password saved for "${name}".` : this.failText(env));
  }

  async testConnection(name: string): Promise<void> {
    this.setNotice("info", `Testing ${name}…`);
    const env = await this.fetch<string[]>("connection.test", { name });
    if (env.ok) {
      this.state.databases[name] = env.data ?? [];
      this.setNotice("ok", `${name}: connected. ${env.data?.length ?? 0} database(s) found.`);
    } else this.setNotice("error", `${name}: ${this.failText(env)}`);
  }

  async updateConnection(name: string, patch: { environment?: string; readOnly?: boolean; agentAccess?: Access }): Promise<void> {
    const current = this.state.connections.find((c) => c.name === name);
    const raising = patch.agentAccess && patch.agentAccess !== "off" && patch.agentAccess !== current?.access;
    if (raising && !(await ensureAuthorized(this.context, `Let agents use "${name}"`))) { this.emit(); return; }
    const env = await this.fetch<ConnView[]>("connection.update", { name, ...patch });
    if (env.ok && env.data) this.state.connections = env.data;
    else this.setNotice("error", this.failText(env));
    this.emit();
  }

  async removeConnection(name: string): Promise<void> {
    if (!(await ensureAuthorized(this.context, `Remove the saved connection "${name}"`))) return;
    const env = await this.fetch("connection.remove", { name });
    this.setNotice(env.ok ? "ok" : "error", env.ok ? `Removed "${name}".` : this.failText(env));
    await this.refreshAll();
  }

  // ---- approvals + Autopilot ----

  async decide(id: string, approve: boolean): Promise<void> {
    if (approve && !(await ensureAuthorized(this.context, "Approve a database change requested by an agent"))) return;
    const env = await this.fetch("approvals.decide", { id, approve });
    if (!env.ok) this.setNotice("error", this.failText(env));
    await this.refreshApprovals();
  }

  async setAutopilot(on: boolean): Promise<void> {
    if (on && !(await ensureAuthorized(this.context, "Switch on Autopilot: DBHelm will approve agent changes without asking"))) return;
    const cfg = vscode.workspace.getConfiguration("dbhelm");
    const env = await this.fetch<Autopilot>("autopilot.set", {
      on, allowDangerous: cfg.get<boolean>("autopilotAllowDangerous", false), minutes: cfg.get<number>("autopilotMinutes", 0),
    });
    if (env.ok && env.data) this.state.autopilot = env.data;
    else this.setNotice("error", this.failText(env));
    this.emit();
  }

  // ---- snapshots + backups ----

  async loadDatabases(connection: string): Promise<void> {
    if (this.state.databases[connection]) return;
    const env = await this.fetch<string[]>("databases", { connection });
    if (env.ok) { this.state.databases[connection] = env.data ?? []; this.emit(); }
    else this.setNotice("error", `${connection}: ${this.failText(env)}`);
  }

  async loadSnapshots(connection: string, database: string): Promise<void> {
    const env = await this.fetch<Snap[]>("snapshot.list", { connection, database });
    this.state.snapshots[`${connection}/${database}`] = env.ok ? env.data ?? [] : [];
    if (!env.ok) this.setNotice("error", this.failText(env));
    this.emit();
  }

  async snapshotCreate(connection: string, database: string, message?: string): Promise<void> {
    await this.runJob("snapshot", `Snapshot of ${connection}.${database}`,
      (signal) => this.broker.call("snapshot.create", { connection, database, message: message || "snapshot from VS Code" }, { signal }),
      (env) => ({ ok: true, text: `Snapshot ${String(env.data?.id ?? "").slice(0, 8)} saved for ${connection}.${database}.` }));
    await this.loadSnapshots(connection, database);
  }

  async snapshotRestore(connection: string, database: string, snapshotId: string): Promise<void> {
    if (!(await ensureAuthorized(this.context, `Restore ${connection}.${database} from a snapshot`))) return;
    await this.runJob("restore", `Restore of ${connection}.${database}`,
      (signal) => this.broker.call("snapshots.restore", { connection, database, snapshotId }, { signal }),
      (env) => {
        const safety = env.data?.safetySnapshotId as string | undefined;
        return {
          ok: true,
          text: `Restored ${connection}.${database} from ${snapshotId.slice(0, 8)}.${safety ? ` A safety snapshot (${safety.slice(0, 8)}) was taken first.` : ""}`,
          undo: safety ? { connection, database, snapshotId: safety } : undefined,
        };
      });
    await this.loadSnapshots(connection, database);
  }

  async backupCreate(connection: string, database: string): Promise<void> {
    await this.runJob("backup", `Backup of ${connection}${database ? `.${database}` : ""}`,
      (signal) => this.broker.call("backups.create", { connection, database }, { signal }),
      () => ({ ok: true, text: `Backup of ${connection}${database ? `.${database}` : ""} saved.` }));
  }

  async backupRestore(connection: string, backupId: string): Promise<void> {
    if (!(await ensureAuthorized(this.context, "Restore a backup over the current database"))) return;
    await this.runJob("restore", "Backup restore",
      (signal) => this.broker.call("backups.restore", { connection, backupId }, { signal }),
      () => ({ ok: true, text: `Backup restored to ${connection}.` }));
  }

  async backupDelete(id: string): Promise<void> {
    const env = await this.fetch("backups.delete", { id });
    this.setNotice(env.ok ? "ok" : "error", env.ok ? "Backup deleted." : this.failText(env));
    await this.refreshAll();
  }

  // ---- Chart: read-only schema browser and data grid ----

  /** Picks the database to browse and loads its tables. */
  async chartSelect(connection: string, database: string): Promise<void> {
    const c = this.state.connections.find((x) => x.name === connection);
    if (!c) return;
    this.state.chart = { connection, database, engine: c.engine, loading: true };
    this.emit();
    const env = await this.fetch<any[]>("schema", { connection, database });
    this.state.chart = env.ok
      ? { connection, database, engine: c.engine, tables: (env.data ?? []).map((t) => ({ name: t.name, docCount: t.docCount ?? 0, storageSize: t.storageSize ?? 0 })) }
      : { connection, database, engine: c.engine, error: this.failText(env) };
    this.emit();
  }

  async chartTable(table: string): Promise<void> {
    const ch = this.state.chart;
    if (!ch) return;
    Object.assign(ch, { table, describe: undefined, grid: undefined, error: undefined, loading: true });
    this.emit();
    const env = await this.fetch<any>("describe", ch.engine === "mongodb" ? { connection: ch.connection, database: ch.database, collection: table } : { connection: ch.connection, database: ch.database, table });
    if (!env.ok) { ch.loading = false; ch.error = this.failText(env); this.emit(); return; }
    if (ch.engine === "mongodb") {
      ch.describe = { indexes: (env.data.indexes ?? []).map((i: any) => ({ name: i.name, detail: i.keysJson, unique: i.unique })), approxCount: env.data.approxCount };
    } else {
      const sch = env.data.schema ?? {};
      ch.describe = {
        columns: sch.columns ?? [], foreignKeys: sch.foreignKeys ?? [],
        indexes: (env.data.indexes ?? []).map((i: any) => ({ name: i.name, detail: i.ddl })),
      };
    }
    await this.chartPage(0, "");
  }

  async chartPage(page: number, filter: string): Promise<void> {
    const ch = this.state.chart;
    if (!ch?.table) return;
    ch.loading = true;
    ch.error = undefined;
    this.emit();
    const started = Date.now();
    const params = ch.engine === "mongodb"
      ? { connection: ch.connection, database: ch.database, collection: ch.table, filter: filter.trim() || undefined, skip: page * PAGE_SIZE, limit: PAGE_SIZE + 1, sort: '{"_id":1}' }
      : { connection: ch.connection, database: ch.database, sql: pageSql(ch.engine, ch.database, ch.table, filter, page), maxRows: PAGE_SIZE + 1 };
    const env = await this.fetch<any>("query", params);
    ch.loading = false;
    if (!env.ok) { ch.error = this.failText(env); ch.grid = ch.grid && { ...ch.grid, filter }; this.emit(); return; }
    const grid = ch.engine === "mongodb" ? docsToGrid(env.data.documents ?? []) : sqlToGrid(env.data);
    const hasMore = grid.rows.length > PAGE_SIZE;
    ch.grid = { columns: grid.columns, rows: grid.rows.slice(0, PAGE_SIZE), page, pageSize: PAGE_SIZE, hasMore, filter, ms: Date.now() - started };
    this.emit();
  }

  // ---- agent skill + log ----

  async installSkill(overwrite: boolean): Promise<string> {
    if (!this.root) throw new Error("Open a project folder first.");
    const binary = this.lastBinary === "dbhelm" ? "dbhelm" : this.lastBinary;
    const res = setupAgentSkill(this.context.extensionPath, this.root, binary, overwrite);
    this.state.skillInstalled = isAgentSkillInstalled(this.root);
    this.emit();
    const changed = res.files.filter((f) => f.action === "created" || f.action === "updated");
    const skipped = res.files.filter((f) => f.action === "skipped-differs");
    if (skipped.length) return `Kept your edited ${skipped.map((f) => f.path).join(", ")}. Run again and choose Replace to overwrite.`;
    return changed.length ? `Agent skill installed (${changed.length} file(s)). Agents now use ${binary}.` : "Agent skill is already up to date.";
  }

  /** The MCP config for this project, ready to paste into a client's settings. */
  mcpConfigText(): string {
    if (!this.root) throw new Error("Open a project folder first.");
    return mcpSnippet(this.lastBinary, this.root);
  }

  /** Adds DBHelm to the project's .mcp.json (Claude Code reads it; other clients accept the same shape). */
  addMcpToProject(overwrite: boolean): { path: string; action: string } {
    if (!this.root) throw new Error("Open a project folder first.");
    return writeProjectMcp(this.root, this.lastBinary, overwrite);
  }

  openLog(): void {
    if (!this.root) return;
    const p = logPath(this.root);
    if (!fs.existsSync(p)) { void vscode.window.showInformationMessage("DBHelm: the ship's log is empty. Nothing has happened yet."); return; }
    void vscode.window.showTextDocument(vscode.Uri.file(p));
  }

  dispose(): void {
    this.broker.dispose();
    this.changed.dispose();
  }
}
