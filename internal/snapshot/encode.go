package snapshot

import (
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// canonicalBytes renders a document as canonical Extended JSON: deterministic
// field types (e.g. always $numberLong rather than ambiguous ints) so that
// byte-identical content always hashes identically. Field order is preserved
// from the document as stored, so a pure field-reorder counts as a
// modification — an accepted tradeoff for a straightforward, dependency-free
// implementation.
func canonicalBytes(doc bson.D) ([]byte, error) {
	return bson.MarshalExtJSON(doc, true, false)
}

// idKey returns a stable string form of a document's _id, used to match the
// "same" document across snapshots regardless of content changes.
func idKey(doc bson.D) (string, error) {
	for _, e := range doc {
		if e.Key != "_id" {
			continue
		}
		if oid, ok := e.Value.(bson.ObjectID); ok {
			return "oid:" + oid.Hex(), nil
		}
		b, err := bson.MarshalExtJSON(bson.D{{Key: "_id", Value: e.Value}}, true, false)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return "", fmt.Errorf("document has no _id field")
}
