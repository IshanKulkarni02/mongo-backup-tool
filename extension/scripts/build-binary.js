// Builds the Go `dbhelm` binary into bin/<platform>-<arch>/ so the extension can ship and run it.
//   node scripts/build-binary.js            build for this machine (development)
//   node scripts/build-binary.js --all      build every supported target (release)
const { execFileSync } = require("child_process");
const fs = require("fs");
const path = require("path");

const extRoot = path.join(__dirname, "..");
const repoRoot = path.join(extRoot, "..");
const version = require(path.join(extRoot, "package.json")).version;

const TARGETS = [
  { node: "darwin-arm64", goos: "darwin", goarch: "arm64" },
  { node: "darwin-x64", goos: "darwin", goarch: "amd64" },
  { node: "linux-x64", goos: "linux", goarch: "amd64" },
  { node: "linux-arm64", goos: "linux", goarch: "arm64" },
  { node: "win32-x64", goos: "windows", goarch: "amd64" },
  { node: "win32-arm64", goos: "windows", goarch: "arm64" },
];
const hostGoos = { darwin: "darwin", linux: "linux", win32: "windows" }[process.platform];
const hostGoarch = { x64: "amd64", arm64: "arm64" }[process.arch];

const wanted = process.argv.includes("--all") ? TARGETS : TARGETS.filter((t) => t.goos === hostGoos && t.goarch === hostGoarch);
if (!wanted.length) { console.error(`No Go target for ${process.platform}-${process.arch}`); process.exit(1); }

for (const t of wanted) {
  const out = path.join(extRoot, "bin", t.node, t.goos === "windows" ? "dbhelm.exe" : "dbhelm");
  fs.mkdirSync(path.dirname(out), { recursive: true });
  console.log(`building ${t.node}`);
  execFileSync("go", ["build", "-trimpath", "-ldflags", `-s -w -X github.com/IshanKulkarni02/dbhelm/cmd.version=v${version}`, "-o", out, "."], {
    cwd: repoRoot, stdio: "inherit", env: { ...process.env, GOOS: t.goos, GOARCH: t.goarch, CGO_ENABLED: "0" },
  });
}
