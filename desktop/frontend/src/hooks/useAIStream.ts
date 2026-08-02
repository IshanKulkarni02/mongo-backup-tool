import { useCallback, useEffect, useRef, useState } from "react";
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
  // generationRef guards against two start() calls racing: if a second
  // call fires before the first call's streamIdPromise has resolved,
  // unsubRef.current is still null at that point, so the naive
  // unsubRef.current?.() at the top of start() can't tear down a
  // listener that doesn't exist yet. Each start() call captures its own
  // generation number; when its promise resolves, it only registers a
  // listener (or updates state) if it's still the current generation —
  // a call superseded by a later start() is a stale no-op instead of
  // registering a second, permanently-leaked Wails listener whose deltas
  // would interleave with the newer stream's.
  const generationRef = useRef(0);

  const start = useCallback((streamIdPromise: Promise<string>) => {
    const generation = ++generationRef.current;
    unsubRef.current?.();
    unsubRef.current = null;
    streamIdRef.current = null;
    setText("");
    setError("");
    setStreaming(true);
    streamIdPromise
      .then((streamId) => {
        if (generation !== generationRef.current) return; // superseded — never subscribe
        streamIdRef.current = streamId;
        unsubRef.current = EventsOn(`ai:stream:${streamId}`, (ev: StreamEvent) => {
          if (generation !== generationRef.current) return;
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
        if (generation !== generationRef.current) return;
        setError(String(e));
        setStreaming(false);
      });
  }, []);

  // Unsubscribe on unmount — closing the AI panel while a stream is in
  // flight must not leave the Wails listener registered and updating
  // state (or nothing, since the component is gone, but still holding
  // the subscription open) for the rest of the stream.
  useEffect(() => {
    return () => {
      unsubRef.current?.();
    };
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
