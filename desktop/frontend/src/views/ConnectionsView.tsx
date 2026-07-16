import { useEffect, useState } from "react";
import { Plus, Database, Trash2, CheckCircle2, Loader2 } from "lucide-react";
import {
  ListConnections,
  AddConnection,
  RemoveConnection,
  TestConnection,
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
  const toast = useToast();

  const load = () => ListConnections().then(setConnections);

  useEffect(() => {
    load();
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
                <div className="conn-name">{c.name}</div>
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
    </div>
  );
}

function AddConnectionModal({ onClose, onAdded }: { onClose: () => void; onAdded: () => void }) {
  const [name, setName] = useState("");
  const [uri, setUri] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  async function submit() {
    if (!name || !uri) {
      setError("Both a name and a URI are required");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await AddConnection(name, uri);
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
      <Input
        label="URI"
        placeholder="mongodb://localhost:27017"
        value={uri}
        onChange={(e) => setUri(e.target.value)}
        mono
        error={error}
      />
    </Modal>
  );
}
