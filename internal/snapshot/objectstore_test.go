package snapshot

import (
	"bytes"
	"fmt"
	"sort"
	"testing"
)

// backends runs a test against both storage backends, since they must
// behave identically from the ObjectStore interface's perspective.
func backends(t *testing.T) map[string]Backend {
	t.Helper()
	fs, err := newFSBackend(t.TempDir())
	if err != nil {
		t.Fatalf("newFSBackend: %v", err)
	}
	t.Cleanup(func() { fs.Close() })

	bolt, err := newBoltBackend(t.TempDir())
	if err != nil {
		t.Fatalf("newBoltBackend: %v", err)
	}
	t.Cleanup(func() { bolt.Close() })

	return map[string]Backend{"fs": fs, "bolt": bolt}
}

func TestBackendPutGetDedup(t *testing.T) {
	for name, backend := range backends(t) {
		t.Run(name, func(t *testing.T) {
			data := []byte(`{"_id":{"$oid":"000000000000000000000001"},"name":"a"}`)

			hash1, isNew1, err := putOne(backend, data)
			if err != nil {
				t.Fatalf("putOne: %v", err)
			}
			if !isNew1 {
				t.Fatalf("first put should be new")
			}

			hash2, isNew2, err := putOne(backend, data)
			if err != nil {
				t.Fatalf("second putOne: %v", err)
			}
			if isNew2 {
				t.Fatalf("identical content should dedup, got isNew=true")
			}
			if hash1 != hash2 {
				t.Fatalf("identical content produced different hashes: %s vs %s", hash1, hash2)
			}

			got, err := backend.Get(hash1)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if !bytes.Equal(got, data) {
				t.Fatalf("Get returned different bytes: got %q want %q", got, data)
			}

			if !backend.Exists(hash1) {
				t.Fatalf("Exists should be true after Put")
			}

			hashes, err := backend.AllHashes()
			if err != nil {
				t.Fatalf("AllHashes: %v", err)
			}
			if len(hashes) != 1 || hashes[0] != hash1 {
				t.Fatalf("AllHashes = %v, want [%s]", hashes, hash1)
			}

			if err := backend.Delete(hash1); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if backend.Exists(hash1) {
				t.Fatalf("Exists should be false after Delete")
			}
		})
	}
}

func TestBackendDifferentContentDifferentHash(t *testing.T) {
	for name, backend := range backends(t) {
		t.Run(name, func(t *testing.T) {
			h1, _, err := putOne(backend, []byte("a"))
			if err != nil {
				t.Fatal(err)
			}
			h2, _, err := putOne(backend, []byte("b"))
			if err != nil {
				t.Fatal(err)
			}
			if h1 == h2 {
				t.Fatalf("different content produced the same hash")
			}
		})
	}
}

func TestBackendPutManyBatch(t *testing.T) {
	for name, backend := range backends(t) {
		t.Run(name, func(t *testing.T) {
			codec, err := newDocCodec()
			if err != nil {
				t.Fatal(err)
			}
			defer codec.Close()

			items := []EncodedObject{
				encodeDocument(codec, []byte("doc-1")),
				encodeDocument(codec, []byte("doc-2")),
				encodeDocument(codec, []byte("doc-1")), // duplicate within the same batch
			}

			n, err := backend.PutMany(items)
			if err != nil {
				t.Fatalf("PutMany: %v", err)
			}
			if n != 2 {
				t.Errorf("PutMany newCount = %d, want 2", n)
			}

			hashes, err := backend.AllHashes()
			if err != nil {
				t.Fatal(err)
			}
			if len(hashes) != 2 {
				t.Errorf("AllHashes = %v, want 2 entries", hashes)
			}
		})
	}
}

func TestBackendDocRefsRoundTrip(t *testing.T) {
	for name, backend := range backends(t) {
		t.Run(name, func(t *testing.T) {
			refs := []DocRef{
				{ID: "a", Hash: "h-a"},
				{ID: "b", Hash: "h-b"},
				{ID: "c", Hash: "h-c"},
			}
			if err := backend.WriteDocRefs("manifest-1", "widgets", newSliceDocRefIterator(refs)); err != nil {
				t.Fatalf("WriteDocRefs: %v", err)
			}

			it, err := backend.IterDocRefs("manifest-1", "widgets")
			if err != nil {
				t.Fatalf("IterDocRefs: %v", err)
			}
			got, err := drainDocRefIterator(it)
			if err != nil {
				t.Fatalf("draining iterator: %v", err)
			}

			sort.Slice(got, func(i, j int) bool { return got[i].ID < got[j].ID })
			if len(got) != len(refs) {
				t.Fatalf("got %d refs, want %d", len(got), len(refs))
			}
			for i, ref := range refs {
				if got[i] != ref {
					t.Errorf("ref[%d] = %+v, want %+v", i, got[i], ref)
				}
			}

			if err := backend.DeleteDocRefs("manifest-1"); err != nil {
				t.Fatalf("DeleteDocRefs: %v", err)
			}
			it2, err := backend.IterDocRefs("manifest-1", "widgets")
			if err != nil {
				t.Fatalf("IterDocRefs after delete: %v", err)
			}
			remaining, err := drainDocRefIterator(it2)
			if err != nil {
				t.Fatal(err)
			}
			if len(remaining) != 0 {
				t.Errorf("expected no doc refs after DeleteDocRefs, got %v", remaining)
			}
		})
	}
}

func TestBackendDocRefsSpanMultipleChunks(t *testing.T) {
	for name, backend := range backends(t) {
		t.Run(name, func(t *testing.T) {
			n := docRefChunkSize*2 + 7 // spans 3 chunks for the bolt backend
			refs := make([]DocRef, n)
			for i := range refs {
				id := fmt.Sprintf("id%06d", i)
				refs[i] = DocRef{ID: id, Hash: id}
			}
			if err := backend.WriteDocRefs("manifest-2", "big", newSliceDocRefIterator(refs)); err != nil {
				t.Fatalf("WriteDocRefs: %v", err)
			}

			it, err := backend.IterDocRefs("manifest-2", "big")
			if err != nil {
				t.Fatalf("IterDocRefs: %v", err)
			}
			got, err := drainDocRefIterator(it)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != n {
				t.Fatalf("got %d refs, want %d", len(got), n)
			}
		})
	}
}
