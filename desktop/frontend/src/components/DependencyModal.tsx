import { useCallback, useEffect, useState } from "react";
import { CheckCircle2, XCircle } from "lucide-react";
import {
  CheckDependencies,
  ManualInstallInstructions,
  AutoInstallAvailable,
  InstallDependencies,
} from "../../wailsjs/go/main/App";
import { main } from "../../wailsjs/go/models";
import { Job, useJobUpdates } from "../hooks/useJobs";
import { Modal } from "./Modal";
import { Button } from "./Button";
import "./DependencyModal.css";

type Choice = "checking" | "manual" | "automatic" | "installing" | null;

export function DependencyModal({ onResolved }: { onResolved: () => void }) {
  const [statuses, setStatuses] = useState<main.DependencyStatus[] | null>(null);
  const [autoAvailable, setAutoAvailable] = useState(false);
  const [choice, setChoice] = useState<Choice>(null);
  const [instructions, setInstructions] = useState<string[]>([]);
  const [installLog, setInstallLog] = useState<string[]>([]);
  const [installJobID, setInstallJobID] = useState<string | null>(null);

  const check = useCallback(() => {
    Promise.all([CheckDependencies(), AutoInstallAvailable()]).then(([deps, auto]) => {
      setStatuses(deps);
      setAutoAvailable(auto);
      if (deps.every((d) => d.installed)) {
        onResolved();
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    check();
  }, [check]);

  const onJobUpdate = useCallback(
    (job: Job) => {
      if (job.id !== installJobID) return;
      if (job.status === "running") return;
      if (job.status === "done") {
        setInstallLog((Array.isArray(job.result) ? (job.result as string[]) : []));
        check();
      } else if (job.status === "failed") {
        setInstallLog((prev) => [...prev, "Failed: " + (job.message ?? "unknown error")]);
        setChoice("manual");
        ManualInstallInstructions().then(setInstructions);
      }
    },
    [installJobID, check]
  );
  useJobUpdates(onJobUpdate);

  async function startAutoInstall() {
    setChoice("installing");
    setInstallLog(["Installing..."]);
    const id = await InstallDependencies();
    setInstallJobID(id);
  }

  async function showManual() {
    setChoice("manual");
    setInstructions(await ManualInstallInstructions());
  }

  if (statuses === null || statuses.every((d) => d.installed)) {
    return null;
  }

  return (
    <Modal title="Dependencies needed" onClose={onResolved}>
      <div className="dep-list">
        {statuses.map((d) => (
          <div key={d.name} className="dep-row">
            {d.installed ? <CheckCircle2 size={16} className="dep-ok" /> : <XCircle size={16} className="dep-missing" />}
            <span className="mono">{d.name}</span>
            <span className="dep-desc">{d.description}</span>
          </div>
        ))}
      </div>

      <p className="dep-note">
        These are needed for classic backups (snapshots work without them). You can also skip this and continue —
        backup/restore will just fail with a clear error until they're installed.
      </p>

      {choice === null && (
        <div className="dep-actions">
          <Button variant="ghost" onClick={showManual}>
            View manual instructions
          </Button>
          {autoAvailable && <Button onClick={startAutoInstall}>Install automatically</Button>}
          <Button variant="ghost" onClick={onResolved}>
            Continue anyway
          </Button>
        </div>
      )}

      {choice === "manual" && (
        <>
          <pre className="dep-instructions mono">{instructions.join("\n")}</pre>
          <div className="dep-actions">
            <Button variant="ghost" onClick={check}>
              Re-check
            </Button>
            <Button variant="ghost" onClick={onResolved}>
              Continue anyway
            </Button>
          </div>
        </>
      )}

      {(choice === "installing") && (
        <>
          <pre className="dep-instructions mono">{installLog.join("\n")}</pre>
        </>
      )}
    </Modal>
  );
}
