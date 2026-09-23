import * as path from "path";
import * as vscode from "vscode";
import { Access, ConnectionForm, Controller, State } from "./controller";

/** Webview -> extension. */
export type InMsg =
  | { type: "ready" }
  | { type: "refresh" }
  | { type: "openInEditor"; focus?: "add" }
  | { type: "openHelp" }
  | { type: "openSettings" }
  | { type: "chartOpen"; connection?: string }
  | { type: "chartSelect"; connection: string; database: string }
  | { type: "chartTable"; table: string }
  | { type: "chartPage"; page: number; filter: string }
  | { type: "mcpConfig"; action: "copy" | "project" }
  | { type: "restartBroker" }
  | { type: "openLog" }
  | { type: "setupSkill" }
  | { type: "configureAppLock" }
  | { type: "clearNotice" }
  | { type: "dismissResult" }
  | { type: "cancelJob" }
  | { type: "pickSqliteFile" }
  | { type: "saveConnection"; form: ConnectionForm }
  | { type: "setPassword"; name: string }
  | { type: "testConnection"; name: string }
  | { type: "updateConnection"; name: string; patch: { environment?: string; readOnly?: boolean; agentAccess?: Access } }
  | { type: "removeConnection"; name: string }
  | { type: "decide"; id: string; approve: boolean }
  | { type: "setAutopilot"; on: boolean }
  | { type: "loadDatabases"; connection: string }
  | { type: "loadSnapshots"; connection: string; database: string }
  | { type: "snapshotCreate"; connection: string; database: string; message?: string }
  | { type: "snapshotRestore"; connection: string; database: string; snapshotId: string }
  | { type: "backupCreate"; connection: string; database: string }
  | { type: "backupRestore"; connection: string; backupId: string }
  | { type: "backupDelete"; id: string };

/** Extension -> webview. */
export type OutMsg =
  | { type: "state"; state: State }
  | { type: "picked"; path: string }
  | { type: "focus"; focus: "add" };

const NONCE_CHARS = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
function getNonce(): string {
  let s = "";
  for (let i = 0; i < 32; i++) s += NONCE_CHARS.charAt(Math.floor(Math.random() * NONCE_CHARS.length));
  return s;
}

/** The HTML shell shared by the sidebar and the editor tab. All logic lives in media/webview/app.js. */
export type Surface = "sidebar" | "config" | "chart";

