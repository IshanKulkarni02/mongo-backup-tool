package main

import (
	"context"
	"fmt"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
)

// CollectionInfo is one collection's summary, shown in the browser's tree.
type CollectionInfo struct {
	Name        string `json:"name"`
	DocCount    int64  `json:"docCount"`
	StorageSize int64  `json:"storageSize"`
}

// docSession acquires the cached document-store session for a connection.
// The caller must invoke the returned release func when done.
func (a *App) docSession(connectionName string) (engine.DocumentSession, func(), error) {
	sess, release, err := a.engines.Acquire(context.Background(), connectionName)
	if err != nil {
		return nil, nil, err
	}
	ds, ok := sess.(engine.DocumentSession)
	if !ok {
		release()
		return nil, nil, fmt.Errorf("connection %q doesn't support document browsing", connectionName)
	}
	return ds, release, nil
}

// ListCollections returns every collection in a database with its document
// count and storage size.
func (a *App) ListCollections(connectionName, database string) ([]CollectionInfo, error) {
	sess, release, err := a.docSession(connectionName)
	if err != nil {
		return nil, err
	}
	defer release()

	infos, err := sess.ListNamespaces(context.Background(), database)
	if err != nil {
		return nil, err
	}
	out := make([]CollectionInfo, len(infos))
	for i, ns := range infos {
		out[i] = CollectionInfo{Name: ns.Name, DocCount: ns.DocCount, StorageSize: ns.StorageSize}
	}
	return out, nil
}

// QueryResult is a page of documents, each rendered as relaxed Extended
// JSON (human-readable — plain numbers/strings where unambiguous, unlike
// the snapshot engine's canonical mode which favors deterministic hashing
// over readability).
type QueryResult struct {
	Documents []string `json:"documents"`
	Total     int64    `json:"total"`
	Skip      int      `json:"skip"`
	Limit     int      `json:"limit"`
}

// QueryDocuments runs a filtered, sorted, paginated find. filterJSON and
// sortJSON are Extended JSON text (or empty for "match everything" /
// "no explicit sort").
func (a *App) QueryDocuments(connectionName, database, collection, filterJSON, sortJSON string, skip, limit int) (QueryResult, error) {
	sess, release, err := a.docSession(connectionName)
	if err != nil {
		return QueryResult{}, err
	}
	defer release()

	page, err := sess.QueryDocuments(context.Background(), engine.DocQuery{
		Database:   database,
		Namespace:  collection,
		FilterJSON: filterJSON,
		SortJSON:   sortJSON,
		Skip:       skip,
		Limit:      limit,
	})
	if err != nil {
		return QueryResult{}, err
	}
	return QueryResult{Documents: page.Documents, Total: page.Total, Skip: page.Skip, Limit: page.Limit}, nil
}

// InsertDocument inserts a new document (Extended JSON text).
func (a *App) InsertDocument(connectionName, database, collection, docJSON string) error {
	if err := a.requireWritable(connectionName); err != nil {
		return err
	}
	sess, release, err := a.docSession(connectionName)
	if err != nil {
		return err
	}
	defer release()
	return sess.InsertDocument(context.Background(), database, collection, docJSON)
}

// UpdateDocument replaces a document, matched by its original _id (captured
// before editing, so the edit can't accidentally retarget itself by
// changing _id in the textarea).
func (a *App) UpdateDocument(connectionName, database, collection, originalIDJSON, docJSON string) error {
	if err := a.requireWritable(connectionName); err != nil {
		return err
	}
	sess, release, err := a.docSession(connectionName)
	if err != nil {
		return err
	}
	defer release()
	return sess.UpdateDocument(context.Background(), database, collection, originalIDJSON, docJSON)
}

// DeleteDocument removes one document by _id.
func (a *App) DeleteDocument(connectionName, database, collection, idJSON string) error {
	if err := a.requireWritable(connectionName); err != nil {
		return err
	}
	sess, release, err := a.docSession(connectionName)
	if err != nil {
		return err
	}
	defer release()
	return sess.DeleteDocument(context.Background(), database, collection, idJSON)
}

// DropCollection permanently deletes a collection and all its documents.
func (a *App) DropCollection(connectionName, database, collection string) error {
	if err := a.requireWritable(connectionName); err != nil {
		return err
	}
	sess, release, err := a.docSession(connectionName)
	if err != nil {
		return err
	}
	defer release()
	return sess.DropNamespace(context.Background(), database, collection)
}

// CreateCollection explicitly creates an empty collection (Mongo normally
// creates collections implicitly on first insert; this is for when a user
// wants an empty one to exist, e.g. before setting up indexes).
func (a *App) CreateCollection(connectionName, database, collection string) error {
	if err := a.requireWritable(connectionName); err != nil {
		return err
	}
	sess, release, err := a.docSession(connectionName)
	if err != nil {
		return err
	}
	defer release()
	return sess.CreateNamespace(context.Background(), database, collection)
}
