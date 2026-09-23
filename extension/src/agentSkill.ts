import * as fs from "fs";
import * as path from "path";
import { agentsDocPath } from "./config";

/**
 * Installs the DBHelm agent skill into the user's project: a Claude Code skill folder plus a
 * plain-markdown twin other coding agents (Copilot, Cursor, Codex, ...) can be pointed at. Both
 * come from the one skill/dbhelm source shipped with the extension, and both name the exact
 * binary to run, so the agent and this extension always drive the same DBHelm. Pure fs/path
 * module (no `vscode`): the prompts live in extension.ts, as in FTPilot.
 */

export const SKILL_RELATIVE_DIR = path.join(".claude", "skills", "dbhelm");
const POINTER_START = "<!-- dbhelm:agent-instructions:start -->";
const POINTER_END = "<!-- dbhelm:agent-instructions:end -->";

/** The packaged skill, or (running from a source checkout) the repo's skill/dbhelm. */
export function skillSourceDir(extensionPath: string): string | undefined {
  for (const candidate of [path.join(extensionPath, "skill", "dbhelm"), path.join(extensionPath, "..", "skill", "dbhelm")]) {
    if (fs.existsSync(path.join(candidate, "SKILL.md"))) return candidate;
  }
  return undefined;
}

function binaryNote(binary: string): string {
  return `> **DBHelm binary for this workspace:** \`${binary}\`. Use this full path wherever \`dbhelm\` appears below if \`dbhelm\` is not on your PATH.\n`;
}

/** SKILL.md with the binary note placed right after the frontmatter. */
export function skillMarkdown(source: string, binary: string): string {
  const m = /^---\n[\s\S]*?\n---\n/.exec(source);
  if (!m) return `${binaryNote(binary)}\n${source}`;
  return `${m[0]}\n${binaryNote(binary)}${source.slice(m[0].length)}`;
}

function stripFrontmatter(md: string): string {
  return md.replace(/^---\n[\s\S]*?\n---\n/, "").replace(/^\s+/, "");
}

export type WriteAction = "created" | "updated" | "unchanged" | "skipped-differs";
export interface WriteResult { path: string; action: WriteAction }

function writeIfDifferent(abs: string, contents: string, overwrite: boolean, rel: string): WriteResult {
  if (fs.existsSync(abs)) {
    if (fs.readFileSync(abs, "utf8") === contents) return { path: rel, action: "unchanged" };
    if (!overwrite) return { path: rel, action: "skipped-differs" };
    fs.writeFileSync(abs, contents, "utf8");
    return { path: rel, action: "updated" };
  }
  fs.mkdirSync(path.dirname(abs), { recursive: true });
  fs.writeFileSync(abs, contents, "utf8");
  return { path: rel, action: "created" };
}

export function isAgentSkillInstalled(root: string): boolean {
  return fs.existsSync(path.join(root, SKILL_RELATIVE_DIR, "SKILL.md"));
}

/** Adds or refreshes the pointer block in a root AGENTS.md / CLAUDE.md, only if that file already exists. */
function upsertPointer(abs: string): WriteAction | undefined {
  if (!fs.existsSync(abs)) return undefined;
  const current = fs.readFileSync(abs, "utf8");
  const block =
    `${POINTER_START}\n` +
    `When a task needs to read or change a database, follow [\`.dbhelm/AGENTS.md\`](.dbhelm/AGENTS.md): ` +
    `use \`dbhelm agent\`, never a raw client or saved credentials.\n` +
    `${POINTER_END}`;
  const re = new RegExp(`${POINTER_START}[\\s\\S]*?${POINTER_END}`);
  const next = re.test(current) ? current.replace(re, block) : `${current}${current.endsWith("\n") ? "" : "\n"}\n${block}\n`;
  if (next === current) return "unchanged";
  fs.writeFileSync(abs, next, "utf8");
  return re.test(current) ? "updated" : "created";
}

export interface SetupResult { files: WriteResult[]; pointers: { path: string; action: WriteAction }[] }

export function setupAgentSkill(extensionPath: string, root: string, binary: string, overwrite: boolean): SetupResult {
  const src = skillSourceDir(extensionPath);
  if (!src) throw new Error("The agent skill files are missing from this DBHelm install. Reinstall the extension.");
  const skill = fs.readFileSync(path.join(src, "SKILL.md"), "utf8");
  const withBinary = skillMarkdown(skill, binary);
  const files: WriteResult[] = [
    writeIfDifferent(path.join(root, SKILL_RELATIVE_DIR, "SKILL.md"), withBinary, overwrite, path.join(SKILL_RELATIVE_DIR, "SKILL.md")),
    writeIfDifferent(agentsDocPath(root), `# DBHelm: database access for agents\n\n${stripFrontmatter(withBinary)}`, overwrite, path.join(".dbhelm", "AGENTS.md")),
  ];
  const refDir = path.join(src, "references");
  if (fs.existsSync(refDir)) {
    for (const name of fs.readdirSync(refDir)) {
      const rel = path.join(SKILL_RELATIVE_DIR, "references", name);
      files.push(writeIfDifferent(path.join(root, rel), fs.readFileSync(path.join(refDir, name), "utf8"), overwrite, rel));
    }
  }
  const pointers = ["AGENTS.md", "CLAUDE.md"].flatMap((name) => {
    const action = upsertPointer(path.join(root, name));
    return action ? [{ path: name, action }] : [];
  });
  return { files, pointers };
}
