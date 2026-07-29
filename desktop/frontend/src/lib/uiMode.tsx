import { createContext, useContext, useEffect, useState, type PropsWithChildren } from "react";

export type UIMode = "beginner" | "pro";

const STORAGE_KEY = "dbhelm.uiMode";

function loadStoredMode(): UIMode {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    return v === "pro" ? "pro" : "beginner";
  } catch {
    return "beginner";
  }
}

const UIModeContext = createContext<{ mode: UIMode; setMode: (m: UIMode) => void }>({
  mode: "beginner",
  setMode: () => {},
});

// UIModeProvider drives the app-wide Beginner/Pro split: Beginner (the
// default) hides technical nav items and swaps jargon-heavy labels/forms
// for plain-language ones; Pro restores the full, unrestricted app. The
// choice is remembered across restarts via localStorage.
export function UIModeProvider({ children }: PropsWithChildren) {
  const [mode, setModeState] = useState<UIMode>(() => loadStoredMode());

  function setMode(m: UIMode) {
    setModeState(m);
    try {
      localStorage.setItem(STORAGE_KEY, m);
    } catch {
      // localStorage unavailable (e.g. private mode) — mode just won't persist
    }
  }

  useEffect(() => {
    document.documentElement.dataset.uiMode = mode;
  }, [mode]);

  return <UIModeContext.Provider value={{ mode, setMode }}>{children}</UIModeContext.Provider>;
}

export function useUIMode() {
  return useContext(UIModeContext);
}
