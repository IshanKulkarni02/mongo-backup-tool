package snapshot

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestCanonicalBytesStableAcrossCalls(t *testing.T) {
	doc := bson.D{{Key: "_id", Value: 1}, {Key: "name", Value: "widget"}, {Key: "qty", Value: int64(3)}}

	b1, err := canonicalBytes(doc)
	if err != nil {
		t.Fatalf("canonicalBytes: %v", err)
	}
	b2, err := canonicalBytes(doc)
	if err != nil {
		t.Fatalf("canonicalBytes: %v", err)
	}
	if string(b1) != string(b2) {
		t.Fatalf("canonicalBytes not stable: %q vs %q", b1, b2)
	}
}

func TestCanonicalBytesDifferForDifferentContent(t *testing.T) {
	a := bson.D{{Key: "_id", Value: 1}, {Key: "name", Value: "a"}}
	b := bson.D{{Key: "_id", Value: 1}, {Key: "name", Value: "b"}}

	ba, err := canonicalBytes(a)
	if err != nil {
		t.Fatal(err)
	}
	bb, err := canonicalBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(ba) == string(bb) {
		t.Fatalf("different documents produced identical canonical bytes")
	}
}

func TestIdKeyObjectID(t *testing.T) {
	oid := bson.NewObjectID()
	doc := bson.D{{Key: "_id", Value: oid}, {Key: "name", Value: "widget"}}

	key, err := idKey(doc)
	if err != nil {
		t.Fatalf("idKey: %v", err)
	}
	want := "oid:" + oid.Hex()
	if key != want {
		t.Errorf("idKey = %q, want %q", key, want)
	}
}

func TestIdKeyNonObjectID(t *testing.T) {
	doc := bson.D{{Key: "_id", Value: "custom-id-123"}, {Key: "name", Value: "widget"}}

	key, err := idKey(doc)
	if err != nil {
		t.Fatalf("idKey: %v", err)
	}
	if key == "" {
		t.Errorf("idKey returned empty string")
	}

	// Same _id value should always produce the same key.
	key2, err := idKey(doc)
	if err != nil {
		t.Fatal(err)
	}
	if key != key2 {
		t.Errorf("idKey not stable: %q vs %q", key, key2)
	}
}

func TestIdKeyMissing(t *testing.T) {
	doc := bson.D{{Key: "name", Value: "widget"}}
	if _, err := idKey(doc); err == nil {
		t.Fatalf("expected an error for a document with no _id")
	}
}
