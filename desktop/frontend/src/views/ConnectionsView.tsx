import { useEffect, useState } from "react";
import { Plus, Database, Trash2, CheckCircle2, Loader2, Users, AlertTriangle } from "lucide-react";
import {
  ListConnections,
  AddConnection,
  RemoveConnection,
  TestConnection,
  EngineIDs,
  PickSQLiteFile,
  SwitchTenant,
  SecureCredentialStorageAvailable,
} from "../../wailsjs/go/main/App";
import { main } from "../../wailsjs/go/models";
import { Button } from "../components/Button";
import { SegmentedControl } from "../components/SegmentedControl";
import { Card } from "../components/Card";
import { Input } from "../components/Input";
import { Modal } from "../components/Modal";
import { EmptyState } from "../components/EmptyState";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { useToast } from "../components/Toast";
import { useUIMode } from "../lib/uiMode";
import "./ConnectionsView.css";

export function ConnectionsView({
  onOpenDatabase,
}: {
  onOpenDatabase?: (connection: main.ConnectionInfo, database: string) => void;
}) {
  const { mode } = useUIMode();
  const [connections, setConnections] = useState<main.ConnectionInfo[] | null>(null);
  const [showAdd, setShowAdd] = useState(false);
  const [removeTarget, setRemoveTarget] = useState<string | null>(null);
  const [testResults, setTestResults] = useState<Record<string, string[] | "error" | "loading">>({});
  const [tenantTarget, setTenantTarget] = useState<main.ConnectionInfo | null>(null);
  const [secureStorage, setSecureStorage] = useState<boolean | null>(null);
  const toast = useToast();

  const load = () =>
    ListConnections()
      .then(setConnections)
      .catch((e) => toast.push("error", String(e)));

  useEffect(() => {
    load();
    SecureCredentialStorageAvailable().then(setSecureStorage);
  }, []);

  async function handleTest(name: string) {
    setTestResults((prev) => ({ ...prev, [name]: "loading" }));
    try {
      const dbs = await TestConnection(name);
      setTestResults((prev) => ({ ...prev, [name]: dbs }));
    } catch (e) {
      setTestResults((prev) => ({ ...prev, [name]: "error" }));
      toast.push("error", String(e));
    }
  }

  // Beginner mode skips the "Test" affordance entirely — a non-technical
  // user expects to just see their databases, not to know they need to
  // click a button labeled "Test" first.
  useEffect(() => {
    if (mode !== "beginner" || !connections) return;
    for (const c of connections) {
      if (!(c.name in testResults)) handleTest(c.name);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode, connections]);

  async function handleRemove() {
    if (!removeTarget) return;
    try {
      await RemoveConnection(removeTarget);
      toast.push("success", `Removed ${removeTarget}`);
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setRemoveTarget(null);
      load();
    }
  }

  const addLabel = mode === "beginner" ? "Connect a Database" : "Add connection";

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">{mode === "beginner" ? "My Databases" : "Connections"}</h1>
        <Button onClick={() => setShowAdd(true)}>
          <Plus size={16} /> {addLabel}
        </Button>
      </div>

      {secureStorage === false && (
        <div className="credential-warning" role="status">
          <AlertTriangle size={16} />
          {mode === "beginner"
            ? "Your computer's secure password storage isn't available, so database passwords are saved in a private file only you can read."
            : "System keychain unavailable. Saved database passwords use the owner-only config file (0600) fallback."}
        </div>
      )}

      {connections?.length === 0 && (
        <EmptyState
          icon={<Database size={32} />}
          title={mode === "beginner" ? "No databases yet" : "No connections yet"}
          description={
            mode === "beginner"
              ? "Connect your first database to start viewing your data."
              : "Add a local or Atlas MongoDB connection to get started."
          }
          action={
            <Button onClick={() => setShowAdd(true)}>
              <Plus size={16} /> {addLabel}
            </Button>
          }
        />
      )}

      <div className="conn-list">
        {connections?.map((c) => {
          const result = testResults[c.name];
          return (
            <Card key={c.name} className="conn-row">
              <div className="conn-info">
                <div className="conn-name">
                  {c.name}
                  <span className="conn-engine-badge">{ENGINE_LABELS[c.engine] ?? c.engine}</span>
                  {c.environment && (
                    <span className={`conn-env-badge conn-env-${c.environment}`}>{c.environment}</span>
                  )}
                  {c.readOnly && (
                    <span className="conn-readonly-badge">{mode === "beginner" ? "protected" : "read-only"}</span>
                  )}
                  {mode === "pro" && c.tenantSessionVar && (
                    <span className="conn-tenant-badge" title={`tenant session var: ${c.tenantSessionVar}`}>
                      tenant: {c.tenantValue || "(none set)"}
                    </span>
                  )}
                </div>
                {mode === "pro" && <div className="conn-uri mono">{c.redactedUri}</div>}
                {result === "loading" && mode === "beginner" && (
                  <div className="conn-dbs conn-dbs-loading">
                    <Loader2 size={14} className="spin" /> Connecting...
                  </div>
                )}
                {Array.isArray(result) && (
                  <div className="conn-dbs">
                    {result.length === 0
                      ? mode === "beginner"
                        ? "No data in this database yet"
                        : "No databases"
                      : result.map((d) => (
                          <button
                            key={d}
                            type="button"
                            className="conn-db-chip mono"
                            onClick={() => onOpenDatabase?.(c, d)}
                            disabled={!onOpenDatabase}
                            title={onOpenDatabase ? `Open ${d}` : undefined}
                          >
                            {d}
                          </button>
                        ))}
                  </div>
                )}
                {result === "error" && (
                  <div className="conn-dbs conn-dbs-error">
                    {mode === "beginner" ? "Couldn't connect — check the details and try again" : "Connection failed"}
                    {mode === "beginner" && (
                      <button type="button" className="conn-retry-link" onClick={() => handleTest(c.name)}>
                        Try again
                      </button>
                    )}
                  </div>
                )}
              </div>
              <div className="conn-actions">
                {mode === "pro" && (
                  <Button variant="ghost" onClick={() => handleTest(c.name)} disabled={result === "loading"}>
                    {result === "loading" ? <Loader2 size={16} className="spin" /> : <CheckCircle2 size={16} />}
                    Test
                  </Button>
                )}
                {mode === "pro" && c.tenantSessionVar && (
                  <Button variant="ghost" onClick={() => setTenantTarget(c)}>
                    <Users size={16} /> Switch tenant
                  </Button>
                )}
                <Button variant="danger" onClick={() => setRemoveTarget(c.name)}>
                  <Trash2 size={16} />
                  {mode === "beginner" && " Remove"}
                </Button>
              </div>
            </Card>
          );
        })}
      </div>

      {showAdd && mode === "beginner" && (
        <GuidedAddConnectionModal
          onClose={() => setShowAdd(false)}
          onAdded={() => {
            setShowAdd(false);
            load();
          }}
        />
      )}

      {showAdd && mode === "pro" && (
        <AddConnectionModal
          onClose={() => setShowAdd(false)}
          onAdded={() => {
            setShowAdd(false);
            load();
          }}
        />
      )}

      {removeTarget && (
        <ConfirmDialog
          title="Remove connection"
          message={`Remove "${removeTarget}"? This only removes the saved connection — it doesn't touch any data.`}
          confirmLabel="Remove"
          danger
          onConfirm={handleRemove}
          onCancel={() => setRemoveTarget(null)}
        />
      )}

      {tenantTarget && (
        <SwitchTenantModal
          connection={tenantTarget}
          onClose={() => setTenantTarget(null)}
          onSwitched={() => {
            setTenantTarget(null);
            load();
          }}
        />
      )}
    </div>
  );
}

