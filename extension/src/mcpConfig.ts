import * as fs from "fs";
import * as path from "path";
import { runDir } from "./config";

/** The MCP server entry MCP clients (Claude Code, Cursor, Codex, ...) use to reach this project's broker. */
export function mcpServerEntry(binary: string, root: string): { command: string; args: string[]; env: Record<string, string> } {
  // DBHELM_RUN_DIR pins the broker to this project no matter which directory the client launches from.
  return { command: binary, args: ["mcp"], env: { DBHELM_RUN_DIR: runDir(root) } };
}

export function mcpSnippet(binary: string, root: string): string {
  return JSON.stringify({ mcpServers: { dbhelm: mcpServerEntry(binary, root) } }, null, 2);
}

export type McpWrite = "created" | "updated" | "unchanged" | "skipped-differs";

/** Merges the dbhelm server into the project's .mcp.json, keeping every other server and setting. */
export function writeProjectMcp(root: string, binary: string, overwrite: boolean): { path: string; action: McpWrite } {
  const file = path.join(root, ".mcp.json");
  const entry = mcpServerEntry(binary, root);
  let doc: { mcpServers?: Record<string, unknown> } = {};
  let existed = false;
  if (fs.existsSync(file)) {
    existed = true;
    try { doc = JSON.parse(fs.readFileSync(file, "utf8")); }
    catch { throw new Error(".mcp.json exists but is not valid JSON. Fix or remove it, then run this again."); }
  }
  const servers = (doc.mcpServers ??= {});
  const current = servers.dbhelm;
  if (current && JSON.stringify(current) === JSON.stringify(entry)) return { path: file, action: "unchanged" };
  if (current && !overwrite) return { path: file, action: "skipped-differs" };
  servers.dbhelm = entry;
  fs.writeFileSync(file, JSON.stringify(doc, null, 2) + "\n", "utf8");
  return { path: file, action: existed ? "updated" : "created" };
}
