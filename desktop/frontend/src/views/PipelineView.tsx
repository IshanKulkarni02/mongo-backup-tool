import { useEffect, useState } from "react";
import { Database, Play, Plus, Trash2, ArrowUp, ArrowDown, Sparkles } from "lucide-react";
import { ListConnections, TestConnection, ListCollections, RunAggregation, QueryDocuments, GenerateAggregation } from "../../wailsjs/go/main/App";
import { main } from "../../wailsjs/go/models";
import { Button } from "../components/Button";
import { Card } from "../components/Card";
import { EmptyState } from "../components/EmptyState";
import { Skeleton } from "../components/Skeleton";
import { JsonTree } from "../components/JsonTree";
import { AiPanel } from "../components/AiPanel";
import { useToast } from "../components/Toast";
import "./BrowserView.css";
import "./PipelineView.css";

const STAGE_TEMPLATES: Record<string, string> = {
  $match: '{\n  "$match": {}\n}',
  $group: '{\n  "$group": {\n    "_id": null,\n    "count": {"$sum": 1}\n  }\n}',
  $sort: '{\n  "$sort": {}\n}',
  $project: '{\n  "$project": {}\n}',
  $lookup: '{\n  "$lookup": {\n    "from": "",\n    "localField": "",\n    "foreignField": "",\n    "as": ""\n  }\n}',
  $limit: '{\n  "$limit": 10\n}',
};

interface Stage {
  id: number;
  text: string;
}

let nextStageId = 1;

