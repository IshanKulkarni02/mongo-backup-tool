package mongotools

import (
	"path/filepath"
	"testing"
)

// TestCompareVersions is the regression test for #79: a plain
// lexicographic comparison of version directory names (e.g. via
// sort.Strings or os.ReadDir's default order) gets some version pairs
// backwards — "100.1.0" sorts before "100.10.0" as a string even though
// 100.10.0 is the newer release. compareVersions must compare each
// dot-separated component numerically instead.
func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int // sign only: >0, 0, or <0
	}{
		{"100.10.0", "100.9.0", 1},   // the corrected repro case from the issue discussion
		{"100.1.0", "100.10.0", -1},  // the other direction of the same pairing
		{"100.9.0", "100.10.0", -1},
		{"6.0.1", "6.0.10", -1},
		{"6.0.10", "6.0.1", 1},
		{"100.10.0", "100.10.0", 0},
		{"2.0.0", "1.99.99", 1},
	}
	for _, c := range cases {
		got := compareVersions(c.a, c.b)
		switch {
		case c.want > 0 && got <= 0:
			t.Errorf("compareVersions(%q, %q) = %d, want > 0", c.a, c.b, got)
		case c.want < 0 && got >= 0:
			t.Errorf("compareVersions(%q, %q) = %d, want < 0", c.a, c.b, got)
		case c.want == 0 && got != 0:
			t.Errorf("compareVersions(%q, %q) = %d, want 0", c.a, c.b, got)
		}
	}
}

// TestVersionBinDirsNewestFirstOrdersNumerically confirms the actual
// observable output of the fix: given a set of version directory names
// in the order os.ReadDir would return them (lexicographic), the
// resulting bin-dir list must start with the newest version — which is
// what determines which installed Database Tools version Find picks,
// since its caller returns the first existing candidate among
// fallbackDirs' results.
func TestVersionBinDirsNewestFirstOrdersNumerically(t *testing.T) {
	base := filepath.Join("C:", "Program Files", "MongoDB", "Tools")
	// os.ReadDir's lexicographic order for these names.
	lexicographic := []string{"100.1.0", "100.10.0", "100.9.0"}

	got := versionBinDirsNewestFirst(base, lexicographic)

	want := []string{
		filepath.Join(base, "100.10.0", "bin"),
		filepath.Join(base, "100.9.0", "bin"),
		filepath.Join(base, "100.1.0", "bin"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %d dirs, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dir order = %v, want %v (newest version first)", got, want)
		}
	}
}
