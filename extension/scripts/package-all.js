// Builds one VSIX per platform, each containing only that platform's dbhelm binary.
//   node scripts/package-all.js            all six targets (needs `go`, cross-compiles)
//   node scripts/package-all.js linux-x64  one target
// Output: dist/dbhelm-<target>-<version>.vsix. Publishing is a separate, deliberate step.
const { execFileSync } = require("child_process");
const fs = require("fs");
const path = require("path");

const root = path.join(__dirname, "..");
const TARGETS = ["darwin-arm64", "darwin-x64", "linux-x64", "linux-arm64", "win32-x64", "win32-arm64"];
const wanted = process.argv.slice(2).length ? process.argv.slice(2) : TARGETS;
for (const t of wanted) if (!TARGETS.includes(t)) { console.error(`unknown target ${t}`); process.exit(1); }

const run = (cmd, args) => execFileSync(cmd, args, { cwd: root, stdio: "inherit", shell: process.platform === "win32" });

run("node", ["scripts/build-binary.js", "--all"]);
run("node", ["scripts/prepare-package.js"]);
run("npm", ["run", "compile"]);

const bin = path.join(root, "bin");
const stash = path.join(root, "bin.all");
fs.rmSync(stash, { recursive: true, force: true });
fs.renameSync(bin, stash);
fs.mkdirSync(path.join(root, "dist"), { recursive: true });
try {
  for (const t of wanted) {
    fs.rmSync(bin, { recursive: true, force: true });
    fs.mkdirSync(bin);
    fs.cpSync(path.join(stash, t), path.join(bin, t), { recursive: true });
    const out = path.join("dist", `dbhelm-${t}-${require(path.join(root, "package.json")).version}.vsix`);
    console.log(`packaging ${t}`);
    run("npx", ["vsce", "package", "--target", t, "--out", out]);
  }
} finally {
  fs.rmSync(bin, { recursive: true, force: true });
  fs.renameSync(stash, bin);
}
