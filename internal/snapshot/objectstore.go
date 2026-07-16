package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// EncodedObject is a document's content, already hashed and compressed,
// ready to hand to an ObjectStore. Computing this is pure CPU work (no I/O),
// which is what lets the snapshot pipeline (pipeline.go) do it in parallel
// across a worker pool ahead of a single serialized writer.
type EncodedObject struct {
	Hash       string
	Compressed []byte
}

// ObjectStore is content-addressed storage for canonical document bytes,
// scoped to one connection+database. Identical documents (byte-identical
// canonical encoding) are stored exactly once, so snapshots that mostly
// repeat prior content are cheap. Two implementations exist:
// objectStoreBolt (default: a single embedded KV file, safe against inode
// exhaustion at large scale) and objectStoreFS (one file per hash, used only
// for the Git/Git-LFS remote-sync backend, since LFS needs individually
// addressable blob files).
type ObjectStore interface {
	// Get retrieves and decompresses a stored object by hash.
	Get(hash string) ([]byte, error)
	// Exists reports whether an object with the given hash is already stored.
	Exists(hash string) bool
	// PutMany stores a batch of already-encoded objects in one operation,
	// deduping against existing content, and returns how many were new.
	PutMany(items []EncodedObject) (newCount int, err error)
	// Delete removes a stored object. Used by GC.
	Delete(hash string) error
	// AllHashes returns every hash currently stored. Used by GC's sweep.
	AllHashes() ([]string, error)
	// Close releases any resources (file handles, etc.) held by the store.
	Close() error
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// docCodec holds one reusable zstd encoder + decoder. Creating a zstd
// encoder/decoder has real setup cost, so the snapshot pipeline gives each
// worker goroutine its own long-lived codec rather than constructing one per
// document. Not safe for concurrent use — one per goroutine.
type docCodec struct {
	enc *zstd.Encoder
	dec *zstd.Decoder
}

func newDocCodec() (*docCodec, error) {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, err
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		enc.Close()
		return nil, err
	}
	return &docCodec{enc: enc, dec: dec}, nil
}

func (c *docCodec) Close() {
	c.enc.Close()
	c.dec.Close()
}

func (c *docCodec) compress(data []byte) []byte {
	return c.enc.EncodeAll(data, nil)
}

func (c *docCodec) decompress(data []byte) ([]byte, error) {
	return c.dec.DecodeAll(data, nil)
}

// encodeDocument hashes a document's canonical bytes (the hash is over the
// *uncompressed* bytes, so identical content always hashes the same
// regardless of compression settings) and zstd-compresses it, without
// touching any store. This is the pure-CPU step the worker pool parallelizes.
func encodeDocument(codec *docCodec, canonical []byte) EncodedObject {
	return EncodedObject{Hash: hashOf(canonical), Compressed: codec.compress(canonical)}
}

// sharedDecoder is a mutex-guarded single decoder used by store
// implementations for one-off Get() calls, which aren't the hot,
// heavily-parallelized path (snapshot creation is). Avoids each store
// needing its own worker pool just to decompress on read.
type sharedDecoder struct {
	mu  sync.Mutex
	dec *zstd.Decoder
}

func newSharedDecoder() (*sharedDecoder, error) {
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	return &sharedDecoder{dec: dec}, nil
}

func (s *sharedDecoder) decompress(data []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dec.DecodeAll(data, nil)
}

func (s *sharedDecoder) Close() {
	s.dec.Close()
}

// putOne is a convenience wrapper for storing a single object outside the
// pipeline (tests, small ad-hoc scans, GC-triggered safety snapshots of tiny
// scopes) where spinning up a worker pool isn't worth it.
func putOne(store ObjectStore, canonical []byte) (hash string, isNew bool, err error) {
	codec, err := newDocCodec()
	if err != nil {
		return "", false, err
	}
	defer codec.Close()

	obj := encodeDocument(codec, canonical)
	n, err := store.PutMany([]EncodedObject{obj})
	if err != nil {
		return "", false, err
	}
	return obj.Hash, n == 1, nil
}