export function renderHtml(surface: Surface, webview: vscode.Webview, uris: { codicon: vscode.Uri; css: vscode.Uri; js: vscode.Uri }): string {
  const nonce = getNonce();
  const csp = `default-src 'none'; font-src ${webview.cspSource}; style-src ${webview.cspSource}; script-src 'nonce-${nonce}';`;
  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta http-equiv="Content-Security-Policy" content="${csp}">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<link href="${uris.codicon}" rel="stylesheet">
<link href="${uris.css}" rel="stylesheet">
<title>DBHelm</title>
</head>
<body class="${surface === "sidebar" ? "" : "in-editor"}">
<div id="root"><div class="empty">Loading…</div></div>
<script nonce="${nonce}">window.SURFACE = "${surface}";</script>
<script nonce="${nonce}" src="${uris.js}"></script>
</body>
</html>`;
}

export class DbhelmPanel implements vscode.WebviewViewProvider {
  public static readonly viewType = "dbhelmPanel";
  private webviews = new Set<vscode.Webview>();
  private editorPanel?: vscode.WebviewPanel;
  private chartPanel?: vscode.WebviewPanel;
  private sidebarView?: vscode.WebviewView;
  private timer?: NodeJS.Timeout;
  private readonly sub: vscode.Disposable;

  constructor(private readonly context: vscode.ExtensionContext, private readonly controller: Controller) {
    this.sub = controller.onDidChange(() => this.schedulePush());
    controller.panelVisible = () => !!this.sidebarView?.visible || !!this.editorPanel?.visible || !!this.chartPanel?.visible;
  }

  private get roots(): vscode.Uri[] {
    return [
      vscode.Uri.joinPath(this.context.extensionUri, "media", "webview"),
      vscode.Uri.joinPath(this.context.extensionUri, "node_modules", "@vscode", "codicons", "dist"),
    ];
  }

  private html(webview: vscode.Webview, surface: Surface): string {
    return renderHtml(surface, webview, {
      codicon: webview.asWebviewUri(vscode.Uri.joinPath(this.context.extensionUri, "node_modules", "@vscode", "codicons", "dist", "codicon.css")),
      css: webview.asWebviewUri(vscode.Uri.joinPath(this.context.extensionUri, "media", "webview", "app.css")),
      js: webview.asWebviewUri(vscode.Uri.joinPath(this.context.extensionUri, "media", "webview", "app.js")),
    });
  }

  private attach(webview: vscode.Webview, surface: Surface): vscode.Disposable {
    webview.options = { enableScripts: true, localResourceRoots: this.roots };
    webview.html = this.html(webview, surface);
    this.webviews.add(webview);
    const sub = webview.onDidReceiveMessage((msg: InMsg) => void this.handle(msg, webview));
    return new vscode.Disposable(() => { sub.dispose(); this.webviews.delete(webview); });
  }

  resolveWebviewView(view: vscode.WebviewView): void {
    this.sidebarView = view;
    const sub = this.attach(view.webview, "sidebar");
    view.onDidDispose(() => { sub.dispose(); if (this.sidebarView === view) this.sidebarView = undefined; });
    view.onDidChangeVisibility(() => { if (view.visible) void this.controller.refreshAll(); });
  }

  openInEditor(focus?: "add"): void {
    if (this.editorPanel) {
      this.editorPanel.reveal(undefined, false);
      if (focus) void this.editorPanel.webview.postMessage({ type: "focus", focus } satisfies OutMsg);
      return;
    }
    const panel = vscode.window.createWebviewPanel(`${DbhelmPanel.viewType}.editor`, "DBHelm: Connections and Agent Access", vscode.ViewColumn.Active, {
      enableScripts: true, retainContextWhenHidden: true, localResourceRoots: this.roots,
    });
    panel.iconPath = vscode.Uri.joinPath(this.context.extensionUri, "media", "icon.png");
    this.editorPanel = panel;
    const sub = this.attach(panel.webview, "config");
    panel.onDidDispose(() => { sub.dispose(); this.editorPanel = undefined; });
    if (focus) setTimeout(() => void panel.webview.postMessage({ type: "focus", focus } satisfies OutMsg), 400);
  }

  /** The Chart: read-only schema browser and data grid, in its own editor tab. */
  openChart(connection?: string): void {
    const start = () => { if (connection) void this.controller.loadDatabases(connection); };
    if (this.chartPanel) { this.chartPanel.reveal(undefined, false); start(); return; }
    const panel = vscode.window.createWebviewPanel(`${DbhelmPanel.viewType}.chart`, "DBHelm: Chart", vscode.ViewColumn.Active, {
      enableScripts: true, retainContextWhenHidden: true, localResourceRoots: this.roots,
    });
    panel.iconPath = vscode.Uri.joinPath(this.context.extensionUri, "media", "icon.png");
    this.chartPanel = panel;
    const sub = this.attach(panel.webview, "chart");
    panel.onDidDispose(() => { sub.dispose(); this.chartPanel = undefined; });
    start();
  }

  private schedulePush(): void {
    if (this.timer) return;
    this.timer = setTimeout(() => { this.timer = undefined; this.push(); }, 40);
  }

  private push(): void {
    const msg: OutMsg = { type: "state", state: this.controller.state };
    for (const w of this.webviews) void w.postMessage(msg);
  }

  private async handle(msg: InMsg, from: vscode.Webview): Promise<void> {
    const c = this.controller;
    switch (msg.type) {
      case "ready": void from.postMessage({ type: "state", state: c.state } satisfies OutMsg); void c.refreshAll(); break;
      case "refresh": await c.refreshAll(); break;
      case "openInEditor": this.openInEditor(msg.focus); break;
      case "openHelp": void vscode.commands.executeCommand("markdown.showPreview", vscode.Uri.file(path.join(this.context.extensionPath, "media", "HELP.md"))); break;
      case "openLog": c.openLog(); break;
      case "chartOpen": this.openChart(msg.connection); break;
      case "chartSelect": await c.chartSelect(msg.connection, msg.database); break;
      case "chartTable": await c.chartTable(msg.table); break;
      case "chartPage": await c.chartPage(msg.page, msg.filter); break;
      case "openSettings": void vscode.commands.executeCommand("workbench.action.openSettings", "@ext:IshanKulkarni.dbhelm"); break;
      case "restartBroker": await c.restart(); break;
      case "mcpConfig": await vscode.commands.executeCommand(msg.action === "copy" ? "dbhelm.copyMcpConfig" : "dbhelm.addMcpToProject"); break;
      case "setupSkill": await vscode.commands.executeCommand("dbhelm.setupAgentSkill"); break;
      case "configureAppLock": await vscode.commands.executeCommand("dbhelm.configureAppLock"); break;
      case "clearNotice": c.clearNotice(); break;
      case "dismissResult": c.dismissResult(); break;
      case "cancelJob": c.cancelJob(); break;
      case "pickSqliteFile": {
        const picked = await vscode.window.showOpenDialog({ canSelectMany: false, title: "Choose a SQLite database file", filters: { "SQLite databases": ["db", "sqlite", "sqlite3"], "All files": ["*"] } });
        if (picked?.[0]) void from.postMessage({ type: "picked", path: picked[0].fsPath } satisfies OutMsg);
        break;
      }
      case "saveConnection": await c.saveConnection(msg.form); break;
      case "setPassword": await c.setPassword(msg.name); break;
      case "testConnection": await c.testConnection(msg.name); break;
      case "updateConnection": await c.updateConnection(msg.name, msg.patch); break;
      case "removeConnection": await c.removeConnection(msg.name); break;
      case "decide": await c.decide(msg.id, msg.approve); break;
      case "setAutopilot": await c.setAutopilot(msg.on); break;
      case "loadDatabases": await c.loadDatabases(msg.connection); break;
      case "loadSnapshots": await c.loadSnapshots(msg.connection, msg.database); break;
      case "snapshotCreate": await c.snapshotCreate(msg.connection, msg.database, msg.message); break;
      case "snapshotRestore": await c.snapshotRestore(msg.connection, msg.database, msg.snapshotId); break;
      case "backupCreate": await c.backupCreate(msg.connection, msg.database); break;
      case "backupRestore": await c.backupRestore(msg.connection, msg.backupId); break;
      case "backupDelete": await c.backupDelete(msg.id); break;
    }
  }

  dispose(): void {
    this.sub.dispose();
    if (this.timer) clearTimeout(this.timer);
    this.editorPanel?.dispose();
    this.chartPanel?.dispose();
  }
}