function SwitchTenantModal({
  connection,
  onClose,
  onSwitched,
}: {
  connection: main.ConnectionInfo;
  onClose: () => void;
  onSwitched: () => void;
}) {
  const [value, setValue] = useState(connection.tenantValue);
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  async function submit() {
    setBusy(true);
    try {
      await SwitchTenant(connection.name, value);
      toast.push("success", `Switched ${connection.name} to tenant "${value}"`);
      onSwitched();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={`Switch tenant — ${connection.name}`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy || !value}>
            {busy ? "Switching..." : "Switch"}
          </Button>
        </>
      }
    >
      <p>
        Session variable <strong className="mono">{connection.tenantSessionVar}</strong> will be set on every new
        connection to this database.
      </p>
      <Input label="Tenant value" value={value} onChange={(e) => setValue(e.target.value)} mono autoFocus />
    </Modal>
  );
}

const ENGINE_LABELS: Record<string, string> = {
  mongodb: "MongoDB",
  postgres: "PostgreSQL",
  mysql: "MySQL",
  sqlite: "SQLite",
};

const URI_PLACEHOLDERS: Record<string, string> = {
  mongodb: "mongodb://localhost:27017",
  postgres: "postgres://user:pass@localhost:5432/mydb",
  mysql: "user:pass@tcp(localhost:3306)/mydb",
  sqlite: "/path/to/database.db",
};

