# Architecture

```
 VS Code extension --operator token--> dbhelm serve <--agent token-- `dbhelm agent ...` <-- SKILL.md
   (this folder)                       (the broker)
```

- `dbhelm serve` is spawned per workspace (`.dbhelm/run/broker.json`, 0600, agent token only). The operator token is printed to the spawner's stdout pipe and lives in memory only.
- Two roles, two tokens. Approving requests, Autopilot and connection management are operator-only.
- The broker fails closed: with no operator attached (no open `/events` stream), change requests are refused, and Autopilot drops when the operator detaches.
- `src/controller.ts` holds all state and operations; `src/panel.ts` is a thin webview host; `media/webview/app.js` renders state and posts messages.
- The bundled binary is copied to a stable per-user path (`<config dir>/bin/dbhelm`) so the agent skill's command survives extension updates.
- Deliberate deviation from the toolkit guide: database passwords are stored by the Go core in the OS keychain (so the CLI, desktop app and agents share one connection store) instead of `context.secrets`. The webview still only ever learns "is it set". The webview script is a real file rather than a template string; `check-webview.js` still gates compile.
