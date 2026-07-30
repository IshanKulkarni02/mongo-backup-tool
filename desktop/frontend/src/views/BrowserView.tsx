import { useCallback, useEffect, useState } from "react";
import { Database, Plus, RefreshCw, Trash2, Pencil, Table2, ListTree, ChevronLeft, ChevronRight } from "lucide-react";
import {
  ListConnections,
  TestConnection,
  ListCollections,
  QueryDocuments,
  InsertDocument,
  UpdateDocument,
  DeleteDocument,
  DropCollection,
  CreateCollection,
  ListIndexes,
  CreateIndex,
  DropIndex,
} from "../../wailsjs/go/main/App";
import { main } from "../../wailsjs/go/models";
import { Button } from "../components/Button";
import { Card } from "../components/Card";
import { Input } from "../components/Input";
import { Modal } from "../components/Modal";
import { EmptyState } from "../components/EmptyState";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { Skeleton } from "../components/Skeleton";
import { JsonTree } from "../components/JsonTree";
import { FriendlyDoc } from "../components/FriendlyDoc";
import { useToast } from "../components/Toast";
import { useUIMode } from "../lib/uiMode";
import { Select } from "../components/Select";
import "./BrowserView.css";

function humanSize(n: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${units[i]}`;
}

function extractID(docText: string): string | null {
  try {
    const parsed = JSON.parse(docText);
    if (parsed && typeof parsed === "object" && "_id" in parsed) {
      return JSON.stringify(parsed._id);
    }
  } catch {
    // not valid JSON yet — caller handles the error state
  }
  return null;
}

const PAGE_SIZE = 25;

export function BrowserView({
  initialTarget,
  onConsumeInitialTarget,
}: {
  initialTarget?: { connection: string; database: string } | null;
  onConsumeInitialTarget?: () => void;
} = {}) {
  const { mode } = useUIMode();
  // Captured once at mount so a parent clearing initialTarget afterwards
  // (via onConsumeInitialTarget) doesn't affect this already-mounted view.
  const [pendingTarget] = useState(initialTarget ?? null);
  const [connections, setConnections] = useState<main.ConnectionInfo[]>([]);
  const [connection, setConnection] = useState("");
  const [databases, setDatabases] = useState<string[]>([]);
  const [database, setDatabase] = useState("");
  const [collections, setCollections] = useState<CollectionState>(null);
  const [collection, setCollection] = useState("");
  const [tab, setTab] = useState<"documents" | "indexes">("documents");
  const [dropTarget, setDropTarget] = useState<string | null>(null);
  const [showNewCollection, setShowNewCollection] = useState(false);
  const toast = useToast();

  useEffect(() => {
    ListConnections().then((conns) => {
      setConnections(conns);
      if (pendingTarget && conns.some((c) => c.name === pendingTarget.connection)) {
        setConnection(pendingTarget.connection);
      } else if (conns.length > 0) {
        setConnection(conns[0].name);
      }
      onConsumeInitialTarget?.();
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!connection) return;
    setDatabases([]);
    setDatabase("");
    TestConnection(connection)
      .then((dbs) => {
        setDatabases(dbs);
        if (pendingTarget && pendingTarget.connection === connection && dbs.includes(pendingTarget.database)) {
          setDatabase(pendingTarget.database);
        }
      })
      .catch((e) => toast.push("error", String(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection]);

  const loadCollections = useCallback(() => {
    if (!connection || !database) return;
    ListCollections(connection, database)
      .then(setCollections)
      .catch((e) => toast.push("error", String(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection, database]);

  useEffect(() => {
    setCollection("");
    loadCollections();
  }, [loadCollections]);

  async function handleDropCollection() {
    if (!dropTarget) return;
    try {
      await DropCollection(connection, database, dropTarget);
      toast.push("success", `Dropped ${dropTarget}`);
      if (collection === dropTarget) setCollection("");
      loadCollections();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setDropTarget(null);
    }
  }

  const beginner = mode === "beginner";

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">{beginner ? "My Data" : "Browser"}</h1>
      </div>

      <div className="scope-picker">
        <Select
          value={connection}
          onChange={setConnection}
          options={connections.map((c) => ({ value: c.name, label: c.name }))}
          placeholder="No connections"
          disabled={connections.length === 0}
        />
        <Select
          value={database}
          onChange={setDatabase}
          options={databases.map((d) => ({ value: d, label: d }))}
          placeholder="Select a database"
          disabled={databases.length === 0}
        />
      </div>

      {connection && database && (
        <div className="browser-layout">
          <div className="browser-sidebar">
            <div className="browser-sidebar-header">
              <span>{beginner ? "Groups" : "Collections"}</span>
              <button
                className="icon-btn"
                onClick={() => setShowNewCollection(true)}
                title={beginner ? "Add a group" : "New collection"}
              >
                <Plus size={14} />
              </button>
            </div>
            {collections === null && <Skeleton height={80} />}
            {collections?.length === 0 && (
              <div className="browser-empty-hint">{beginner ? "No groups yet." : "No collections yet."}</div>
            )}
            {collections?.map((c) => (
              <div key={c.name} className={`collection-item ${collection === c.name ? "active" : ""}`}>
                <button className="collection-item-btn" onClick={() => setCollection(c.name)}>
                  <span className="collection-name mono">{c.name}</span>
                  <span className="collection-meta">
                    {c.docCount} {beginner ? "records" : "docs"} · {humanSize(c.storageSize)}
                  </span>
                </button>
                <button
                  className="icon-btn danger"
                  onClick={() => setDropTarget(c.name)}
                  title={beginner ? "Delete group" : "Drop collection"}
                >
                  <Trash2 size={13} />
                </button>
              </div>
            ))}
          </div>

          <div className="browser-main">
            {!collection && (
              <EmptyState
                icon={<Database size={32} />}
                title={beginner ? "Select a group" : "Select a collection"}
                description={
                  beginner
                    ? "Pick a group on the left to see what's inside."
                    : "Pick a collection on the left to browse its documents."
                }
              />
            )}
            {collection && (
              <>
                {!beginner && (
                  <div className="browser-tabs">
                    <button className={`tab-btn ${tab === "documents" ? "active" : ""}`} onClick={() => setTab("documents")}>
                      <Table2 size={14} /> Documents
                    </button>
                    <button className={`tab-btn ${tab === "indexes" ? "active" : ""}`} onClick={() => setTab("indexes")}>
                      <ListTree size={14} /> Indexes
                    </button>
                  </div>
                )}
                {(beginner || tab === "documents") && (
                  <DocumentsPanel connection={connection} database={database} collection={collection} onMutated={loadCollections} />
                )}
                {!beginner && tab === "indexes" && <IndexesPanel connection={connection} database={database} collection={collection} />}
              </>
            )}
          </div>
        </div>
      )}

      {dropTarget && (
        <ConfirmDialog
          title={beginner ? "Delete group" : "Drop collection"}
          message={
            beginner
              ? `Permanently delete "${dropTarget}" and everything in it? This cannot be undone.`
              : `Permanently delete collection "${dropTarget}" and all its documents? This cannot be undone.`
          }
          confirmLabel={beginner ? "Delete" : "Drop"}
          danger
          onConfirm={handleDropCollection}
          onCancel={() => setDropTarget(null)}
        />
      )}

      {showNewCollection && (
        <NewCollectionModal
          connection={connection}
          database={database}
          onClose={() => setShowNewCollection(false)}
          onCreated={() => {
            setShowNewCollection(false);
            loadCollections();
          }}
        />
      )}
    </div>
  );
}

type CollectionState = main.CollectionInfo[] | null;

function NewCollectionModal({
  connection,
  database,
  onClose,
  onCreated,
}: {
  connection: string;
  database: string;
  onClose: () => void;
  onCreated: () => void;
}) {
  const { mode } = useUIMode();
  const beginner = mode === "beginner";
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  async function submit() {
    if (!name) return;
    setBusy(true);
    try {
      await CreateCollection(connection, database, name);
      toast.push("success", beginner ? `Created "${name}"` : `Created collection "${name}"`);
      onCreated();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={beginner ? "Add a group" : "New collection"}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy || !name}>
            {busy ? "Creating..." : "Create"}
          </Button>
        </>
      }
    >
      <Input label={beginner ? "Group name" : "Name"} value={name} onChange={(e) => setName(e.target.value)} autoFocus />
    </Modal>
  );
}

function DocumentsPanel({
  connection,
  database,
  collection,
  onMutated,
}: {
  connection: string;
  database: string;
  collection: string;
  onMutated: () => void;
}) {
  const { mode } = useUIMode();
  const beginner = mode === "beginner";
  const [filter, setFilter] = useState("{}");
  const [sort, setSort] = useState("");
  const [skip, setSkip] = useState(0);
  const [result, setResult] = useState<main.QueryResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [queryError, setQueryError] = useState("");
  const [editing, setEditing] = useState<{ text: string; isNew: boolean } | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const [viewMode, setViewMode] = useState<"friendly" | "json">("friendly");
  const [showAdvancedSearch, setShowAdvancedSearch] = useState(false);
  const toast = useToast();

  const runQuery = useCallback(() => {
    setLoading(true);
    setQueryError("");
    QueryDocuments(connection, database, collection, filter, sort, skip, PAGE_SIZE)
      .then(setResult)
      .catch((e) => setQueryError(String(e)))
      .finally(() => setLoading(false));
  }, [connection, database, collection, filter, sort, skip]);

  useEffect(() => {
    setSkip(0);
  }, [connection, database, collection]);

  useEffect(() => {
    runQuery();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection, database, collection, skip]);

  async function saveEdit() {
    if (!editing) return;
    try {
      if (editing.isNew) {
        await InsertDocument(connection, database, collection, editing.text);
        toast.push("success", beginner ? "Record added" : "Document inserted");
      } else {
        const id = extractID(editing.text);
        if (!id) throw new Error("Document must include _id");
        await UpdateDocument(connection, database, collection, id, editing.text);
        toast.push("success", beginner ? "Record saved" : "Document saved");
      }
      setEditing(null);
      runQuery();
      onMutated();
    } catch (e) {
      toast.push("error", String(e));
    }
  }

  async function handleDelete() {
    if (!deleteTarget) return;
    try {
      await DeleteDocument(connection, database, collection, deleteTarget);
      toast.push("success", beginner ? "Record deleted" : "Document deleted");
      // Deleting the last document(s) on a page can leave `skip` pointing
      // past the new (smaller) total — clamp it back onto a valid page so
      // the grid doesn't end up empty with a nonsensical "26-25 of 25"
      // range. Clamping triggers the [skip]-keyed effect to refetch; if it
      // doesn't change, refetch here instead.
      const newTotal = Math.max(0, (result?.total ?? 1) - 1);
      const clampedSkip = Math.max(0, Math.min(skip, newTotal - PAGE_SIZE));
      if (clampedSkip !== skip) {
        setSkip(clampedSkip);
      } else {
        runQuery();
      }
      onMutated();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setDeleteTarget(null);
    }
  }

  const total = result?.total ?? 0;
  const from = total === 0 ? 0 : skip + 1;
  const to = Math.min(skip + PAGE_SIZE, total);

  return (
    <div>
      {beginner ? (
        <div className="query-bar-beginner">
          <button className="advanced-search-toggle" onClick={() => setShowAdvancedSearch(!showAdvancedSearch)}>
            {showAdvancedSearch ? "Hide search" : "Search / filter records"}
          </button>
          <Button onClick={() => setEditing({ text: '{\n  \n}', isNew: true })}>
            <Plus size={14} /> Add new record
          </Button>
        </div>
      ) : (
        <div className="query-bar">
          <Input placeholder="Filter (Extended JSON), e.g. {}" mono value={filter} onChange={(e) => setFilter(e.target.value)} />
          <Input placeholder="Sort, e.g. {&quot;createdAt&quot;:-1}" mono value={sort} onChange={(e) => setSort(e.target.value)} />
          <Button variant="ghost" onClick={() => (skip === 0 ? runQuery() : setSkip(0))}>
            <RefreshCw size={14} /> Run
          </Button>
          <Button onClick={() => setEditing({ text: '{\n  \n}', isNew: true })}>
            <Plus size={14} /> Insert
          </Button>
        </div>
      )}

      {beginner && showAdvancedSearch && (
        <div className="query-bar-advanced">
          <Input
            label="Filter (advanced, technical)"
            placeholder="{}"
            mono
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
          <Input
            label="Sort (advanced, technical)"
            placeholder='{"createdAt":-1}'
            mono
            value={sort}
            onChange={(e) => setSort(e.target.value)}
          />
          <Button variant="ghost" onClick={() => (skip === 0 ? runQuery() : setSkip(0))}>
            <RefreshCw size={14} /> Search
          </Button>
        </div>
      )}

      {queryError && <div className="query-error">{queryError}</div>}

      {loading && (
        <>
          <Skeleton height={48} />
          <div style={{ height: 6 }} />
          <Skeleton height={48} />
        </>
      )}

      {!loading && result && result.documents.length === 0 && !queryError && (
        <EmptyState
          icon={<Database size={28} />}
          title={beginner ? "No records here yet" : "No matching documents"}
        />
      )}

      {!loading && !beginner && result && result.documents.length > 0 && (
        <div className="doc-view-toggle">
          <button
            className={`tab-btn ${viewMode === "friendly" ? "active" : ""}`}
            onClick={() => setViewMode("friendly")}
          >
            Simple
          </button>
          <button className={`tab-btn ${viewMode === "json" ? "active" : ""}`} onClick={() => setViewMode("json")}>
            JSON
          </button>
        </div>
      )}

      <div className="doc-list">
        {!loading &&
          result?.documents.map((doc, i) => (
            <Card key={i} className="doc-row">
              {beginner || viewMode === "friendly" ? <FriendlyDoc json={doc} /> : <JsonTree json={doc} />}
              <div className="doc-actions">
                <Button variant="ghost" onClick={() => setEditing({ text: doc, isNew: false })}>
                  <Pencil size={14} />
                </Button>
                <Button
                  variant="danger"
                  onClick={() => {
                    const id = extractID(doc);
                    if (id) setDeleteTarget(id);
                  }}
                >
                  <Trash2 size={14} />
                </Button>
              </div>
            </Card>
          ))}
      </div>

      {total > 0 && (
        <div className="pagination">
          <span>
            {from}-{to} of {total}
          </span>
          <Button variant="ghost" disabled={skip === 0} onClick={() => setSkip(Math.max(0, skip - PAGE_SIZE))}>
            <ChevronLeft size={14} />
          </Button>
          <Button variant="ghost" disabled={to >= total} onClick={() => setSkip(skip + PAGE_SIZE)}>
            <ChevronRight size={14} />
          </Button>
        </div>
      )}

      {editing && (
        <Modal
          title={
            beginner
              ? editing.isNew
                ? "Add a new record"
                : "Edit record"
              : editing.isNew
              ? "Insert document"
              : "Edit document"
          }
          onClose={() => setEditing(null)}
          footer={
            <>
              <Button variant="ghost" onClick={() => setEditing(null)}>
                Cancel
              </Button>
              <Button onClick={saveEdit}>Save</Button>
            </>
          }
        >
          {beginner && (
            <p className="doc-editor-hint">
              Records are written in a technical format called JSON — keep the {"{ }"} and quotes as they are, just
              change the values.
            </p>
          )}
          <textarea
            className="doc-editor mono"
            value={editing.text}
            onChange={(e) => setEditing({ ...editing, text: e.target.value })}
            rows={16}
            autoFocus
          />
        </Modal>
      )}

      {deleteTarget && (
        <ConfirmDialog
          title={beginner ? "Delete record" : "Delete document"}
          message={beginner ? "Delete this record? This cannot be undone." : "Delete this document? This cannot be undone."}
          confirmLabel="Delete"
          danger
          onConfirm={handleDelete}
          onCancel={() => setDeleteTarget(null)}
        />
      )}
    </div>
  );
}

function IndexesPanel({ connection, database, collection }: { connection: string; database: string; collection: string }) {
  const [indexes, setIndexes] = useState<main.IndexInfo[] | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [dropTarget, setDropTarget] = useState<string | null>(null);
  const toast = useToast();

  const load = useCallback(() => {
    ListIndexes(connection, database, collection)
      .then(setIndexes)
      .catch((e) => toast.push("error", String(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection, database, collection]);

  useEffect(() => {
    load();
  }, [load]);

  async function handleDrop() {
    if (!dropTarget) return;
    try {
      await DropIndex(connection, database, collection, dropTarget);
      toast.push("success", `Dropped index ${dropTarget}`);
      load();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setDropTarget(null);
    }
  }

  return (
    <div>
      <div className="panel-toolbar">
        <Button onClick={() => setShowCreate(true)}>
          <Plus size={14} /> Create index
        </Button>
      </div>
      {indexes === null && <Skeleton height={40} />}
      <div className="index-list">
        {indexes?.map((idx) => (
          <Card key={idx.name} className="index-row">
            <div>
              <div className="index-name">{idx.name}</div>
              <div className="index-keys mono">
                {idx.keysJson} {idx.unique && <span className="index-unique">unique</span>}
              </div>
            </div>
            {idx.name !== "_id_" && (
              <Button variant="danger" onClick={() => setDropTarget(idx.name)}>
                <Trash2 size={14} />
              </Button>
            )}
          </Card>
        ))}
      </div>

      {showCreate && (
        <CreateIndexModal
          connection={connection}
          database={database}
          collection={collection}
          onClose={() => setShowCreate(false)}
          onCreated={() => {
            setShowCreate(false);
            load();
          }}
        />
      )}

      {dropTarget && (
        <ConfirmDialog
          title="Drop index"
          message={`Drop index "${dropTarget}"? Queries relying on it may become slower.`}
          confirmLabel="Drop"
          danger
          onConfirm={handleDrop}
          onCancel={() => setDropTarget(null)}
        />
      )}
    </div>
  );
}

function CreateIndexModal({
  connection,
  database,
  collection,
  onClose,
  onCreated,
}: {
  connection: string;
  database: string;
  collection: string;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [keys, setKeys] = useState('{"": 1}');
  const [unique, setUnique] = useState(false);
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  async function submit() {
    setBusy(true);
    try {
      await CreateIndex(connection, database, collection, keys, unique);
      toast.push("success", "Index created");
      onCreated();
    } catch (e) {
      toast.push("error", String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title="Create index"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy}>
            {busy ? "Creating..." : "Create"}
          </Button>
        </>
      }
    >
      <Input label="Keys (Extended JSON)" mono value={keys} onChange={(e) => setKeys(e.target.value)} autoFocus />
      <label className="checkbox-row">
        <input type="checkbox" checked={unique} onChange={(e) => setUnique(e.target.checked)} />
        Unique
      </label>
    </Modal>
  );
}
