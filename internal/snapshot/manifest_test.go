package snapshot

import "testing"

func TestScopeIndexFindExact(t *testing.T) {
	idx := &scopeIndex{Snapshots: []Summary{
		{ID: "abc123", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "def456", CreatedAt: "2026-01-02T00:00:00Z"},
	}}

	s, ok := idx.Find("abc123")
	if !ok || s.ID != "abc123" {
		t.Fatalf("Find(abc123) = %v, %v", s, ok)
	}
}

func TestScopeIndexFindUniquePrefix(t *testing.T) {
	idx := &scopeIndex{Snapshots: []Summary{
		{ID: "abc123", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "def456", CreatedAt: "2026-01-02T00:00:00Z"},
	}}

	s, ok := idx.Find("abc")
	if !ok || s.ID != "abc123" {
		t.Fatalf("Find(abc) = %v, %v", s, ok)
	}
}

func TestScopeIndexFindAmbiguousPrefix(t *testing.T) {
	idx := &scopeIndex{Snapshots: []Summary{
		{ID: "abc111", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "abc222", CreatedAt: "2026-01-02T00:00:00Z"},
	}}

	_, ok := idx.Find("abc")
	if ok {
		t.Fatalf("Find(abc) should be ambiguous and fail")
	}
}

func TestScopeIndexLatest(t *testing.T) {
	idx := &scopeIndex{Snapshots: []Summary{
		{ID: "s1", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "s3", CreatedAt: "2026-01-03T00:00:00Z"},
		{ID: "s2", CreatedAt: "2026-01-02T00:00:00Z"},
	}}

	latest, ok := idx.Latest()
	if !ok || latest.ID != "s3" {
		t.Fatalf("Latest() = %v, %v, want s3", latest, ok)
	}
}

func TestScopeIndexSorted(t *testing.T) {
	idx := &scopeIndex{Snapshots: []Summary{
		{ID: "s3", CreatedAt: "2026-01-03T00:00:00Z"},
		{ID: "s1", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "s2", CreatedAt: "2026-01-02T00:00:00Z"},
	}}

	sorted := idx.Sorted()
	if len(sorted) != 3 || sorted[0].ID != "s1" || sorted[1].ID != "s2" || sorted[2].ID != "s3" {
		t.Fatalf("Sorted() = %v, want [s1 s2 s3]", sorted)
	}
}

func TestManifestDocCount(t *testing.T) {
	m := &Manifest{Collections: map[string]CollectionManifest{
		"a": {DocCount: 2},
		"b": {DocCount: 1},
	}}
	if got := m.DocCount(); got != 3 {
		t.Errorf("DocCount() = %d, want 3", got)
	}
}
