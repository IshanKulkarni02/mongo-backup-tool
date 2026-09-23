import { ChildProcess, spawn } from "child_process";
import * as http from "http";
import * as vscode from "vscode";
import { runDir } from "./config";

/** Must match broker.ProtocolVersion in the Go core. */
export const PROTOCOL_VERSION = 1;

export interface ErrorBody { code: string; message: string; hint?: string }
export interface Meta { truncated?: boolean; rows?: number; ms?: number; requestId?: string }
export interface Envelope<T = any> { ok: boolean; data?: T; error?: ErrorBody; meta?: Meta }
export interface BrokerEvent { type: string; data: any }
export type BrokerStatus = "stopped" | "starting" | "ready" | "error";

interface Info { port: number; pid: number; operatorToken: string; agentToken: string }

/**
 * The operator's end of the broker: spawns `dbhelm serve` for this workspace, keeps the operator
 * token in memory only (it is never written anywhere), and streams events. Closing the child's
 * stdin, or losing this process, stops the broker, so it never outlives the extension.
 */
export class Broker {
  private child?: ChildProcess;
  private info?: Info;
  private sse?: http.ClientRequest;
  private stopping = false;
  private reconnect?: NodeJS.Timeout;
  status: BrokerStatus = "stopped";
  error?: string;

  private readonly eventEmitter = new vscode.EventEmitter<BrokerEvent>();
  private readonly statusEmitter = new vscode.EventEmitter<void>();
  readonly onEvent = this.eventEmitter.event;
  readonly onStatus = this.statusEmitter.event;

  constructor(private readonly output: vscode.OutputChannel) {}

  private setStatus(status: BrokerStatus, error?: string): void {
    this.status = status;
    this.error = error;
    this.statusEmitter.fire();
  }

  async start(binary: string, root: string): Promise<void> {
    await this.stop();
    this.stopping = false;
    this.setStatus("starting");
    try {
      await this.checkProtocol(binary);
      this.info = await this.spawnServe(binary, root);
      this.openEvents();
      this.setStatus("ready");
    } catch (err) {
      this.setStatus("error", err instanceof Error ? err.message : String(err));
      await this.stop();
      this.setStatus("error", this.error);
    }
  }

  private checkProtocol(binary: string): Promise<void> {
    return new Promise((resolve, reject) => {
      const child = spawn(binary, ["version", "--json"], { timeout: 15_000 });
      let out = "";
      let err = "";
      child.stdout.on("data", (d) => (out += d));
      child.stderr.on("data", (d) => (err += d));
      child.on("error", (e) => reject(new Error(`Cannot run the DBHelm binary (${binary}): ${e.message}. Set "dbhelm.binaryPath" or reinstall the extension.`)));
      child.on("close", () => {
        try {
          const v = JSON.parse(out.trim().split("\n").pop() || "");
          if (v.protocolVersion !== PROTOCOL_VERSION) {
            reject(new Error(`The DBHelm binary speaks protocol ${v.protocolVersion}, this extension needs ${PROTOCOL_VERSION}. Update both to the same release.`));
          } else resolve();
        } catch {
          reject(new Error(`The DBHelm binary did not answer "version --json"${err ? `: ${err.trim()}` : ""}. It may be too old for this extension.`));
        }
      });
    });
  }

  private spawnServe(binary: string, root: string): Promise<Info> {
    return new Promise((resolve, reject) => {
      const child = spawn(binary, ["serve", "--run-dir", runDir(root), "--exit-on-stdin-close"], { cwd: root, stdio: ["pipe", "pipe", "pipe"] });
      this.child = child;
      let buf = "";
      let settled = false;
      const timer = setTimeout(() => fail(new Error("The DBHelm broker did not start within 10 seconds.")), 10_000);
      const fail = (e: Error) => { if (!settled) { settled = true; clearTimeout(timer); reject(e); } };
      child.stdout.on("data", (d) => {
        if (settled) return;
        buf += d;
        const nl = buf.indexOf("\n");
        if (nl < 0) return;
        try {
          const info = JSON.parse(buf.slice(0, nl)) as Info;
          settled = true;
          clearTimeout(timer);
          resolve(info);
        } catch { fail(new Error("The DBHelm broker printed something unexpected on start.")); }
      });
      let errText = "";
      child.stderr.on("data", (d) => { errText += d; this.output.appendLine(String(d).trimEnd()); });
      child.on("error", (e) => fail(new Error(`Cannot start the DBHelm broker: ${e.message}`)));
      child.on("exit", (code) => {
        this.child = undefined;
        fail(new Error(errText.trim().split("\n").pop() || `The DBHelm broker exited (code ${code}).`));
        if (!this.stopping && this.status === "ready") this.setStatus("error", "The DBHelm broker stopped unexpectedly. Run \"DBHelm: Restart Broker\".");
      });
    });
  }

