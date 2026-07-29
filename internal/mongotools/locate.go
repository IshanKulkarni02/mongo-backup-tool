// Package mongotools wraps the MongoDB Database Tools (mongodump,
// mongorestore) and provides a lightweight connectivity check via the
// official Go driver.
package mongotools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Find locates a Database Tools binary (e.g. "mongodump") by, in order:
// an override env var (DBHELM_MONGODUMP_PATH), the system PATH, then a
// set of common install locations per OS.
func Find(base string) (string, error) {
	envVar := "DBHELM_" + strings.ToUpper(base) + "_PATH"
	if p := os.Getenv(envVar); p != "" {
		if fileExists(p) {
			return p, nil
		}
		return "", fmt.Errorf("%s=%s does not exist", envVar, p)
	}

	name := base
	if runtime.GOOS == "windows" {
		name = base + ".exe"
	}

	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}

	for _, dir := range fallbackDirs() {
		p := filepath.Join(dir, name)
		if fileExists(p) {
			return p, nil
		}
	}

	return "", fmt.Errorf(
		"%s not found on PATH or in common install locations.\n"+
			"Install the MongoDB Database Tools, or point to it directly with %s=/path/to/%s.\n"+
			"Run `dbhelm doctor` for OS-specific install instructions.",
		base, envVar, name,
	)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func fallbackDirs() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		dirs := []string{}
		programFiles := os.Getenv("ProgramFiles")
		if programFiles == "" {
			programFiles = `C:\Program Files`
		}
		// MongoDB installs versioned dirs like "MongoDB\Tools\100.10.0\bin".
		base := filepath.Join(programFiles, "MongoDB", "Tools")
		if entries, err := os.ReadDir(base); err == nil {
			var versions []string
			for _, e := range entries {
				if e.IsDir() {
					versions = append(versions, e.Name())
				}
			}
			dirs = append(dirs, versionBinDirsNewestFirst(base, versions)...)
		}
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			dirs = append(dirs, filepath.Join(local, "Programs", "mongodb-database-tools", "bin"))
		}
		return dirs
	case "darwin":
		return []string{
			"/opt/homebrew/bin",
			"/usr/local/bin",
			filepath.Join(home, ".local", "bin"),
		}
	default: // linux and other unix
		return []string{
			"/usr/local/bin",
			"/usr/bin",
			filepath.Join(home, ".local", "bin"),
		}
	}
}

// versionBinDirsNewestFirst maps version directory names (e.g. "100.9.0")
// under base to their "bin" subdirectories, ordered newest-first. Find's
// caller returns the first existing candidate it finds among fallbackDirs'
// results, so the order here directly determines which installed version
// gets picked when more than one is present side by side: os.ReadDir's
// lexicographic order gets this wrong for some version pairs (e.g.
// "100.1.0" sorts before "100.10.0" as a string, even though 100.10.0 is
// newer), silently preferring an older install over a newer one. Pulled
// out of fallbackDirs so the ordering logic can be unit tested directly,
// independent of runtime.GOOS.
func versionBinDirsNewestFirst(base string, versions []string) []string {
	sorted := append([]string(nil), versions...)
	sort.Slice(sorted, func(i, j int) bool {
		return compareVersions(sorted[i], sorted[j]) > 0
	})
	dirs := make([]string, len(sorted))
	for i, v := range sorted {
		dirs[i] = filepath.Join(base, v, "bin")
	}
	return dirs
}

// compareVersions compares two dot-separated numeric version strings
// (e.g. "100.10.0" vs "100.9.0") component by component as integers, and
// reports whether a is newer than, equal to, or older than b (positive,
// zero, or negative, mirroring strings.Compare's convention). A plain
// lexicographic string comparison gets this wrong whenever two version
// segments differ in digit count — "100.10.0" < "100.9.0" as strings,
// even though 10 > 9. A component that isn't purely numeric is treated
// as 0 rather than erroring, since this only ever compares directory
// names already expected to look like versions.
func compareVersions(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}
		if av != bv {
			return av - bv
		}
	}
	return 0
}
