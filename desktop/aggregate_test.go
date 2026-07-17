package main

import "testing"

func TestWritesData(t *testing.T) {
	cases := []struct {
		name     string
		pipeline string
		want     bool
	}{
		{"match only", `[{"$match":{"active":true}}]`, false},
		{"group and sort", `[{"$group":{"_id":"$user"}},{"$sort":{"count":-1}}]`, false},
		{"out stage", `[{"$match":{}},{"$out":"summary"}]`, true},
		{"merge stage", `[{"$merge":{"into":"summary"}}]`, true},
		{"empty pipeline", `[]`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := writesData(c.pipeline); got != c.want {
				t.Errorf("writesData(%q) = %v, want %v", c.pipeline, got, c.want)
			}
		})
	}
}
