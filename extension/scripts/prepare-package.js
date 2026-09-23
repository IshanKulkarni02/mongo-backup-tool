// Copies the agent skill from the repo into the extension folder so it ships inside the VSIX.
const fs = require("fs");
const path = require("path");

const src = path.join(__dirname, "..", "..", "skill", "dbhelm");
const dest = path.join(__dirname, "..", "skill", "dbhelm");
if (!fs.existsSync(path.join(src, "SKILL.md"))) { console.error("skill/dbhelm/SKILL.md not found next to the extension"); process.exit(1); }
fs.rmSync(dest, { recursive: true, force: true });
fs.cpSync(src, dest, { recursive: true });
console.log("skill copied");
