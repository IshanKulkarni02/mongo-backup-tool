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
import { Card } from "../components/Card";
import { Input } from "../components/Input";
import { Modal } from "../components/Modal";
import { EmptyState } from "../components/EmptyState";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { useToast } from "../components/Toast";
import "./ConnectionsView.css";

export function ConnectionsView() {
  const [connections, setConnections] = useState<main.ConnectionInfo[] | null>(null);
  const [showAdd, setShowAdd] = useState(false);
  const [removeTarget, setRemoveTarget] = useState<string | null>(null);
  const [testResults, setTestResults] = useState<Record<string, string[] | "error" | "loading">>({});
  const [tenantTarget, setTenantTarget] = useState<main.ConnectionInfo | null>(null);
  const [secureStorage, setSecureStorage] = useState<boolean | null>(null);
  const toast = useToast();

  const load = () => ListConnections().then(setConnections);

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

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">Connections</h1>
        <Button onClick={() => setShowAdd(true)}>
          <Plus size={16} /> Add connection
        </Button>
      </div>

      {secureStorage === false && (
        <div className="credential-warning" role="status">
          <AlertTriangle size={16} />
          System keychain unavailable. Saved database passwords use the owner-only config file (0600) fallback.
        </div>
      )}

      {connections?.length === 0 && (
        <EmptyState
          icon={<Database size={32} />}
          title="No connections yet"
          description="Add a local or Atlas MongoDB connection to get started."
          action={
            <Button onClick={() => setShowAdd(true)}>
              <Plus size={16} /> Add connection
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
                  <span className="conn-engine-badge">{c.engine}</span>
                  {c.environment && (
                    <span className={`conn-env-badge conn-env-${c.environment}`}>{c.environment}</span>
                  )}
                  {c.readOnly && <span className="conn-readonly-badge">read-only</span>}
                  {c.tenantSessionVar && (
                    <span className="conn-tenant-badge" title={`tenant session var: ${c.tenantSessionVar}`}>
                      tenant: {c.tenantValue || "(none set)"}
                    </span>
                  )}
                </div>
                <div className="conn-uri mono">{c.redactedUri}</div>
                {Array.isArray(result) && (
                  <div className="conn-dbs">
                    {result.length === 0
                      ? "No databases"
                      : result.map((d) => (
                          <span key={d} className="conn-db-chip mono">
                            {d}
                          </span>
                        ))}
                  </div>
                )}
                {result === "error" && <div className="conn-dbs conn-dbs-error">Connection failed</div>}
              </div>
              <div className="conn-actions">
                <Button variant="ghost" onClick={() => handleTest(c.name)} disabled={result === "loading"}>
                  {result === "loading" ? <Loader2 size={16} className="spin" /> : <CheckCircle2 size={16} />}
                  Test
                </Button>
                {c.tenantSessionVar && (
                  <Button variant="ghost" onClick={() => setTenantTarget(c)}>
                    <Users size={16} /> Switch tenant
                  </Button>
                )}
                <Button variant="danger" onClick={() => setRemoveTarget(c.name)}>
                  <Trash2 size={16} />
                </Button>
              </div>
            </Card>
          );
        })}
      </div>

      {showAdd && (
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
        <select className="input" value={engine} onChange={(e) => setEngine(e.target.value)}>
          {ALL_ENGINES.map((id) => (
            <option key={id} value={id} disabled={!availableEngines.includes(id)}>
              {ENGINE_LABELS[id] ?? id}
            </option>
          ))}
        </select>
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
        <select className="input" value={environment} onChange={(e) => setEnvironment(e.target.value)}>
          <option value="">None</option>
          <option value="dev">Development</option>
          <option value="staging">Staging</option>
          <option value="prod">Production</option>
        </select>
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
