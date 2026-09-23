import * as crypto from "crypto";
import * as fs from "fs";
import * as path from "path";
import * as vscode from "vscode";
import { userConfigDir } from "./config";

const exeName = process.platform === "win32" ? "dbhelm.exe" : "dbhelm";

/** Folder name of the binary this extension build ships, e.g. darwin-arm64. */
function platformDir(): string {
  return `${process.platform}-${process.arch}`;
}

function bundledPath(context: vscode.ExtensionContext): string {
  return path.join(context.extensionPath, "bin", platformDir(), exeName);
}

/** The stable location agents are told to run, so an extension update never breaks their command. */
export function installedBinaryPath(): string {
  return path.join(userConfigDir(), "bin", exeName);
}

function sha256(file: string): string {
  return crypto.createHash("sha256").update(fs.readFileSync(file)).digest("hex");
}

/**
 * Copies the bundled binary to the stable per-user location when it is missing or different.
 * The extension and the agent skill then run the exact same file. A copy that fails (for
 * instance a Windows binary that is in use) leaves the existing one in place.
 */
export function installBundledBinary(context: vscode.ExtensionContext): { path: string; updated: boolean } | undefined {
  const src = bundledPath(context);
  if (!fs.existsSync(src)) return undefined;
  const dest = installedBinaryPath();
  try {
    if (fs.existsSync(dest) && sha256(dest) === sha256(src)) return { path: dest, updated: false };
    fs.mkdirSync(path.dirname(dest), { recursive: true });
    const tmp = `${dest}.${process.pid}.tmp`;
    fs.copyFileSync(src, tmp);
    fs.chmodSync(tmp, 0o755);
    fs.renameSync(tmp, dest);
    return { path: dest, updated: true };
  } catch {
    return fs.existsSync(dest) ? { path: dest, updated: false } : { path: src, updated: false };
  }
}

/** Which binary to run: the user's setting, the installed copy, or `dbhelm` on PATH. */
export function resolveBinary(context: vscode.ExtensionContext): string {
  const configured = vscode.workspace.getConfiguration("dbhelm").get<string>("binaryPath", "").trim();
  if (configured) return configured;
  const installed = installBundledBinary(context);
  if (installed) return installed.path;
  return "dbhelm";
}
