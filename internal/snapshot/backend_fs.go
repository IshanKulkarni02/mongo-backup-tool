package snapshot

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// fsBackend is the one-file-per-hash storage backend, used only for the
// Git/Git-LFS remote-sync backend: LFS tracks individually addressable blob
// files, not one large opaque database file, so this layout — not bbolt —
// is what a git-backed scope uses. Doc-ref lists are stored as one
// newline-delimited JSON file per (snapshot, collection), which streams
// naturally via a line scanner and happens to be reasonably git-diffable.
type fsBackend struct {
	dir string
	dec *sharedDecoder
}

func newFSBackend(scope string) (*fsBackend, error) {
	dec, err := newSharedDecoder()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(objectsDir(scope), 0o755); err != nil {
		return nil, err
	}
	return &fsBackend{dir: scope, dec: dec}, nil
}

func (b *fsBackend) objectPath(hash string) string {
	dir := objectsDir(b.dir)
	if len(hash) < 2 {
		return filepath.Join(dir, hash)
	}
	return filepath.Join(dir, hash[:2], hash+".zst")
}

// --- ObjectStore ---

func (b *fsBackend) Exists(hash string) bool {
	_, err := os.Stat(b.objectPath(hash))
	return err == nil
}

func (b *fsBackend) PutMany(items []EncodedObject) (int, error) {
	newCount := 0
	for _, item := range items {
		path := b.objectPath(item.Hash)
		if _, err := os.Stat(path); err == nil {
			continue // dedup
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return newCount, err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, item.Compressed, 0o644); err != nil {
			os.Remove(tmp)
			return newCount, err
		}
		if err := os.Rename(tmp, path); err != nil {
			os.Remove(tmp)
			return newCount, err
		}
		newCount++
	}
	return newCount, nil
}

func (b *fsBackend) Get(hash string) ([]byte, error) {
	compressed, err := os.ReadFile(b.objectPath(hash))
	if err != nil {
		return nil, fmt.Errorf("reading object %s: %w", hash, err)
	}
	data, err := b.dec.decompress(compressed)
	if err != nil {
		return nil, fmt.Errorf("decompressing object %s: %w", hash, err)
	}
	return data, nil
}

func (b *fsBackend) Delete(hash string) error {
	err := os.Remove(b.objectPath(hash))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (b *fsBackend) AllHashes() ([]string, error) {
	var hashes []string
	err := filepath.WalkDir(objectsDir(b.dir), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := filepath.Base(path)
		const suffix = ".zst"
		if len(name) > len(suffix) && name[len(name)-len(suffix):] == suffix {
			hashes = append(hashes, name[:len(name)-len(suffix)])
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return hashes, err
}

func (b *fsBackend) Close() error {
	b.dec.Close()
	return nil
}

// --- doc-ref storage: one newline-delimited JSON file per snapshot+collection ---

func (b *fsBackend) docRefsDir(manifestID string) string {
	return filepath.Join(manifestsDir(b.dir), manifestID)
}

// docRefsPath derives a filesystem-safe, collision-resistant file name for
// one collection's doc-ref list, the same way scopeDirName does for scope
// directories: sanitizing to [A-Za-z0-9._-] alone is lossy (e.g. Mongo
// collections "a$b" and "a!b" both become "a_b"), so a short hash of the
// *original, unsanitized* collection name is appended — the sanitized
// prefix keeps the file human-readable/git-diffable, the hash suffix
// guarantees two differently-named collections in the same manifest never
// collide regardless of what characters their names contain.
func (b *fsBackend) docRefsPath(manifestID, collection string) string {
	sum := sha256.Sum256([]byte(collection))
	suffix := hex.EncodeToString(sum[:])[:10]
	return filepath.Join(b.docRefsDir(manifestID), fmt.Sprintf("%s__%s.docrefs.jsonl", sanitize(collection), suffix))
}

// legacyDocRefsPath is where docRefsPath used to place a collection's
// doc-ref file, before the collision-resistant hash suffix was added (see
// docRefsPath) — sanitize(collection) alone, with no guard against two
// different names sanitizing to the same string. IterDocRefs falls back
// to this path so doc-ref data written by an older dbhelm build (e.g. an
// existing Git/LFS remote-synced scope) is still readable after
// upgrading, rather than silently appearing empty.
func (b *fsBackend) legacyDocRefsPath(manifestID, collection string) string {
	return filepath.Join(b.docRefsDir(manifestID), sanitize(collection)+".docrefs.jsonl")
}

// WriteDocRefs streams refs to a temp file one entry at a time (never
// holding the whole collection in memory) and renames it into place only
// once every entry is durably written, so a reader never observes a
// partially-written doc-ref file.
func (b *fsBackend) WriteDocRefs(manifestID, collection string, refs docRefIterator) error {
	defer refs.Close()
	if err := os.MkdirAll(b.docRefsDir(manifestID), 0o755); err != nil {
		return err
	}
	path := b.docRefsPath(manifestID, collection)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for {
		ref, ok, err := refs.Next()
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
		if !ok {
			break
		}
		if err := enc.Encode(ref); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// IterDocRefs streams the doc-ref list one line at a time via a buffered
// scanner, so memory use stays proportional to one line, not the whole
// collection.
func (b *fsBackend) IterDocRefs(manifestID, collection string) (docRefIterator, error) {
	f, err := os.Open(b.docRefsPath(manifestID, collection))
	if os.IsNotExist(err) {
		// Fall back to the pre-collision-fix path, for doc-ref data written
		// by an older dbhelm build (see legacyDocRefsPath).
		f, err = os.Open(b.legacyDocRefsPath(manifestID, collection))
	}
	if os.IsNotExist(err) {
		return newSliceDocRefIterator(nil), nil
	}
	if err != nil {
		return nil, err
	}
	return &fsDocRefIterator{f: f, scanner: bufio.NewScanner(f)}, nil
}

func (b *fsBackend) DeleteDocRefs(manifestID string) error {
	err := os.RemoveAll(b.docRefsDir(manifestID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

type fsDocRefIterator struct {
	f       *os.File
	scanner *bufio.Scanner
}

func (it *fsDocRefIterator) Next() (DocRef, bool, error) {
	if !it.scanner.Scan() {
		return DocRef{}, false, it.scanner.Err()
	}
	var ref DocRef
	if err := json.Unmarshal(it.scanner.Bytes(), &ref); err != nil {
		return DocRef{}, false, err
	}
	return ref, true, nil
}

func (it *fsDocRefIterator) Close() error {
	return it.f.Close()
}