const ALL_ENGINES = ["mongodb", "postgres", "mysql", "sqlite"];

const ENGINE_BLURBS: Record<string, string> = {
  mongodb: "Stores flexible, JSON-like records. Common for apps and MongoDB Atlas.",
  postgres: "A reliable, general-purpose SQL database.",
  mysql: "A reliable, widely-used SQL database.",
  sqlite: "A single file on your computer — the simplest option, no server needed.",
};

const DEFAULT_PORTS: Record<string, string> = { mongodb: "27017", postgres: "5432", mysql: "3306" };

// buildGuidedURI assembles a connection string from plain host/port/user/
// password/database fields so a beginner never has to type or understand
// URI syntax. Values with special characters are percent-encoded; MySQL's
// DSN format doesn't use percent-encoding, so very unusual passwords there
// may still need the raw URI field in Pro mode.
function buildGuidedURI(
  engine: string,
  opts: { host: string; port: string; user: string; password: string; dbName: string; useSrv: boolean }
): string {
  const { host, port, user, password, dbName, useSrv } = opts;
  const encUser = encodeURIComponent(user);
  const encPass = encodeURIComponent(password);
  const auth = user ? `${encUser}:${encPass}@` : "";
  switch (engine) {
    case "mongodb": {
      const scheme = useSrv ? "mongodb+srv" : "mongodb";
      const hostPart = useSrv ? host : `${host}:${port || DEFAULT_PORTS.mongodb}`;
      return `${scheme}://${auth}${hostPart}/${dbName}`;
    }
    case "postgres":
      return `postgres://${auth}${host}:${port || DEFAULT_PORTS.postgres}/${dbName}`;
    case "mysql":
      return `${user}:${password}@tcp(${host}:${port || DEFAULT_PORTS.mysql})/${dbName}`;
    default:
      return "";
  }
}

