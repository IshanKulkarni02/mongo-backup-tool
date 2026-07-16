import { useEffect } from "react";
import { EventsOn } from "../../wailsjs/runtime/runtime";

export interface Job {
  id: string;
  type: string;
  status: "running" | "done" | "failed";
  message?: string;
  result?: unknown;
}

// Wails' EventsOn has no per-listener unsubscribe (EventsOff removes every
// listener for the event name), so multiple components can't each call
// EventsOn/EventsOff independently without stepping on each other. Instead,
// the Wails event is subscribed to exactly once at module scope, fanning
// out to any number of React-side listeners that come and go freely.
const listeners = new Set<(job: Job) => void>();
let subscribed = false;

function ensureSubscribed() {
  if (subscribed) return;
  subscribed = true;
  EventsOn("job:update", (job: Job) => {
    listeners.forEach((fn) => fn(job));
  });
}

/** Subscribes to job:update events pushed from the Go backend (jobs.go). */
export function useJobUpdates(onUpdate: (job: Job) => void) {
  useEffect(() => {
    ensureSubscribed();
    listeners.add(onUpdate);
    return () => {
      listeners.delete(onUpdate);
    };
  }, [onUpdate]);
}
