package snapshot

import (
	"bufio"
	"container/heap"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// extSortRunSize bounds how many DocRefs are buffered in memory at once
// while a collection is being scanned. Once a collection has more DocRefs
// than this, extSortSpill starts spilling sorted runs to disk instead of
// growing the in-memory buffer further — this is what keeps snapshotting a
// multi-million-document collection from holding the whole thing in RAM.
// A var (not const) so tests can shrink it to exercise multi-run spilling
// and the k-way merge without needing tens of thousands of test DocRefs.
var extSortRunSize = 50000

// extSortSpill accumulates DocRefs in arrival order (unsorted) and produces
// them back out fully sorted by ID, in bounded memory, regardless of how
// many were added. Small collections (at or under extSortRunSize entries)
// never touch disk — the buffer is just sorted in place. Larger collections
// spill sorted runs to temp files as the buffer fills, then NewIterator()
// performs a bounded-memory k-way merge across the runs (one buffered
// reader + one in-flight DocRef per run, not the whole collection).
//
// A spill can be iterated multiple times via NewIterator() — the temp run
// files persist until Cleanup() is called — matching the existing
// docRefSource contract, which may be invoked more than once for the same
// collection.
type extSortSpill struct {
	dir      string // temp directory holding this collection's run files
	buf      []DocRef
	runPaths []string
	dirty    bool // true once buf has content not yet reflected in runPaths (spilled or not)
}

func newExtSortSpill() (*extSortSpill, error) {
	dir, err := os.MkdirTemp("", "mongobak-extsort-*")
	if err != nil {
		return nil, err
	}
	return &extSortSpill{dir: dir, buf: make([]DocRef, 0, extSortRunSize)}, nil
}

// Add appends one DocRef, in whatever order the caller produces it (does not
// need to be sorted). Spills the current buffer to a new sorted run file
// once it reaches extSortRunSize.
func (s *extSortSpill) Add(ref DocRef) error {
	s.buf = append(s.buf, ref)
	s.dirty = true
	if len(s.buf) >= extSortRunSize {
		return s.spillBuffer()
	}
	return nil
}

func (s *extSortSpill) spillBuffer() error {
	if len(s.buf) == 0 {
		return nil
	}
	sort.Slice(s.buf, func(i, j int) bool { return s.buf[i].ID < s.buf[j].ID })

	path := filepath.Join(s.dir, itoaPad(len(s.runPaths)))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, ref := range s.buf {
		if err := enc.Encode(ref); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	s.runPaths = append(s.runPaths, path)
	s.buf = s.buf[:0]
	s.dirty = false
	return nil
}

func itoaPad(n int) string {
	const digits = "0123456789"
	b := [10]byte{}
	for i := len(b) - 1; i >= 0; i-- {
		b[i] = digits[n%10]
		n /= 10
	}
	return string(b[:])
}

// NewIterator returns a docRefIterator over every DocRef added so far, in
// ascending ID order. Safe to call more than once (e.g. once per side of a
// diff, or once per page of a paginated read) — each call opens fresh file
// handles rather than consuming shared state. If nothing was ever spilled to
// disk (small collection), it iterates the in-memory buffer directly with no
// file I/O at all.
func (s *extSortSpill) NewIterator() (docRefIterator, error) {
	if len(s.runPaths) == 0 {
		// Never spilled: sort the buffer once (idempotent — already sorted
		// after the first call) and hand back a plain slice iterator.
		if s.dirty {
			sort.Slice(s.buf, func(i, j int) bool { return s.buf[i].ID < s.buf[j].ID })
			s.dirty = false
		}
		out := make([]DocRef, len(s.buf))
		copy(out, s.buf)
		return newSliceDocRefIterator(out), nil
	}

	// A dirty in-memory tail alongside on-disk runs: flush it so every run
	// is uniformly on disk for the merge.
	if s.dirty {
		if err := s.spillBuffer(); err != nil {
			return nil, err
		}
	}

	runs := make([]*runReader, 0, len(s.runPaths))
	for _, path := range s.runPaths {
		f, err := os.Open(path)
		if err != nil {
			for _, r := range runs {
				r.f.Close()
			}
			return nil, err
		}
		r := &runReader{f: f, scanner: bufio.NewScanner(f)}
		if err := r.advance(); err != nil {
			for _, rr := range runs {
				rr.f.Close()
			}
			f.Close()
			return nil, err
		}
		if r.ok {
			runs = append(runs, r)
		} else {
			f.Close()
		}
	}

	mh := &mergeHeap{runs: runs}
	heap.Init(mh)
	return &extSortMergeIterator{mh: mh}, nil
}

// Cleanup removes every temp run file this spill created. Safe to call more
// than once; safe to call even if NewIterator() was never invoked.
func (s *extSortSpill) Cleanup() error {
	return os.RemoveAll(s.dir)
}

// runReader wraps one sorted run file, holding its current (already-read)
// DocRef so mergeHeap can compare across runs without re-reading.
type runReader struct {
	f       *os.File
	scanner *bufio.Scanner
	cur     DocRef
	ok      bool
}

func (r *runReader) advance() error {
	if !r.scanner.Scan() {
		r.ok = false
		return r.scanner.Err()
	}
	var ref DocRef
	if err := json.Unmarshal(r.scanner.Bytes(), &ref); err != nil {
		return err
	}
	r.cur = ref
	r.ok = true
	return nil
}

// mergeHeap is a min-heap of runReaders ordered by each run's current head
// DocRef ID, implementing the k-way merge step of an external merge sort.
type mergeHeap struct {
	runs []*runReader
}

func (h mergeHeap) Len() int            { return len(h.runs) }
func (h mergeHeap) Less(i, j int) bool  { return h.runs[i].cur.ID < h.runs[j].cur.ID }
func (h mergeHeap) Swap(i, j int)       { h.runs[i], h.runs[j] = h.runs[j], h.runs[i] }
func (h *mergeHeap) Push(x interface{}) { h.runs = append(h.runs, x.(*runReader)) }
func (h *mergeHeap) Pop() interface{} {
	old := h.runs
	n := len(old)
	item := old[n-1]
	h.runs = old[:n-1]
	return item
}

// extSortMergeIterator is a docRefIterator over the k-way merge of every run
// in a mergeHeap. Memory use is O(number of runs), never O(collection size).
type extSortMergeIterator struct {
	mh *mergeHeap
}

func (it *extSortMergeIterator) Next() (DocRef, bool, error) {
	if it.mh.Len() == 0 {
		return DocRef{}, false, nil
	}
	top := it.mh.runs[0]
	ref := top.cur
	if err := top.advance(); err != nil {
		return DocRef{}, false, err
	}
	if top.ok {
		heap.Fix(it.mh, 0)
	} else {
		heap.Pop(it.mh)
		top.f.Close()
	}
	return ref, true, nil
}

func (it *extSortMergeIterator) Close() error {
	for _, r := range it.mh.runs {
		r.f.Close()
	}
	it.mh.runs = nil
	return nil
}
