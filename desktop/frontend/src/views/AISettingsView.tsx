import { useCallback, useEffect, useState } from "react";
import { CheckCircle2, XCircle, Download, RefreshCw } from "lucide-react";
import {
  GetAISettings,
  SaveAISettings,
  CheckOllama,
  InstallOllama,
  ListOllamaModels,
  PullOllamaModel,
  CancelJob,
} from "../../wailsjs/go/main/App";
import { depmanager, ai } from "../../wailsjs/go/models";
import { useJobProgress, useJobUpdates, Job } from "../hooks/useJobs";
import { Button } from "../components/Button";
import { Card } from "../components/Card";
import { Input } from "../components/Input";
import { Skeleton } from "../components/Skeleton";
import { useToast } from "../components/Toast";
import "./AISettingsView.css";

const PROVIDERS = [
  { id: "ollama", label: "Ollama (local, private)" },
  { id: "anthropic", label: "Anthropic (BYOK)" },
  { id: "openai", label: "OpenAI (BYOK)" },
];

export function AISettingsView() {
  const [providerId, setProviderId] = useState("ollama");
  const [model, setModel] = useState("");
  const [ollamaHost, setOllamaHost] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [hasApiKey, setHasApiKey] = useState(false);
  const [saving, setSaving] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const toast = useToast();

  useEffect(() => {
    GetAISettings().then((s) => {
      setProviderId(s.providerId || "ollama");
      setModel(s.model);
      setOllamaHost(s.ollamaHost);
      setHasApiKey(s.hasApiKey);
      setLoaded(true);
    });
  }, []);

  async function save() {
    setSaving(true);
    try {
      await SaveAISettings(providerId, model, ollamaHost, apiKey);
      setApiKey("");
      if (apiKey) setHasApiKey(true);
      toast.push("success", "AI settings saved");
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setSaving(false);
    }
  }

  if (!loaded) return <Skeleton height={200} />;

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">AI</h1>
      </div>

      <Card className="ai-settings-card">
        <h2 className="ai-settings-section-title">Provider</h2>
        <div className="field">
          <label className="field-label">Provider</label>
          <select className="input" value={providerId} onChange={(e) => setProviderId(e.target.value)}>
            {PROVIDERS.map((p) => (
              <option key={p.id} value={p.id}>
                {p.label}
              </option>
            ))}
          </select>
        </div>
        <Input
          label="Model"
          placeholder={providerId === "ollama" ? "e.g. qwen2.5-coder" : providerId === "anthropic" ? "e.g. claude-sonnet-4-20250514" : "e.g. gpt-4o"}
          value={model}
          onChange={(e) => setModel(e.target.value)}
        />
        {providerId === "ollama" && (
          <Input label="Ollama host (optional)" placeholder="http://localhost:11434" value={ollamaHost} onChange={(e) => setOllamaHost(e.target.value)} mono />
        )}
        {(providerId === "anthropic" || providerId === "openai") && (
          <Input
            label={hasApiKey ? "API key (already set — enter a new one to replace it)" : "API key"}
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder={hasApiKey ? "••••••••" : ""}
          />
        )}
        <Button onClick={save} disabled={saving || !model}>
          {saving ? "Saving..." : "Save"}
        </Button>
      </Card>

      {providerId === "ollama" && <OllamaManager ollamaHost={ollamaHost} />}
    </div>
  );
}

