package snapshot

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	objectsBucket = []byte("objects")
	docRefsBucket = []byte("docrefs")
)

// boltBackend is the default storage backend: one embedded KV file holding
// both the content-addressed object store and the chunked doc-ref lists.
type boltBackend struct {
	db  *bolt.DB
	dec *sharedDecoder
}

func newBoltBackend(scope string) (*boltBackend, error) {
	path := filepath.Join(scope, "store.bolt")
	db, err := bolt.Open(path, 0o644, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("opening snapshot store %s: %w", path, err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(objectsBucket); err != nil {
			return err
		}
		_, err := tx.CreateBucketIfNotExists(docRefsBucket)
		return err
	}); err != nil {
		db.Close()
		return nil, err
	}
	dec, err := newSharedDecoder()
	if err != nil {
		db.Close()
		return nil, err
	}
	return &boltBackend{db: db, dec: dec}, nil
}

// --- ObjectStore ---

func (b *boltBackend) Exists(hash string) bool {
	var found bool
	b.db.View(func(tx *bolt.Tx) error {
		found = tx.Bucket(objectsBucket).Get([]byte(hash)) != nil
		return nil
	})
	return found
}

// PutMany writes an entire batch in a single write transaction: many worker
// goroutines encode documents concurrently (pipeline.go), but only this one
// transaction touches the store, so bbolt's single-writer model never
// becomes a per-document bottleneck.
func (b *boltBackend) PutMany(items []EncodedObject) (int, error) {
	newCount := 0
	err := b.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(objectsBucket)
		for _, item := range items {
			key := []byte(item.Hash)
			if bucket.Get(key) != nil {
				continue // dedup
			}
			if err := bucket.Put(key, item.Compressed); err != nil {
				return err
			}
			newCount++
		}
		return nil
	})
	return newCount, err
}

func (b *boltBackend) Get(hash string) ([]byte, error) {
	var compressed []byte
	err := b.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(objectsBucket).Get([]byte(hash))
		if v == nil {
			return fmt.Errorf("object %s not found", hash)
		}
		compressed = append([]byte(nil), v...) // copy: v is only valid within the transaction
		return nil
	})
	if err != nil {
		return nil, err
	}
	data, err := b.dec.decompress(compressed)
	if err != nil {
		return nil, fmt.Errorf("decompressing object %s: %w", hash, err)
	}
	return data, nil
}

func (b *boltBackend) Delete(hash string) error {
	return b.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(objectsBucket).Delete([]byte(hash))
	})
}

func (b *boltBackend) AllHashes() ([]string, error) {
	var hashes []string
	err := b.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(objectsBucket).ForEach(func(k, _ []byte) error {
			hashes = append(hashes, string(k))
			return nil
		})
	})
	return hashes, err
}

func (b *boltBackend) Close() error {
	b.dec.Close()
	return b.db.Close()
}

// --- doc-ref chunked storage ---

func docRefChunkKey(manifestID, collection string, chunk int) []byte {
	return []byte(fmt.Sprintf("%s\x00%s\x00%06d", manifestID, collection, chunk))
}

func docRefCollectionPrefix(manifestID, collection string) []byte {
	return []byte(fmt.Sprintf("%s\x00%s\x00", manifestID, collection))
}

func docRefManifestPrefix(manifestID string) []byte {
	return []byte(manifestID + "\x00")
}

func hasBytePrefix(b, prefix []byte) bool {
	return len(b) >= len(prefix) && string(b[:len(prefix)]) == string(prefix)
}

func (b *boltBackend) WriteDocRefs(manifestID, collection string, sorted []DocRef) error {
	return b.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(docRefsBucket)
		for i := 0; i < len(sorted); i += docRefChunkSize {
			end := i + docRefChunkSize
			if end > len(sorted) {
				end = len(sorted)
			}
			data, err := json.Marshal(sorted[i:end])
			if err != nil {
				return err
			}
			if err := bucket.Put(docRefChunkKey(manifestID, collection, i/docRefChunkSize), data); err != nil {
				return err
			}
		}
		return nil
	})
}

// IterDocRefs holds a read-only transaction open for the iterator's
// lifetime, decoding one chunk (docRefChunkSize entries) at a time as Next()
// advances — memory use stays proportional to chunk size, not collection
// size, regardless of how many documents the collection has. Callers must
// Close() the iterator to release the transaction.
func (b *boltBackend) IterDocRefs(manifestID, collection string) (docRefIterator, error) {
	tx, err := b.db.Begin(false)
	if err != nil {
		return nil, err
	}
	return &boltDocRefIterator{
		tx:     tx,
		cursor: tx.Bucket(docRefsBucket).Cursor(),
		prefix: docRefCollectionPrefix(manifestID, collection),
	}, nil
}

func (b *boltBackend) DeleteDocRefs(manifestID string) error {
	prefix := docRefManifestPrefix(manifestID)
	return b.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(docRefsBucket)
		c := bucket.Cursor()
		var keys [][]byte
		for k, _ := c.Seek(prefix); k != nil && hasBytePrefix(k, prefix); k, _ = c.Next() {
			keys = append(keys, append([]byte(nil), k...))
		}
		for _, k := range keys {
			if err := bucket.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

// boltDocRefIterator streams DocRefs out of a held-open read transaction,
// decoding one chunk (docRefChunkSize entries) at a time.
type boltDocRefIterator struct {
	tx      *bolt.Tx
	cursor  *bolt.Cursor
	prefix  []byte
	started bool
	current []DocRef
	pos     int
}

func (it *boltDocRefIterator) Next() (DocRef, bool, error) {
	for it.pos >= len(it.current) {
		var k, v []byte
		if !it.started {
			k, v = it.cursor.Seek(it.prefix)
			it.started = true
		} else {
			k, v = it.cursor.Next()
		}
		if k == nil || !hasBytePrefix(k, it.prefix) {
			return DocRef{}, false, nil
		}
		var chunk []DocRef
		if err := json.Unmarshal(v, &chunk); err != nil {
			return DocRef{}, false, err
		}
		it.current = chunk
		it.pos = 0
	}
	ref := it.current[it.pos]
	it.pos++
	return ref, true, nil
}

func (it *boltDocRefIterator) Close() error {
	return it.tx.Rollback()
}
