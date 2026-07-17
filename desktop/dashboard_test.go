package main

import "testing"

func withTempConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv("MONGOBAK_CONFIG_DIR", t.TempDir())
}

func TestSaveAndListQuery(t *testing.T) {
	withTempConfigDir(t)
	a := &App{}

	id, err := a.SaveQuery("", "Active users", "local", "app", "SELECT * FROM users WHERE active")
	if err != nil {
		t.Fatalf("SaveQuery: %v", err)
	}
	if id == "" {
		t.Fatal("expected a generated ID")
	}

	queries, err := a.ListSavedQueries()
	if err != nil {
		t.Fatalf("ListSavedQueries: %v", err)
	}
	if len(queries) != 1 || queries[0].Name != "Active users" {
		t.Fatalf("expected 1 saved query, got %+v", queries)
	}
}

func TestSaveQueryRequiresNameAndText(t *testing.T) {
	withTempConfigDir(t)
	a := &App{}
	if _, err := a.SaveQuery("", "", "local", "app", "SELECT 1"); err == nil {
		t.Fatal("expected an error for a missing name")
	}
	if _, err := a.SaveQuery("", "name", "local", "app", ""); err == nil {
		t.Fatal("expected an error for missing SQL text")
	}
}

func TestDeleteSavedQueryPrunesWidgets(t *testing.T) {
	withTempConfigDir(t)
	a := &App{}

	qid, err := a.SaveQuery("", "q", "local", "app", "SELECT 1")
	if err != nil {
		t.Fatalf("SaveQuery: %v", err)
	}
	wid, err := a.SaveWidget("", "chart", qid, "bar", "x", []string{"y"})
	if err != nil {
		t.Fatalf("SaveWidget: %v", err)
	}

	if err := a.DeleteSavedQuery(qid); err != nil {
		t.Fatalf("DeleteSavedQuery: %v", err)
	}

	widgets, err := a.ListWidgets()
	if err != nil {
		t.Fatalf("ListWidgets: %v", err)
	}
	for _, w := range widgets {
		if w.ID == wid {
			t.Fatal("expected widget depending on deleted query to be pruned")
		}
	}
}

func TestSaveWidgetRequiresExistingQuery(t *testing.T) {
	withTempConfigDir(t)
	a := &App{}
	if _, err := a.SaveWidget("", "chart", "does-not-exist", "bar", "x", []string{"y"}); err == nil {
		t.Fatal("expected an error when the referenced query doesn't exist")
	}
}

func TestDeleteWidgetMissingReturnsError(t *testing.T) {
	withTempConfigDir(t)
	a := &App{}
	if err := a.DeleteWidget("nope"); err == nil {
		t.Fatal("expected an error deleting a nonexistent widget")
	}
}