function GuidedAddConnectionModal({ onClose, onAdded }: { onClose: () => void; onAdded: () => void }) {
  const [name, setName] = useState("");
  const [engine, setEngine] = useState("mongodb");
  const [availableEngines, setAvailableEngines] = useState<string[]>(["mongodb"]);
  const [filePath, setFilePath] = useState("");
  const [host, setHost] = useState("localhost");
  const [port, setPort] = useState("");
  const [user, setUser] = useState("");
  const [password, setPassword] = useState("");
  const [dbName, setDbName] = useState("");
  const [useSrv, setUseSrv] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  useEffect(() => {
    EngineIDs().then(setAvailableEngines).catch(() => {});
  }, []);

  async function browseSQLiteFile() {
    try {
      const path = await PickSQLiteFile();
      if (path) setFilePath(path);
    } catch (e) {
      toast.push("error", String(e));
    }
  }

  async function submit() {
    if (!name) {
      setError("Give your database a name so you can find it later");
      return;
    }
    const uri = engine === "sqlite" ? filePath : buildGuidedURI(engine, { host, port, user, password, dbName, useSrv });
    if (!uri || (engine !== "sqlite" && !host)) {
      setError(engine === "sqlite" ? "Choose a database file" : "At least the address (host) is required");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await AddConnection(new main.ConnectionInput({ name, uri, engine, environment: "", readOnly: false }));
      toast.push("success", `Connected to "${name}"`);
      onAdded();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title="Connect a database"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy}>
            {busy ? "Connecting..." : "Connect"}
          </Button>
        </>
      }
    >
      <Input
        label="Give it a name"
        placeholder="e.g. My Database"
        value={name}
        onChange={(e) => setName(e.target.value)}
        autoFocus
      />
      <div className="field">
        <label className="field-label">What kind of database is it?</label>
        <SegmentedControl
          value={engine}
          onChange={setEngine}
          options={ALL_ENGINES.map((id) => ({
            value: id,
            label: ENGINE_LABELS[id] ?? id,
            disabled: !availableEngines.includes(id),
          }))}
        />
        <div className="field-hint">{ENGINE_BLURBS[engine]}</div>
      </div>

      {engine === "sqlite" ? (
        <Input
          label="Database file"
          placeholder="Choose a file on your computer"
          value={filePath}
          onChange={(e) => setFilePath(e.target.value)}
          mono
          error={error}
          trailing={
            <Button variant="ghost" onClick={browseSQLiteFile}>
              Choose file
            </Button>
          }
        />
      ) : (
        <>
          {engine === "mongodb" && (
            <div className="ssh-toggle">
              <label>
                <input type="checkbox" checked={useSrv} onChange={(e) => setUseSrv(e.target.checked)} />
                This is a cloud database (e.g. MongoDB Atlas)
              </label>
            </div>
          )}
          <Input
            label={useSrv ? "Cluster address" : "Address (host)"}
            placeholder={useSrv ? "mycluster.abcde.mongodb.net" : "localhost"}
            value={host}
            onChange={(e) => setHost(e.target.value)}
            error={error}
          />
          {!useSrv && (
            <Input
              label="Port (optional)"
              placeholder={DEFAULT_PORTS[engine]}
              value={port}
              onChange={(e) => setPort(e.target.value)}
            />
          )}
          <Input label="Username (optional)" value={user} onChange={(e) => setUser(e.target.value)} />
          <Input
            label="Password (optional)"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <Input
            label={engine === "mongodb" ? "Database name (optional)" : "Database name"}
            value={dbName}
            onChange={(e) => setDbName(e.target.value)}
          />
        </>
      )}
    </Modal>
  );
}