function OllamaManager({ ollamaHost }: { ollamaHost: string }) {
  const [status, setStatus] = useState<depmanager.OllamaStatus | null>(null);
  const [models, setModels] = useState<ai.OllamaModel[] | null>(null);
  const [installJobId, setInstallJobId] = useState<string | null>(null);
  const [installing, setInstalling] = useState(false);
  const [pullModel, setPullModel] = useState("");
  const [pullJobId, setPullJobId] = useState<string | null>(null);
  const [pullProgress, setPullProgress] = useState<{ Status: string; Completed: number; Total: number } | null>(null);
  const toast = useToast();

  const checkStatus = useCallback(() => {
    CheckOllama().then(setStatus);
  }, []);

  const loadModels = useCallback(() => {
    ListOllamaModels(ollamaHost).then(setModels).catch(() => setModels([]));
  }, [ollamaHost]);

  useEffect(() => {
    checkStatus();
  }, [checkStatus]);

  useEffect(() => {
    if (status?.Running) loadModels();
  }, [status, loadModels]);

  const onJobUpdate = useCallback(
    (job: Job) => {
      if (job.type === "ollama-install" && job.id === installJobId && job.status !== "running") {
        setInstalling(false);
        setInstallJobId(null);
        if (job.status === "done") {
          toast.push("success", "Ollama installed");
          checkStatus();
        } else {
          toast.push("error", job.message ?? "Install failed");
        }
      }
      if (job.type === "ollama-pull" && job.id === pullJobId && job.status !== "running") {
        setPullJobId(null);
        setPullProgress(null);
        if (job.status === "done") {
          toast.push("success", `Pulled ${pullModel}`);
          loadModels();
        } else {
          toast.push("error", job.message ?? "Pull failed");
        }
      }
    },
    [installJobId, pullJobId, pullModel, checkStatus, loadModels, toast]
  );
  useJobUpdates(onJobUpdate);

  const onProgress = useCallback(
    (p: { id: string; phase: string; current: number; total: number }) => {
      if (p.id === pullJobId) {
        setPullProgress({ Status: p.phase, Completed: p.current, Total: p.total });
      }
    },
    [pullJobId]
  );
  useJobProgress(onProgress);

  async function install() {
    setInstalling(true);
    const id = await InstallOllama();
    setInstallJobId(id);
  }

  async function pull() {
    if (!pullModel.trim()) return;
    const id = await PullOllamaModel(ollamaHost, pullModel.trim());
    setPullJobId(id);
  }

  function cancelPull() {
    if (pullJobId) CancelJob(pullJobId);
  }

  if (!status) return <Skeleton height={100} />;

  return (
    <Card className="ai-settings-card">
      <h2 className="ai-settings-section-title">Ollama</h2>
      <div className="ollama-status-row">
        {status.Running ? (
          <span className="ollama-status ok">
            <CheckCircle2 size={14} /> Running
          </span>
        ) : status.Installed ? (
          <span className="ollama-status warn">
            <XCircle size={14} /> Installed but not running — launch the Ollama app
          </span>
        ) : (
          <span className="ollama-status warn">
            <XCircle size={14} /> Not installed
          </span>
        )}
        <Button variant="ghost" onClick={checkStatus}>
          <RefreshCw size={13} /> Recheck
        </Button>
        {!status.Installed && (
          <Button onClick={install} disabled={installing}>
            {installing ? "Installing..." : "Install Ollama"}
          </Button>
        )}
      </div>

      {status.Running && (
        <>
          <h3 className="ai-settings-subtitle">Installed models</h3>
          {models === null && <Skeleton height={40} />}
          {models?.length === 0 && <div className="browser-empty-hint">No models pulled yet.</div>}
          <div className="ollama-model-list">
            {models?.map((m) => (
              <span key={m.name} className="ollama-model-chip mono">
                {m.name}
              </span>
            ))}
          </div>

          <div className="ollama-pull-row">
            <Input placeholder="e.g. qwen2.5-coder" value={pullModel} onChange={(e) => setPullModel(e.target.value)} mono />
            {pullJobId ? (
              <Button variant="danger" onClick={cancelPull}>
                Cancel
              </Button>
            ) : (
              <Button onClick={pull} disabled={!pullModel.trim()}>
                <Download size={14} /> Pull model
              </Button>
            )}
          </div>
          {pullJobId && (
            <div className="ollama-pull-progress">
              {pullProgress ? (
                <>
                  {pullProgress.Status}
                  {pullProgress.Total > 0 && ` — ${Math.round((pullProgress.Completed / pullProgress.Total) * 100)}%`}
                </>
              ) : (
                "Starting..."
              )}
            </div>
          )}
        </>
      )}
    </Card>
  );
}
