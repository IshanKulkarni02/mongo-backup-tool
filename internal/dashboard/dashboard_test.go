package dashboard

import "testing"

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()

	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load (empty): %v", err)
	}
	if len(s.SavedQueries) != 0 || len(s.Widgets) != 0 {
		t.Fatalf("expected empty store, got %+v", s)
	}

	s.UpsertQuery(SavedQuery{ID: "q1", Name: "Active users", Connection: "local", Database: "app", SQLText: "SELECT * FROM users WHERE active"})
	s.UpsertWidget(Widget{ID: "w1", Title: "Active users chart", QueryID: "q1", ChartType: ChartBar, XColumn: "id", YColumns: []string{"count"}})
	if err := Save(dir, s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load (populated): %v", err)
	}
	q, ok := loaded.FindQuery("q1")
	if !ok || q.Name != "Active users" {
		t.Fatalf("expected saved query to round-trip, got %+v ok=%v", q, ok)
	}
	if len(loaded.Widgets) != 1 || loaded.Widgets[0].Title != "Active users chart" {
		t.Fatalf("expected widget to round-trip, got %+v", loaded.Widgets)
	}
}

func TestUpsertQueryReplacesExisting(t *testing.T) {
	s := &Store{}
	s.UpsertQuery(SavedQuery{ID: "q1", Name: "v1"})
	s.UpsertQuery(SavedQuery{ID: "q1", Name: "v2"})
	if len(s.SavedQueries) != 1 {
		t.Fatalf("expected upsert to replace, not append, got %d entries", len(s.SavedQueries))
	}
	if s.SavedQueries[0].Name != "v2" {
		t.Fatalf("expected replaced value, got %q", s.SavedQueries[0].Name)
	}
}

func TestRemoveQueryPrunesDependentWidgets(t *testing.T) {
	s := &Store{}
	s.UpsertQuery(SavedQuery{ID: "q1", Name: "a"})
	s.UpsertQuery(SavedQuery{ID: "q2", Name: "b"})
	s.UpsertWidget(Widget{ID: "w1", QueryID: "q1"})
	s.UpsertWidget(Widget{ID: "w2", QueryID: "q2"})

	if !s.RemoveQuery("q1") {
		t.Fatal("expected RemoveQuery to report the query existed")
	}
	if len(s.SavedQueries) != 1 || s.SavedQueries[0].ID != "q2" {
		t.Fatalf("expected only q2 to remain, got %+v", s.SavedQueries)
	}
	if len(s.Widgets) != 1 || s.Widgets[0].ID != "w2" {
		t.Fatalf("expected only w2 to remain (w1 depended on removed q1), got %+v", s.Widgets)
	}
}

func TestRemoveWidget(t *testing.T) {
	s := &Store{}
	s.UpsertWidget(Widget{ID: "w1"})
	if !s.RemoveWidget("w1") {
		t.Fatal("expected RemoveWidget to report the widget existed")
	}
	if s.RemoveWidget("w1") {
		t.Fatal("expected a second RemoveWidget to report false")
	}
}

func TestFindQueryMissing(t *testing.T) {
	s := &Store{}
	if _, ok := s.FindQuery("nope"); ok {
		t.Fatal("expected FindQuery to report false for a missing ID")
	}
}