export function PipelineView() {
  const [connections, setConnections] = useState<main.ConnectionInfo[]>([]);
  const [connection, setConnection] = useState("");
  const [databases, setDatabases] = useState<string[]>([]);
  const [database, setDatabase] = useState("");
  const [collections, setCollections] = useState<main.CollectionInfo[]>([]);
  const [collection, setCollection] = useState("");
  const [stages, setStages] = useState<Stage[]>([{ id: nextStageId++, text: STAGE_TEMPLATES.$match }]);
  const [results, setResults] = useState<string[] | null>(null);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState("");
  const [showAi, setShowAi] = useState(false);
  const toast = useToast();

  useEffect(() => {
    ListConnections().then((conns) => {
      const mongoConns = conns.filter((c) => c.capabilities?.aggregation);
      setConnections(mongoConns);
      if (mongoConns.length > 0) setConnection(mongoConns[0].name);
    });
  }, []);

  useEffect(() => {
    if (!connection) return;
    setDatabases([]);
    setDatabase("");
    TestConnection(connection)
      .then((dbs) => {
        setDatabases(dbs);
        if (dbs.length > 0) setDatabase(dbs[0]);
      })
      .catch((e) => toast.push("error", String(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection]);

  useEffect(() => {
    if (!connection || !database) return;
    setCollection("");
    ListCollections(connection, database)
      .then(setCollections)
      .catch((e) => toast.push("error", String(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection, database]);

  function addStage(template: string) {
    setStages((s) => [...s, { id: nextStageId++, text: template }]);
  }

  function updateStage(id: number, text: string) {
    setStages((s) => s.map((st) => (st.id === id ? { ...st, text } : st)));
  }

  function removeStage(id: number) {
    setStages((s) => s.filter((st) => st.id !== id));
  }

  function moveStage(id: number, dir: -1 | 1) {
    setStages((s) => {
      const idx = s.findIndex((st) => st.id === id);
      const swapWith = idx + dir;
      if (idx < 0 || swapWith < 0 || swapWith >= s.length) return s;
      const copy = [...s];
      [copy[idx], copy[swapWith]] = [copy[swapWith], copy[idx]];
      return copy;
    });
  }

  async function run() {
    if (!connection || !database || !collection) return;
    setRunning(true);
    setError("");
    setResults(null);
    try {
      const parsedStages = stages.map((s) => JSON.parse(s.text));
      const pipelineJSON = JSON.stringify(parsedStages);
      const docs = await RunAggregation(connection, database, collection, pipelineJSON);
      setResults(docs);
    } catch (e) {
      setError(String(e));
    } finally {
      setRunning(false);
    }
  }

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">Pipeline</h1>
      </div>

      {connections.length === 0 ? (
        <EmptyState icon={<Database size={32} />} title="No MongoDB connections yet" description="Add a MongoDB connection to build aggregation pipelines here." />
      ) : (
        <>
          <div className="scope-picker">
            <select className="input" value={connection} onChange={(e) => setConnection(e.target.value)}>
              {connections.map((c) => (
                <option key={c.name} value={c.name}>
                  {c.name}
                </option>
              ))}
            </select>
            <select className="input" value={database} onChange={(e) => setDatabase(e.target.value)} disabled={databases.length === 0}>
              <option value="">Select a database</option>
              {databases.map((d) => (
                <option key={d} value={d}>
                  {d}
                </option>
              ))}
            </select>
            <select className="input" value={collection} onChange={(e) => setCollection(e.target.value)} disabled={collections.length === 0}>
              <option value="">Select a collection</option>
              {collections.map((c) => (
                <option key={c.name} value={c.name}>
                  {c.name}
                </option>
              ))}
            </select>
          </div>

          <div className="pipeline-stages">
            {stages.map((stage, i) => (
              <Card key={stage.id} className="pipeline-stage">
                <div className="pipeline-stage-header">
                  <span className="pipeline-stage-num">Stage {i + 1}</span>
                  <div className="pipeline-stage-actions">
                    <button className="icon-btn" disabled={i === 0} onClick={() => moveStage(stage.id, -1)} title="Move up">
                      <ArrowUp size={13} />
                    </button>
                    <button className="icon-btn" disabled={i === stages.length - 1} onClick={() => moveStage(stage.id, 1)} title="Move down">
                      <ArrowDown size={13} />
                    </button>
                    <button className="icon-btn danger" onClick={() => removeStage(stage.id)} title="Remove stage">
                      <Trash2 size={13} />
                    </button>
                  </div>
                </div>
                <textarea
                  className="pipeline-stage-editor mono"
                  value={stage.text}
                  onChange={(e) => updateStage(stage.id, e.target.value)}
                  rows={6}
                />
              </Card>
            ))}
          </div>

          <div className="query-bar">
            {Object.keys(STAGE_TEMPLATES).map((name) => (
              <Button key={name} variant="ghost" onClick={() => addStage(STAGE_TEMPLATES[name])}>
                <Plus size={13} /> {name}
              </Button>
            ))}
          </div>

          <div className="query-bar">
            <Button onClick={run} disabled={running || !connection || !database || !collection}>
              <Play size={14} /> Run pipeline
            </Button>
            <Button variant="ghost" onClick={() => setShowAi(true)} disabled={!connection || !database || !collection}>
              <Sparkles size={14} /> Ask AI
            </Button>
          </div>

          {error && <div className="query-error">{error}</div>}
          {running && <Skeleton height={120} />}
          {!running && results && results.length === 0 && !error && <EmptyState icon={<Database size={28} />} title="No results" />}
          {!running && results && results.length > 0 && (
            <div className="doc-list">
              {results.map((doc, i) => (
                <Card key={i} className="doc-row">
                  <JsonTree json={doc} />
                </Card>
              ))}
            </div>
          )}
        </>
      )}

      {showAi && (
        <AiPanel
          title="Ask AI to write an aggregation pipeline"
          mode="prompt"
          placeholder="e.g. total sales per user, sorted descending"
          onGenerate={async (request) => {
            const sample = await QueryDocuments(connection, database, collection, "{}", "", 0, 1);
            const schemaSample = sample.documents[0] ?? "{}";
            return GenerateAggregation(collection, schemaSample, request ?? "");
          }}
          onInsert={(text) => {
            try {
              const parsed = JSON.parse(text);
              if (Array.isArray(parsed)) {
                setStages(parsed.map((stage) => ({ id: nextStageId++, text: JSON.stringify(stage, null, 2) })));
                return;
              }
            } catch {
              // fall through to showing it as a single stage the user can fix up
            }
            setStages([{ id: nextStageId++, text }]);
          }}
          insertLabel="Use as pipeline"
          onClose={() => setShowAi(false)}
        />
      )}
    </div>
  );
}
