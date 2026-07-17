package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
)

// aggSession acquires the cached aggregation-capable session for a
// connection. The caller must invoke the returned release func when done.
func (a *App) aggSession(connectionName string) (engine.AggregateSession, func(), error) {
	sess, release, err := a.engines.Acquire(context.Background(), connectionName)
	if err != nil {
		return nil, nil, err
	}
	as, ok := sess.(engine.AggregateSession)
	if !ok {
		release()
		return nil, nil, fmt.Errorf("connection %q doesn't support aggregation pipelines", connectionName)
	}
	return as, release, nil
}

// writesData is a heuristic (substring match on stage names, not a full
// pipeline parse — same trade-off internal/engine/safeguard makes for SQL)
// for whether a pipeline includes a stage that persists data rather than
// only reading it.
func writesData(pipelineJSON string) bool {
	return strings.Contains(pipelineJSON, `"$out"`) || strings.Contains(pipelineJSON, `"$merge"`)
}

// RunAggregation runs an aggregation pipeline (a JSON array of stage
// documents, Extended JSON text) and returns each result document as
// relaxed Extended JSON. Pipelines containing a $out or $merge stage
// persist data and are refused on read-only connections, same as any
// other write.
func (a *App) RunAggregation(connectionName, database, collection, pipelineJSON string) ([]string, error) {
	if writesData(pipelineJSON) {
		if err := a.requireWritable(connectionName); err != nil {
			return nil, err
		}
	}
	sess, release, err := a.aggSession(connectionName)
	if err != nil {
		return nil, err
	}
	defer release()
	return sess.Aggregate(context.Background(), database, collection, pipelineJSON)
}
