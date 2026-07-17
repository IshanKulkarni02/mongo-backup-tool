import { useEffect, useRef, useState } from "react";
import { Sparkles, Square } from "lucide-react";
import { useAIStream } from "../hooks/useAIStream";
import { Modal } from "./Modal";
import { Button } from "./Button";
import { Input } from "./Input";
import "./AiPanel.css";

interface Props {
  title: string;
  /** "prompt" shows a request input the user fills in; "auto" starts
   * generating immediately on open (explain-tuning, error-fix, mocking —
   * features whose input is already fully determined by context). */
  mode: "prompt" | "auto";
  placeholder?: string;
  onGenerate: (request?: string) => Promise<string>;
  /** Omit for advice-only features (explain tuning) with nothing to insert. */
  onInsert?: (text: string) => void;
  insertLabel?: string;
  onClose: () => void;
}

export function AiPanel({ title, mode, placeholder, onGenerate, onInsert, insertLabel = "Insert", onClose }: Props) {
  const [request, setRequest] = useState("");
  const { text, streaming, error, start, cancel } = useAIStream();
  const autoStarted = useRef(false);

  useEffect(() => {
    if (mode === "auto" && !autoStarted.current) {
      autoStarted.current = true;
      start(onGenerate());
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode]);

  function generate() {
    if (mode === "prompt" && !request.trim()) return;
    start(onGenerate(mode === "prompt" ? request : undefined));
  }

  return (
    <Modal
      title={title}
      onClose={onClose}
      footer={
        <>
          {streaming && (
            <Button variant="danger" onClick={cancel}>
              <Square size={14} /> Cancel
            </Button>
          )}
          <Button variant="ghost" onClick={onClose}>
            Close
          </Button>
          {onInsert && (
            <Button onClick={() => { onInsert(text); onClose(); }} disabled={!text || streaming}>
              {insertLabel}
            </Button>
          )}
        </>
      }
    >
      {mode === "prompt" && (
        <div className="ai-prompt-row">
          <Input
            placeholder={placeholder}
            value={request}
            onChange={(e) => setRequest(e.target.value)}
            autoFocus
            onKeyDown={(e) => {
              if (e.key === "Enter") generate();
            }}
          />
          <Button onClick={generate} disabled={streaming}>
            <Sparkles size={14} /> {streaming ? "Generating..." : "Generate"}
          </Button>
        </div>
      )}
      {error && <div className="query-error">{error}</div>}
      {(text || streaming) && (
        <pre className="ai-output mono">
          {text}
          {streaming && <span className="ai-cursor">▍</span>}
        </pre>
      )}
    </Modal>
  );
}
