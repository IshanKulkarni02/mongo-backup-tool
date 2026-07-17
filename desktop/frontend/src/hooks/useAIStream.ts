import { useCallback, useRef, useState } from "react";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { CancelJob } from "../../wailsjs/go/main/App";

interface StreamEvent {
  delta?: string;
  done?: boolean;
  error?: string;
}

// useAIStream subscribes to the dynamically-named "ai:stream:<id>" event a
// Go AI binding starts (see desktop/ai.go's runAIStream), assembling
// incremental deltas into one string. Each stream ID gets its own Wails
// event name, so — unlike job:update's single shared event — a plain
// per-call EventsOn/unsubscribe is enough; no fan-out needed.
export function useAIStream() {
  const [text, setText] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [error, setError] = useState("");
  const unsubRef = useRef<(() => void) | null>(null);
  const streamIdRef = useRef<string | null>(null);

  const start = useCallback((streamIdPromise: Promise<string>) => {
    unsubRef.current?.();
    streamIdRef.current = null;
    setText("");
    setError("");
    setStreaming(true);
    streamIdPromise
      .then((streamId) => {
        streamIdRef.current = streamId;
        unsubRef.current = EventsOn(`ai:stream:${streamId}`, (ev: StreamEvent) => {
          if (ev.error) {
            setError(ev.error);
            setStreaming(false);
            unsubRef.current?.();
            return;
          }
          if (ev.delta) setText((t) => t + ev.delta);
          if (ev.done) {
            setStreaming(false);
            unsubRef.current?.();
          }
        });
      })
      .catch((e) => {
        setError(String(e));
        setStreaming(false);
      });
  }, []);

  // cancel stops the in-flight generation server-side (the same
  // CancelJob binding SQL queries use — an AI stream's ID doubles as a
  // cancelable-job ID, see runAIStream). The "ai:stream:<id>" handler
  // above still fires a final {error: "canceled", done: true} event, so
  // UI state settles the normal way rather than needing special handling
  // here.
  const cancel = useCallback(() => {
    if (streamIdRef.current) CancelJob(streamIdRef.current);
  }, []);

  return { text, streaming, error, start, cancel };
}
