import { useEffect } from "react";
import { EventsOn } from "../../wailsjs/runtime/runtime";

export interface Job {
  id: string;
  type: string;
  status: "running" | "done" | "failed";
  message?: string;
  result?: unknown;
}

export interface JobProgress {
  id: string;
  phase: string;
  current: number;
  total: number;
  line?: string;
}

// Wails' EventsOn has no per-listener unsubscribe (EventsOff removes every
// listener for the event name), so multiple components can't each call
// EventsOn/EventsOff independently without stepping on each other. Instead,
// the Wails event is subscribed to exactly once at module scope, fanning
// out to any number of React-side listeners that come and go freely.
const listeners = new Set<(job: Job) => void>();
const progressListeners = new Set<(progress: JobProgress) => void>();
let subscribed = false;

function ensureSubscribed() {
  if (subscribed) return;
  subscribed = true;
  EventsOn("job:update", (job: Job) => {
    listeners.forEach((fn) => fn(job));
  });
  EventsOn("job:progress", (progress: JobProgress) => {
    progressListeners.forEach((fn) => fn(progress));
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

/** Subscribes to live progress for long-running background jobs. */
export function useJobProgress(onProgress: (progress: JobProgress) => void) {
  useEffect(() => {
    ensureSubscribed();
    progressListeners.add(onProgress);
    return () => {
      progressListeners.delete(onProgress);
    };
  }, [onProgress]);
}
