import { useEffect, useState } from "react";
import { GitCompareArrows, Clipboard } from "lucide-react";
import { CompareVectors } from "../../wailsjs/go/main/App";
import { main } from "../../wailsjs/go/models";
import { Button } from "../components/Button";
import { Card } from "../components/Card";
import { useToast } from "../components/Toast";
import { takePendingTransfer, subscribeTransfer } from "../lib/transferStore";
import "./VectorToolView.css";

// VectorToolView compares two embedding vectors — right-click a vector-
// shaped cell in Tables or Query and choose "Send to Vector Compare", or
// paste one directly — and shows the cosine similarity and Euclidean
// distance the recognition/search threshold is usually tuned against.
export function VectorToolView() {
  const [a, setA] = useState("");
  const [b, setB] = useState("");
  const [result, setResult] = useState<main.VectorComparison | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  useEffect(() => {
    const applyOnMount = takePendingTransfer("vector");
    if (applyOnMount?.kind === "vector") {
      (applyOnMount.slot === "a" ? setA : setB)(applyOnMount.value);
    }
    return subscribeTransfer("vector", (t) => {
      if (t.kind !== "vector") return;
      (t.slot === "a" ? setA : setB)(t.value);
      toast.push("success", `Sent to Vector ${t.slot.toUpperCase()}`);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function pasteInto(setter: (v: string) => void) {
    try {
      const text = await navigator.clipboard.readText();
      setter(text);
    } catch {
      toast.push("error", "Couldn't read the clipboard — your browser/OS may require a permission prompt first");
    }
  }

  async function compare() {
    setBusy(true);
    setError("");
    setResult(null);
    try {
      const r = await CompareVectors(a, b);
      setResult(r);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">Vector Compare</h1>
      </div>
      <p className="vector-tool-hint">
        Paste two embedding vectors — pgvector text, a JSON array, or a comma-separated list. Right-click a cell in
        Tables or Query to copy its value, then use Paste below.
      </p>

      <div className="vector-tool-inputs">
        <div className="field">
          <div className="vector-field-header">
            <label className="field-label">Vector A</label>
            <button className="vector-paste-btn" onClick={() => pasteInto(setA)} title="Paste from clipboard">
              <Clipboard size={13} /> Paste
            </button>
          </div>
          <textarea className="input vector-textarea mono" rows={4} value={a} onChange={(e) => setA(e.target.value)} placeholder="[0.12, -0.34, 0.98, ...]" />
        </div>
        <div className="field">
          <div className="vector-field-header">
            <label className="field-label">Vector B</label>
            <button className="vector-paste-btn" onClick={() => pasteInto(setB)} title="Paste from clipboard">
              <Clipboard size={13} /> Paste
            </button>
          </div>
          <textarea className="input vector-textarea mono" rows={4} value={b} onChange={(e) => setB(e.target.value)} placeholder="[0.15, -0.30, 0.95, ...]" />
        </div>
      </div>

      <div className="query-bar">
        <Button onClick={compare} disabled={busy || !a.trim() || !b.trim()}>
          <GitCompareArrows size={14} /> Compare
        </Button>
      </div>

      {error && <div className="query-error">{error}</div>}

      {result && (
        <Card className="vector-result-card">
          <div className="vector-result-row">
            <span>Dimensions</span>
            <span className="mono">{result.dimensions}</span>
          </div>
          <div className="vector-result-row">
            <span>Cosine similarity</span>
            <span className="mono">{result.cosine.toFixed(6)}</span>
          </div>
          <div className="vector-result-row">
            <span>Euclidean distance</span>
            <span className="mono">{result.euclidean.toFixed(6)}</span>
          </div>
          <div className="vector-result-note">
            Cosine similarity closer to 1.0 means more similar direction; Euclidean distance closer to 0 means more
            similar magnitude and direction. Typical face-recognition thresholds treat a Euclidean distance under
            ~0.6 as a match — tune to your own model's calibration.
          </div>
        </Card>
      )}
    </div>
  );
}
