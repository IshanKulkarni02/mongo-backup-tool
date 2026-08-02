package main

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
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

// writesData parses pipelineJSON the same way the mongodb engine's
// Aggregate does (a JSON array of stage documents, via
// bson.UnmarshalExtJSON) and reports whether any stage's actual top-level
// key is $out or $merge — the only aggregation stages that persist data.
// Checking the parsed pipeline rather than substring-matching the raw JSON
// text closes two gaps a text-based check can't handle: a stage key
// written with a unicode escape — e.g. {"$merge": ...}, which
// UnmarshalExtJSON resolves to the real key $merge even though the raw
// text contains neither literal substring "$out" nor "$merge" — that a
// substring check would miss entirely; and $out/$merge appearing only as
// an ordinary data *value* (e.g. {"$match":{"tag":"$out"}}) that a
// substring check would wrongly flag as a write. An unparsable pipeline is
// treated as not a write here — Aggregate itself rejects it with a clear
// parse error right after; this only decides whether to run the Safe Mode
// check first.
func writesData(pipelineJSON string) bool {
	var stages []bson.D
	if err := bson.UnmarshalExtJSON([]byte(pipelineJSON), true, &stages); err != nil {
		return false
	}
	for _, stage := range stages {
		for _, elem := range stage {
			if elem.Key == "$out" || elem.Key == "$merge" {
				return true
			}
		}
	}
	return false
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