  async stop(): Promise<void> {
    this.stopping = true;
    if (this.reconnect) clearTimeout(this.reconnect);
    this.sse?.destroy();
    this.sse = undefined;
    const child = this.child;
    this.child = undefined;
    this.info = undefined;
    if (child) {
      child.stdin?.end();
      await new Promise<void>((resolve) => {
        const t = setTimeout(() => { child.kill(); resolve(); }, 2000);
        child.once("exit", () => { clearTimeout(t); resolve(); });
      });
    }
    if (this.status !== "error") this.status = "stopped";
  }

  /** Calls a broker method. Resolves with the envelope even when `ok` is false. */
  call<T = any>(method: string, params?: unknown, opts: { signal?: AbortSignal } = {}): Promise<Envelope<T>> {
    const info = this.info;
    if (!info) return Promise.resolve({ ok: false, error: { code: "BROKER_UNAVAILABLE", message: this.error || "The DBHelm broker is not running." } });
    return new Promise((resolve) => {
      const body = JSON.stringify({ method, params });
      const req = http.request(
        { host: "127.0.0.1", port: info.port, path: "/rpc", method: "POST", headers: { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(body), Authorization: `Bearer ${info.operatorToken}` } },
        (res) => {
          const chunks: Buffer[] = [];
          res.on("data", (c) => chunks.push(c));
          res.on("end", () => {
            try { resolve(JSON.parse(Buffer.concat(chunks).toString("utf8"))); }
            catch { resolve({ ok: false, error: { code: "BROKER_UNAVAILABLE", message: `Unreadable response from the broker (HTTP ${res.statusCode}).` } }); }
          });
        }
      );
      req.on("error", (e) => resolve({ ok: false, error: { code: opts.signal?.aborted ? "CANCELED" : "BROKER_UNAVAILABLE", message: opts.signal?.aborted ? "Canceled." : `Cannot reach the broker: ${e.message}` } }));
      opts.signal?.addEventListener("abort", () => req.destroy(new Error("aborted")));
      req.end(body);
    });
  }

  private openEvents(): void {
    const info = this.info;
    if (!info) return;
    const req = http.request({ host: "127.0.0.1", port: info.port, path: "/events", headers: { Authorization: `Bearer ${info.operatorToken}` } }, (res) => {
      let buf = "";
      res.setEncoding("utf8");
      res.on("data", (chunk: string) => {
        buf += chunk;
        let idx: number;
        while ((idx = buf.indexOf("\n\n")) >= 0) {
          const block = buf.slice(0, idx);
          buf = buf.slice(idx + 2);
          const type = /^event: (.*)$/m.exec(block)?.[1];
          const data = /^data: (.*)$/m.exec(block)?.[1];
          if (type && data) {
            try { this.eventEmitter.fire({ type, data: JSON.parse(data) }); } catch { /* ignore a malformed event */ }
          }
        }
      });
      res.on("end", () => this.scheduleReconnect());
    });
    req.on("error", () => this.scheduleReconnect());
    req.end();
    this.sse = req;
  }

  private scheduleReconnect(): void {
    if (this.stopping || !this.info) return;
    if (this.reconnect) clearTimeout(this.reconnect);
    this.reconnect = setTimeout(() => this.openEvents(), 1000);
  }

  dispose(): void {
    void this.stop();
    this.eventEmitter.dispose();
    this.statusEmitter.dispose();
  }
}
