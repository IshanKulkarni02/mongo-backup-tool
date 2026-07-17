package main

import "context"

// IndexInfo is one index, rendered for display (not round-tripped back into
// a create call — CreateIndex takes its own fresh keysJSON/unique input).
type IndexInfo struct {
	Name     string `json:"name"`
	KeysJSON string `json:"keysJson"`
	Unique   bool   `json:"unique"`
}

// ListIndexes returns every index on a collection.
func (a *App) ListIndexes(connectionName, database, collection string) ([]IndexInfo, error) {
	sess, release, err := a.docSession(connectionName)
	if err != nil {
		return nil, err
	}
	defer release()

	infos, err := sess.ListIndexes(context.Background(), database, collection)
	if err != nil {
		return nil, err
	}
	out := make([]IndexInfo, len(infos))
	for i, ix := range infos {
		out[i] = IndexInfo{Name: ix.Name, KeysJSON: ix.KeysJSON, Unique: ix.Unique}
	}
	return out, nil
}

// CreateIndex builds a new index from Extended JSON keys, e.g. {"email":1}
// or {"location":"2dsphere"}.
func (a *App) CreateIndex(connectionName, database, collection, keysJSON string, unique bool) error {
	if err := a.requireWritable(connectionName); err != nil {
		return err
	}
	sess, release, err := a.docSession(connectionName)
	if err != nil {
		return err
	}
	defer release()
	return sess.CreateIndex(context.Background(), database, collection, keysJSON, unique)
}

// DropIndex removes an index by name.
func (a *App) DropIndex(connectionName, database, collection, name string) error {
	if err := a.requireWritable(connectionName); err != nil {
		return err
	}
	sess, release, err := a.docSession(connectionName)
	if err != nil {
		return err
	}
	defer release()
	return sess.DropIndex(context.Background(), database, collection, name)
}
