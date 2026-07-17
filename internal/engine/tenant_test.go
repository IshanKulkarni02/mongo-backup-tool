package engine

import "testing"

func TestValidSessionVarName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"app.current_tenant", true},
		{"current_tenant", true},
		{"_leading_underscore", true},
		{"a.b.c", true},
		{"", false},
		{"1starts_with_digit", false},
		{"has space", false},
		{"has-dash", false},
		{"semi;colon", false},
		{"quote'here", false},
	}
	for _, c := range cases {
		if got := ValidSessionVarName(c.name); got != c.want {
			t.Errorf("ValidSessionVarName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}
