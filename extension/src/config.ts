import * as fs from "fs";
import * as os from "os";
import * as path from "path";

/** Everything DBHelm keeps inside a project lives in this one dot-folder. */
export const DOT_FOLDER = ".dbhelm";

export const dotDir = (root: string) => path.join(root, DOT_FOLDER);
export const runDir = (root: string) => path.join(dotDir(root), "run");
export const logPath = (root: string) => path.join(dotDir(root), "logbook.jsonl");
export const agentsDocPath = (root: string) => path.join(dotDir(root), "AGENTS.md");

/** Appends `entry` to the project's .gitignore unless a line already matches it exactly. */
export function ensureGitignored(workspaceRoot: string, entry: string): void {
  const gitignorePath = path.join(workspaceRoot, ".gitignore");
  let existing = "";
  if (fs.existsSync(gitignorePath)) {
    existing = fs.readFileSync(gitignorePath, "utf8");
    if (existing.split(/\r?\n/).some((line) => line.trim() === entry)) return;
  }
  const prefix = existing && !existing.endsWith("\n") ? "\n" : "";
  fs.writeFileSync(gitignorePath, `${existing}${prefix}${entry}\n`, "utf8");
}

/** Generated, per-machine files: never committed. */
export function ensureDotFolderIgnored(root: string): void {
  ensureGitignored(root, `${DOT_FOLDER}/run/`);
  ensureGitignored(root, `${DOT_FOLDER}/logbook.jsonl`);
}

/** Where the Go core keeps its state (mirrors os.UserConfigDir()/dbhelm, or DBHELM_CONFIG_DIR). */
export function userConfigDir(): string {
  const override = process.env.DBHELM_CONFIG_DIR;
  if (override) return override;
  if (process.platform === "darwin") return path.join(os.homedir(), "Library", "Application Support", "dbhelm");
  if (process.platform === "win32") return path.join(process.env.APPDATA || path.join(os.homedir(), "AppData", "Roaming"), "dbhelm");
  return path.join(process.env.XDG_CONFIG_HOME || path.join(os.homedir(), ".config"), "dbhelm");
}
