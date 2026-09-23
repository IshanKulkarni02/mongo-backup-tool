# Contributing to DBHelm for VS Code

The extension is the human side of the DBHelm toolkit; the agent side is the skill in `../skill/dbhelm`, and both talk to the same Go broker (`dbhelm serve`). Change one, check the other.

## Local setup

```bash
npm install
npm run build:binary
npm run compile
npm test
```

## Rules that keep the toolkit consistent

These follow the FTPilot toolkit style guide.

- Theme tokens only (`--vscode-*`), codicons only, no emoji. Controls are 26px tall, 2px radius, 13px font.
- Anything that changes data goes through a read-only confirm screen with a verb button ("Run Statement", not "OK").
- Passwords are entered only through `showInputBox({ password: true })` and go straight to the broker, which stores them in the OS keychain. They never touch the webview, a draft, a log or a config file.
- Errors say what failed, the likely cause, and the next action. Nautical names (Helm, Bridge, Autopilot, Ship's Log) label features; error and confirm text stays literal.
- The webview posts messages; it never calls the broker. `scripts/check-webview.js` fails the build if it posts a message the host does not handle.
- Never claim success you did not verify.

## Before a PR

`npm run compile` and `npm test` pass; you looked at the sidebar and editor tab in a real VS Code window (light and dark) including empty and failure states; nothing sensitive in logs, config or webview HTML.
