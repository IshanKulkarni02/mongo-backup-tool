import { useCallback, useState } from "react";
import { Search, Square } from "lucide-react";
import { RunCrossDatabaseSearch, CancelJob } from "../../wailsjs/go/main/App";
import { useJobUpdates, useJobProgress, Job, JobProgress } from "../hooks/useJobs";
import { Button } from "../components/Button";
import { Input } from "../components/Input";
import { Card } from "../components/Card";
import { EmptyState } from "../components/EmptyState";
import "./CrossSearchView.css";

// Mirrors desktop/crossdbsearch.go's CrossSearchMatch. That struct never
// appears in an exported App method's signature (it only travels through
// the generic job-result "any"), so Wails' binding generator never emits it
// into wailsjs/go/models.ts — the shape is declared locally instead.
interface CrossSearchMatch {
  connection: string;
  engine: string;
  database: string;
  namespace: string;
  preview: string;
}

// CrossSearchView searches a value across every saved connection — SQL
// tables via a per-column LIKE, Mongo collections via a sampled-field
// $or/$regex (see desktop/crossdbsearch.go) — spanning every engine, so
// unlike most views here it has no capability gate in App.tsx's NAV.
export function CrossSearchView() {
  const [term, setTerm] = useState("");
  const [jobId, setJobId] = useState<string | null>(null);
  const [progress, setProgress] = useState<JobProgress | null>(null);
  const [results, setResults] = useState<CrossSearchMatch[] | null>(null);
  const [error, setError] = useState("");
  const running = jobId !== null;

  const onJobUpdate = useCallback(
    (job: Job) => {
      if (job.type !== "cross-db-search" || job.id !== jobId || job.status === "running") return;
      setJobId(null);
      setProgress(null);
      if (job.status === "done") {
        setResults((job.result as CrossSearchMatch[] | null) ?? []);
        setError("");
      } else {
        setError(job.message ?? "Search failed");
      }
    },
    [jobId]
  );
  useJobUpdates(onJobUpdate);

  const onProgress = useCallback(
    (p: JobProgress) => {
      if (p.id !== jobId) return;
      setProgress(p);
    },
    [jobId]
  );
  useJobProgress(onProgress);

  async function search() {
    if (!term.trim()) return;
    setResults(null);
    setError("");
    const id = await RunCrossDatabaseSearch(term.trim());
    setJobId(id);
  }

  function cancel() {
    if (jobId) CancelJob(jobId);
  }

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">Cross-Database Search</h1>
      </div>
      <p className="crosssearch-hint">
        Search a value across every saved connection at once — every SQL table's columns, and every Mongo
        collection's fields, spanning all your databases in one pass.
      </p>

      <div className="query-bar">
        <Input
          placeholder="Search term"
          value={term}
          onChange={(e) => setTerm(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && !running && search()}
          disabled={running}
          style={{ flex: 1, maxWidth: 360 }}
        />
        {!running ? (
          <Button onClick={search} disabled={!term.trim()}>
            <Search size={14} /> Search
          </Button>
        ) : (
          <Button variant="danger" onClick={cancel}>
            <Square size={14} /> Cancel
          </Button>
        )}
      </div>

      {running && (
        <div className="crosssearch-progress">
          Searching{progress ? ` ${progress.phase}` : "..."}
          {progress && progress.total > 0 && ` (${progress.current}/${progress.total})`}
        </div>
      )}

      {error && <div className="query-error">{error}</div>}

      {results && results.length === 0 && !error && (
        <EmptyState icon={<Search size={28} />} title="No matches" description="Nothing across your saved connections matched that term." />
      )}

      {results && results.length > 0 && (
        <div className="crosssearch-results">
          {results.map((r, i) => (
            <Card key={i} className="crosssearch-result-card">
              <div className="crosssearch-result-header">
                <span className="crosssearch-result-conn">{r.connection}</span>
                <span className="crosssearch-result-badge">{r.engine}</span>
                <span className="mono">
                  {r.database}.{r.namespace}
                </span>
              </div>
              <div className="crosssearch-result-preview mono">{r.preview}</div>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
