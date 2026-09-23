import * as vscode from "vscode";
import { configureAppLock, lockNow, maybeShowFirstRunPrompt } from "./auth";
import { ConnView, Controller, Snap } from "./controller";
import { DbhelmPanel } from "./panel";

let controller: Controller | undefined;

export function activate(context: vscode.ExtensionContext): void {
  const output = vscode.window.createOutputChannel("DBHelm");
  const ctl = new Controller(context, output);
  controller = ctl;
  const panel = new DbhelmPanel(context, ctl);

  // One status item: the Bridge at a glance. Autopilot is loud on purpose.
  const status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 99);
  status.command = "dbhelm.openStatus";
  const renderStatus = () => {
    const s = ctl.state;
    const pending = s.approvals.filter((a) => a.status === "pending").length;
    status.backgroundColor = undefined;
    if (s.broker.status === "error") { status.text = "$(warning) DBHelm"; status.tooltip = s.broker.error; status.backgroundColor = new vscode.ThemeColor("statusBarItem.errorBackground"); }
    else if (s.autopilot.on) { status.text = "$(compass) Autopilot"; status.tooltip = "DBHelm is approving agent changes on your behalf. Click to review."; status.backgroundColor = new vscode.ThemeColor("statusBarItem.warningBackground"); }
    else if (pending > 0) { status.text = `$(bell) DBHelm: ${pending} waiting`; status.tooltip = "An agent is waiting for your decision."; status.backgroundColor = new vscode.ThemeColor("statusBarItem.warningBackground"); }
    else { status.text = "$(database) DBHelm"; status.tooltip = "DBHelm: Manual Helm. Agents ask before changing data."; }
    if (s.workspaceOpen) status.show(); else status.hide();
  };
  ctl.onDidChange(renderStatus);
  renderStatus();

  const cmd = (id: string, fn: (...args: any[]) => unknown) => vscode.commands.registerCommand(id, fn);

  context.subscriptions.push(
    output, ctl, panel, status,
    vscode.window.registerWebviewViewProvider(DbhelmPanel.viewType, panel, { webviewOptions: { retainContextWhenHidden: true } }),
    cmd("dbhelm.openStatus", () => vscode.commands.executeCommand("workbench.view.extension.dbhelm")),
    cmd("dbhelm.addConnection", () => panel.openInEditor("add")),
    cmd("dbhelm.openInEditor", () => panel.openInEditor()),
    cmd("dbhelm.openChart", () => panel.openChart()),
    cmd("dbhelm.openLog", () => ctl.openLog()),
    cmd("dbhelm.openHelp", () => vscode.commands.executeCommand("markdown.showPreview", vscode.Uri.joinPath(context.extensionUri, "media", "HELP.md"))),
    cmd("dbhelm.restartBroker", async () => { await ctl.restart(); }),
    cmd("dbhelm.configureAppLock", () => configureAppLock(context)),
    cmd("dbhelm.lockNow", () => { lockNow(); void vscode.window.showInformationMessage("DBHelm: locked. The next approval, restore or credential change will ask for authentication."); }),

    cmd("dbhelm.toggleAutopilot", async () => {
      if (ctl.state.autopilot.on) { await ctl.setAutopilot(false); return; }
      const allow = ctl.state.settings.autopilotAllowDangerous;
      const pick = await vscode.window.showWarningMessage(
        "Switch on Autopilot?",
        { modal: true, detail: `DBHelm will approve changes agents ask for, without asking you. A safety snapshot is still taken first and everything is logged in the ship's log. ${allow ? "Dangerous changes (DROP, TRUNCATE, ALTER, DELETE or UPDATE without WHERE, restores) are included because you allowed it in settings." : "Dangerous changes (DROP, TRUNCATE, ALTER, DELETE or UPDATE without WHERE, restores) still wait for you."} Autopilot switches off when you close VS Code.` },
        "Switch On Autopilot"
      );
      if (pick) await ctl.setAutopilot(true);
    }),

    cmd("dbhelm.setupAgentSkill", async () => {
      try {
        let msg = await ctl.installSkill(false);
        if (msg.startsWith("Kept your edited")) {
          const pick = await vscode.window.showWarningMessage(msg, "Replace", "Keep Mine");
          if (pick === "Replace") msg = await ctl.installSkill(true);
        }
        ctl.setNotice("ok", msg);
        void vscode.window.showInformationMessage(`DBHelm: ${msg}`);
      } catch (err) {
        void vscode.window.showErrorMessage(`DBHelm: ${err instanceof Error ? err.message : String(err)}`);
      }
    }),

    cmd("dbhelm.copyMcpConfig", async () => {
      try {
        await vscode.env.clipboard.writeText(ctl.mcpConfigText());
        void vscode.window.showInformationMessage("DBHelm: MCP config copied. Paste it into your MCP client's settings (Cursor, Codex, Claude Desktop, ...).");
      } catch (err) { void vscode.window.showErrorMessage(`DBHelm: ${err instanceof Error ? err.message : String(err)}`); }
    }),

    cmd("dbhelm.addMcpToProject", async () => {
      try {
        let r = ctl.addMcpToProject(false);
        if (r.action === "skipped-differs") {
          const pick = await vscode.window.showWarningMessage("DBHelm: .mcp.json already has a different \"dbhelm\" server.", "Replace", "Keep Mine");
          if (pick === "Replace") r = ctl.addMcpToProject(true);
        }
        const text = r.action === "unchanged" ? ".mcp.json already lists DBHelm." : r.action === "skipped-differs" ? "Kept your .mcp.json." : `${r.action === "created" ? "Created" : "Updated"} .mcp.json. Restart your agent to load DBHelm.`;
        ctl.setNotice("ok", text);
        void vscode.window.showInformationMessage(`DBHelm: ${text}`);
      } catch (err) { void vscode.window.showErrorMessage(`DBHelm: ${err instanceof Error ? err.message : String(err)}`); }
    }),

    cmd("dbhelm.takeSnapshot", async () => {
      const target = await pickTarget(ctl, (c) => c.capabilities.snapshots);
      if (!target) return;
      const message = await vscode.window.showInputBox({ title: "DBHelm: Snapshot Message", prompt: "Optional. Say what this checkpoint is for.", ignoreFocusOut: true });
      if (message === undefined) return;
      await ctl.snapshotCreate(target.connection, target.database, message || undefined);
    }),

    cmd("dbhelm.restoreSnapshot", async () => {
      const target = await pickTarget(ctl, (c) => c.capabilities.snapshots);
      if (!target) return;
      await ctl.loadSnapshots(target.connection, target.database);
      const snaps: Snap[] = ctl.state.snapshots[`${target.connection}/${target.database}`] ?? [];
      if (!snaps.length) { void vscode.window.showInformationMessage(`DBHelm: ${target.connection}.${target.database} has no snapshots yet.`); return; }
      const pick = await vscode.window.showQuickPick(
        snaps.map((s) => ({ label: s.message || "(no message)", description: `${s.id.slice(0, 8)}  ${new Date(s.createdAt).toLocaleString()}`, detail: `${s.docCount} rows/documents`, id: s.id })),
        { title: `DBHelm: Restore ${target.connection}.${target.database}`, placeHolder: "Choose the snapshot to restore" }
      );
      if (!pick) return;
      const ok = await vscode.window.showWarningMessage(
        `Restore ${target.connection}.${target.database} from ${pick.id.slice(0, 8)}?`,
        { modal: true, detail: "This replaces the current data with the snapshot's. A safety snapshot of the current data is taken first, so you can undo it." },
        "Restore Snapshot"
      );
      if (ok) await ctl.snapshotRestore(target.connection, target.database, pick.id);
    }),

    cmd("dbhelm.backup", async () => {
      const target = await pickTarget(ctl, (c) => c.engine === "mongodb");
      if (target) await ctl.backupCreate(target.connection, target.database);
    }),

    cmd("dbhelm.restoreBackup", async () => {
      const backups = ctl.state.backups;
      if (!backups.length) { void vscode.window.showInformationMessage("DBHelm: there are no backups yet."); return; }
      const pick = await vscode.window.showQuickPick(
        backups.map((b) => ({ label: `${b.connection}.${b.database || "all databases"}`, description: new Date(b.createdAt).toLocaleString(), detail: b.fileName, b })),
        { title: "DBHelm: Restore Backup", placeHolder: "Choose the backup to restore" }
      );
      if (!pick) return;
      const ok = await vscode.window.showWarningMessage(
        `Restore ${pick.b.connection}.${pick.b.database || "all databases"} from this backup?`,
        { modal: true, detail: "This drops the current data and restores the archive." },
        "Restore Backup"
      );
      if (ok) await ctl.backupRestore(pick.b.connection, pick.b.id);
    }),

    vscode.workspace.onDidChangeConfiguration((e) => { if (e.affectsConfiguration("dbhelm")) void ctl.refreshAll(); }),
    vscode.workspace.onDidChangeWorkspaceFolders(() => void ctl.restart())
  );

  void maybeShowFirstRunPrompt(context);
  void ctl.start();
}

async function pickTarget(ctl: Controller, filter: (c: ConnView) => boolean): Promise<{ connection: string; database: string } | undefined> {
  const conns = ctl.state.connections.filter(filter);
  if (!conns.length) { void vscode.window.showInformationMessage("DBHelm: add a connection first (DBHelm: Add Connection…)."); return undefined; }
  const conn = conns.length === 1 ? conns[0].name : (await vscode.window.showQuickPick(conns.map((c) => ({ label: c.name, description: c.engine })), { title: "DBHelm: Connection" }))?.label;
  if (!conn) return undefined;
  await ctl.loadDatabases(conn);
  const dbs = ctl.state.databases[conn] ?? [];
  if (!dbs.length) { void vscode.window.showWarningMessage(`DBHelm: could not list databases for ${conn}. Test the connection first.`); return undefined; }
  const database = dbs.length === 1 ? dbs[0] : await vscode.window.showQuickPick(dbs, { title: `DBHelm: Database on ${conn}` });
  return database ? { connection: conn, database } : undefined;
}

export function deactivate(): Thenable<void> | void {
  return controller?.broker.stop();
}
