package snapshot

// docRefChunkSize bounds how many DocRefs are held in memory at once while
// writing or iterating a collection's document list — the unit that keeps
// diffing large collections from loading the whole list into RAM.
const docRefChunkSize = 5000

// docRefIterator yields a collection's DocRefs in ascending ID order (the
// order they were written in) without requiring the whole list in memory at
// once. Compare() (diff.go) merge-joins two of these to compute a diff in
// bounded memory regardless of collection size.
type docRefIterator interface {
	// Next returns the next DocRef; ok is false (err nil) at the end.
	Next() (ref DocRef, ok bool, err error)
	Close() error
}

// sliceDocRefIterator adapts an in-memory slice to docRefIterator, for
// small/ad-hoc cases (tests, ScanLive results that are never persisted).
type sliceDocRefIterator struct {
	refs []DocRef
	pos  int
}

func newSliceDocRefIterator(refs []DocRef) *sliceDocRefIterator {
	return &sliceDocRefIterator{refs: refs}
}

func (it *sliceDocRefIterator) Next() (DocRef, bool, error) {
	if it.pos >= len(it.refs) {
		return DocRef{}, false, nil
	}
	ref := it.refs[it.pos]
	it.pos++
	return ref, true, nil
}

func (it *sliceDocRefIterator) Close() error { return nil }

// drainDocRefIterator reads every remaining ref into a slice. Only used
// where the caller genuinely needs the full list (e.g. GC's reference
// sweep, which must know every hash still referenced) — never during diff.
func drainDocRefIterator(it docRefIterator) ([]DocRef, error) {
	defer it.Close()
	var out []DocRef
	for {
		ref, ok, err := it.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return out, nil
		}
		out = append(out, ref)
	}
}
