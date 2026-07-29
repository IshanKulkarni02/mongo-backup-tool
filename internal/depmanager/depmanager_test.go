package depmanager

import (
	"os/exec"
	"testing"
)

func TestCheckOptionalDetectsGitWhenPresent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH in this environment")
	}
	statuses := CheckOptional()
	var git *Status
	for i := range statuses {
		if statuses[i].Dependency.Name == "git" {
			git = &statuses[i]
		}
	}
	if git == nil {
		t.Fatal("git not present in CheckOptional's results")
	}
	if !git.Installed {
		t.Errorf("git.Installed = false, want true (git is on PATH)")
	}
	if git.Version == "" {
		t.Errorf("git.Version is empty, want a version string")
	}
}

func TestCheckOptionalReportsMissingWithoutError(t *testing.T) {
	statuses := CheckOptional()
	for _, s := range statuses {
		if s.Dependency.Name == "definitely-not-a-real-binary-xyz" {
			t.Fatalf("test setup error: unexpected dependency in Optional")
		}
	}
	// Every entry in Optional should appear in CheckOptional's results,
	// installed or not, without CheckOptional itself ever erroring.
	if len(statuses) != len(Optional) {
		t.Errorf("CheckOptional returned %d statuses, want %d (one per Optional entry)", len(statuses), len(Optional))
	}
}

func TestRequiredAndOptionalDependenciesAreDisjoint(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Required {
		seen[d.Name] = true
	}
	for _, d := range Optional {
		if seen[d.Name] {
			t.Errorf("%s appears in both Required and Optional", d.Name)
		}
	}
}
