import { useCallback, useRef } from "react";

// useStaleGuard protects an async fetch effect (keyed on a selection —
// connection, database, table, row, etc.) against out-of-order
// resolution: if the user changes the selection again before an earlier
// request resolves, a slower earlier response can otherwise land after a
// newer one and silently overwrite it, leaving state that belongs to a
// selection the UI no longer shows as current.
//
// Usage: call `startRequest()` once, synchronously, at the very start of
// the effect/handler that kicks off the async work — before the promise
// is created — and keep the `isStale` function it returns. Check
// `isStale()` immediately before applying the response to state (and
// before showing an error toast for a failed request); if it reports
// true, a newer request has started since and this response must be
// discarded rather than applied.
//
// This is a request-generation counter, not a real cancellation
// mechanism (Wails-bound calls are plain promises with no abort/cancel
// plumbed through) — the earlier request still runs to completion
// server-side, this only prevents its result from being applied once
// it's no longer wanted.
export function useStaleGuard() {
  const tokenRef = useRef(0);
  return useCallback(() => {
    const token = ++tokenRef.current;
    return () => token !== tokenRef.current;
  }, []);
}
