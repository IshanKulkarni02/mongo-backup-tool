import { useEffect, useState } from "react";
import { Database, LayoutDashboard } from "lucide-react";
import { ListConnections, TestConnection, ListCollections, ListTables } from "../../wailsjs/go/main/App";
import { main } from "../../wailsjs/go/models";
import { Card } from "../components/Card";
import { EmptyState } from "../components/EmptyState";
import { Skeleton } from "../components/Skeleton";
import "./BeginnerOverviewView.css";

type DbSummary = { name: string; groups: number; records: number };
type ConnSummary = { conn: main.ConnectionInfo; databases: DbSummary[] | "error" | null };

// BeginnerOverviewView replaces the SQL-saved-query Dashboard for Beginner
// mode — that view only ever has content once a user has saved a query
// from the (Pro-only) Query editor, so a Mongo-only beginner would always
// see it blank. This instead summarizes what's actually in their connected
// databases using data every connection already exposes.
export function BeginnerOverviewView({ onOpenDatabase }: { onOpenDatabase?: (connection: main.ConnectionInfo, database: string) => void }) {
  const [summaries, setSummaries] = useState<ConnSummary[] | null>(null);

  useEffect(() => {
    let cancelled = false;
    ListConnections().then(async (conns) => {
      const initial: ConnSummary[] = conns.map((c) => ({ conn: c, databases: null }));
      if (!cancelled) setSummaries(initial);

      // Every connection (and every database within it) is fetched
      // concurrently rather than one at a time — each updates its own
      // card via the functional setSummaries below, so there's no shared
      // state to race on, and a slow/unreachable connection no longer
      // blocks every connection listed after it.
      await Promise.all(
        conns.map(async (c) => {
          try {
            const dbNames = await TestConnection(c.name);
            const databases: DbSummary[] = await Promise.all(
              dbNames.map(async (name) => {
                try {
                  if (c.capabilities?.documents) {
                    const cols = await ListCollections(c.name, name);
                    return { name, groups: cols.length, records: cols.reduce((sum, x) => sum + x.docCount, 0) };
                  }
                  if (c.capabilities?.sql) {
                    const tables = await ListTables(c.name, name);
                    return { name, groups: tables.length, records: tables.reduce((sum, x) => sum + x.rowCount, 0) };
                  }
                } catch {
                  // leave this database's counts at zero rather than failing the whole card
                }
                return { name, groups: 0, records: 0 };
              })
            );
            if (!cancelled) {
              setSummaries((prev) => prev?.map((s) => (s.conn.name === c.name ? { ...s, databases } : s)) ?? prev);
            }
          } catch {
            if (!cancelled) {
              setSummaries((prev) => prev?.map((s) => (s.conn.name === c.name ? { ...s, databases: "error" } : s)) ?? prev);
            }
          }
        })
      );
    });
    return () => {
      cancelled = true;
    };
  }, []);

  const totalRecords = (summaries ?? []).reduce(
    (sum, s) => sum + (Array.isArray(s.databases) ? s.databases.reduce((a, d) => a + d.records, 0) : 0),
    0
  );
  const totalGroups = (summaries ?? []).reduce(
    (sum, s) => sum + (Array.isArray(s.databases) ? s.databases.reduce((a, d) => a + d.groups, 0) : 0),
    0
  );

  return (
    <div>
      <div className="view-header">
        <h1 className="view-title">Overview</h1>
      </div>

      {summaries?.length === 0 && (
        <EmptyState
          icon={<LayoutDashboard size={32} />}
          title="Nothing to show yet"
          description="Connect a database from My Databases to see a summary of your data here."
        />
      )}

      {summaries && summaries.length > 0 && (
        <div className="overview-totals">
          <div className="overview-stat">
            <div className="overview-stat-value">{summaries.length}</div>
            <div className="overview-stat-label">database{summaries.length === 1 ? "" : "s"} connected</div>
          </div>
          <div className="overview-stat">
            <div className="overview-stat-value">{totalGroups}</div>
            <div className="overview-stat-label">groups</div>
          </div>
          <div className="overview-stat">
            <div className="overview-stat-value">{totalRecords.toLocaleString()}</div>
            <div className="overview-stat-label">records total</div>
          </div>
        </div>
      )}

      <div className="overview-conn-list">
        {summaries?.map((s) => (
          <Card key={s.conn.name} className="overview-conn-card">
            <div className="overview-conn-name">
              {s.conn.name}
              <span className="overview-engine-badge">{s.conn.engine}</span>
            </div>
            {s.databases === null && <Skeleton height={32} />}
            {s.databases === "error" && <div className="overview-error">Couldn't connect</div>}
            {Array.isArray(s.databases) && s.databases.length === 0 && (
              <div className="overview-empty-hint">No databases found</div>
            )}
            {Array.isArray(s.databases) && s.databases.length > 0 && (
              <div className="overview-db-list">
                {s.databases.map((d) => (
                  <button key={d.name} className="overview-db-row" onClick={() => onOpenDatabase?.(s.conn, d.name)}>
                    <span className="overview-db-name mono">{d.name}</span>
                    <span className="overview-db-meta">
                      {d.groups} group{d.groups === 1 ? "" : "s"} · {d.records.toLocaleString()} record
                      {d.records === 1 ? "" : "s"}
                    </span>
                  </button>
                ))}
              </div>
            )}
          </Card>
        ))}
      </div>
    </div>
  );
}
