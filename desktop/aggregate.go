package main

import (
	"context"
	"fmt"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/service"
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

// RunAggregation runs an aggregation pipeline (a JSON array of stage
// documents, Extended JSON text) and returns each result document as
// relaxed Extended JSON. Pipelines containing a $out or $merge stage
// persist data and are refused on read-only connections, same as any
// other write.
func (a *App) RunAggregation(connectionName, database, collection, pipelineJSON string) ([]string, error) {
	if service.WritesData(pipelineJSON) {
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
