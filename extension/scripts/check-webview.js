// Compile-time guard for the webview. The UI logic lives in media/webview/app.js (a real file, so a
// stray quote shows up in the editor), but a syntax slip there still only fails at runtime as a
// permanently blank panel. This parses it, renders the HTML shell for both surfaces, and checks that
// every message the webview posts is one the extension host actually handles.
const Module = require("module");
const path = require("path");
const fs = require("fs");

const root = path.join(__dirname, "..");
const appJs = fs.readFileSync(path.join(root, "media", "webview", "app.js"), "utf8");
const fail = (msg) => { console.error(msg); process.exit(1); };

try { new Function(appJs); } catch (err) { fail(`media/webview/app.js has a syntax error: ${err.message}`); }

const load = Module._load;
Module._load = (req, ...rest) => (req === "vscode" ? { Disposable: class {}, Uri: { joinPath: () => ({}) }, EventEmitter: class {} } : load(req, ...rest));
const file = path.join(root, "out", "panel.js");
const m = new Module(file);
m.filename = file;
m.paths = Module._nodeModulePaths(path.dirname(file));
const panelJs = fs.readFileSync(file, "utf8");
m._compile(panelJs + "\nmodule.exports.__renderHtml = renderHtml;", file);

const webview = { cspSource: "vscode-resource:" };
for (const surface of ["sidebar", "config", "chart"]) {
  const html = m.exports.__renderHtml(surface, webview, { codicon: "codicon.css", css: "app.css", js: "app.js" });
  const csp = /Content-Security-Policy" content="([^"]+)"/.exec(html);
  if (!csp || !/default-src 'none'/.test(csp[1]) || /unsafe-inline|unsafe-eval/.test(csp[1])) fail("The webview CSP must start from default-src 'none' and allow no unsafe-inline/unsafe-eval.");
  if (!/<script nonce="[^"]+" src="app\.js">/.test(html)) fail("The HTML shell does not load app.js with a nonce.");
}

const posted = new Set([...appJs.matchAll(/post\(\{ type: "([A-Za-z]+)"/g)].map((x) => x[1]));
const handled = new Set([...panelJs.matchAll(/case "([A-Za-z]+)":/g)].map((x) => x[1]));
const missing = [...posted].filter((t) => !handled.has(t));
if (missing.length) fail(`The webview posts message types the host does not handle: ${missing.join(", ")}`);
console.log(`webview OK (${posted.size} message types checked)`);