function AddConnectionModal({ onClose, onAdded }: { onClose: () => void; onAdded: () => void }) {
  const [name, setName] = useState("");
  const [uri, setUri] = useState("");
  const [engine, setEngine] = useState("mongodb");
  const [environment, setEnvironment] = useState("");
  const [readOnly, setReadOnly] = useState(false);
  const [availableEngines, setAvailableEngines] = useState<string[]>(["mongodb"]);
  const [showSSH, setShowSSH] = useState(false);
  const [sshHost, setSshHost] = useState("");
  const [sshUser, setSshUser] = useState("");
  const [sshPassword, setSshPassword] = useState("");
  const [sshPrivateKey, setSshPrivateKey] = useState("");
  const [tenantSessionVar, setTenantSessionVar] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  useEffect(() => {
    EngineIDs().then(setAvailableEngines).catch(() => {});
  }, []);

  useEffect(() => {
    if (engine === "postgres" || engine === "mysql") return;
    // SSH tunneling only applies to postgres/mysql — without this, the
    // checkbox that controls showSSH disappears (it's only rendered for
    // those engines) while the SSH fields stay visible and their stale
    // values still get submitted with the new engine.
    setShowSSH(false);
    setSshHost("");
    setSshUser("");
    setSshPassword("");
    setSshPrivateKey("");
  }, [engine]);

  async function browseSQLiteFile() {
    try {
      const path = await PickSQLiteFile();
      if (path) setUri(path);
    } catch (e) {
      toast.push("error", String(e));
    }
  }

  async function submit() {
    if (!name || !uri) {
      setError("Both a name and a URI are required");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await AddConnection(
        new main.ConnectionInput({
          name,
          uri,
          engine,
          environment,
          readOnly,
          sshHost: showSSH ? sshHost : "",
          sshUser: showSSH ? sshUser : "",
          sshPassword: showSSH ? sshPassword : "",
          sshPrivateKey: showSSH ? sshPrivateKey : "",
          tenantSessionVar,
        })
      );
      toast.push("success", `Saved connection "${name}"`);
      onAdded();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title="Add connection"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy}>
            {busy ? "Saving..." : "Save"}
          </Button>
        </>
      }
    >
      <Input label="Name" placeholder="e.g. local" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
      <div className="field">
        <label className="field-label">Engine</label>
        <SegmentedControl
          value={engine}
          onChange={setEngine}
          options={ALL_ENGINES.map((id) => ({
            value: id,
            label: ENGINE_LABELS[id] ?? id,
            disabled: !availableEngines.includes(id),
          }))}
        />
      </div>
      <Input
        label={engine === "sqlite" ? "File path" : "URI"}
        placeholder={URI_PLACEHOLDERS[engine] ?? ""}
        value={uri}
        onChange={(e) => setUri(e.target.value)}
        mono
        error={error}
        trailing={
          engine === "sqlite" ? (
            <Button variant="ghost" onClick={browseSQLiteFile}>
              Browse
            </Button>
          ) : undefined
        }
      />
      <div className="field">
        <label className="field-label">Environment (optional)</label>
        <SegmentedControl
          value={environment}
          onChange={setEnvironment}
          options={[
            { value: "", label: "None" },
            { value: "dev", label: "Development" },
            { value: "staging", label: "Staging" },
            { value: "prod", label: "Production" },
          ]}
        />
      </div>
      <div className="ssh-toggle">
        <label>
          <input type="checkbox" checked={readOnly} onChange={(e) => setReadOnly(e.target.checked)} />
          Read-only (Safe Mode) — block all writes on this connection
        </label>
      </div>
      {(engine === "postgres" || engine === "mysql") && (
        <>
          <div className="ssh-toggle">
            <label>
              <input type="checkbox" checked={showSSH} onChange={(e) => setShowSSH(e.target.checked)} />
              Connect through an SSH tunnel
            </label>
          </div>
          <Input
            label="Multi-tenant session variable (optional)"
            placeholder={engine === "postgres" ? "app.current_tenant" : "app_current_tenant"}
            value={tenantSessionVar}
            onChange={(e) => setTenantSessionVar(e.target.value)}
            mono
          />
        </>
      )}
      {showSSH && (
        <>
          <Input label="SSH host" placeholder="bastion.example.com:22" value={sshHost} onChange={(e) => setSshHost(e.target.value)} />
          <Input label="SSH user" value={sshUser} onChange={(e) => setSshUser(e.target.value)} />
          <Input
            label="SSH password"
            type="password"
            value={sshPassword}
            onChange={(e) => setSshPassword(e.target.value)}
          />
          <div className="field">
            <label className="field-label">or SSH private key (PEM)</label>
            <textarea
              className="input ssh-key-textarea mono"
              rows={4}
              value={sshPrivateKey}
              onChange={(e) => setSshPrivateKey(e.target.value)}
              placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
            />
          </div>
        </>
      )}
    </Modal>
  );
}
