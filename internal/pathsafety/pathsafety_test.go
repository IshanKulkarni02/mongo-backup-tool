package pathsafety

import "testing"

func TestSanitizeComponentStripsSeparators(t *testing.T) {
	got := SanitizeComponent("../../../../../../../tmp/pwned")
	if got == "" || containsSeparator(got) {
		t.Fatalf("SanitizeComponent left a path separator in %q", got)
	}
}

func containsSeparator(s string) bool {
	for _, r := range s {
		if r == '/' || r == '\\' {
			return true
		}
	}
	return false
}

func TestSafeJoinRejectsEscape(t *testing.T) {
	if _, err := SafeJoin("/home/user/.dbhelm/backups", "../../../etc/passwd"); err == nil {
		t.Fatal("expected SafeJoin to reject a traversal file name")
	}
}

func TestSafeJoinAllowsNormalName(t *testing.T) {
	path, err := SafeJoin("/home/user/.dbhelm/backups", "local_all_20260101-000000.archive.gz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/home/user/.dbhelm/backups/local_all_20260101-000000.archive.gz"
	if path != want {
		t.Fatalf("got %q, want %q", path, want)
	}
}

func TestSanitizeThenSafeJoinBlocksTraversal(t *testing.T) {
	fileName := SanitizeComponent("../../../../../../../tmp/pwned") + "_all_20260101-000000.archive.gz"
	path, err := SafeJoin("/home/user/.dbhelm/backups", fileName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len("/home/user/.dbhelm/backups/"); len(path) <= got || path[:got] != "/home/user/.dbhelm/backups/" {
		t.Fatalf("resulting path %q escaped the backups directory", path)
	}
}
