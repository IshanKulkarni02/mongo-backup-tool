package snapshot

import "encoding/json"

// canonicalRowBytes renders a SQL row as canonical JSON for content
// hashing — the SQL analog of encode.go's canonicalBytes. encoding/json
// already sorts map[string]T keys alphabetically and already base64-
// encodes []byte values, so both properties a deterministic, binary-safe
// canonical form needs come for free from a plain json.Marshal; no bespoke
// canonicalization like Mongo's Extended JSON encoding is needed here.
func canonicalRowBytes(row map[string]any) ([]byte, error) {
	return json.Marshal(row)
}

// rowIdentity returns a stable string form of a row's primary key, used to
// match the "same" row across snapshots regardless of content changes —
// the SQL analog of encode.go's idKey. Encoding the PK values as a JSON
// array (not a map) preserves column order, so composite keys are handled
// correctly without extra bookkeeping: (user_id, org_id) and
// (org_id, user_id) never collide even if some other table happened to
// have columns of the same names in the opposite order, since identity is
// additionally scoped per-table via the DocRef namespace (manifestID,
// table) it's stored under.
func rowIdentity(pkColumns []string, row map[string]any) (string, error) {
	values := make([]any, len(pkColumns))
	for i, col := range pkColumns {
		values[i] = row[col]
	}
	b, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
