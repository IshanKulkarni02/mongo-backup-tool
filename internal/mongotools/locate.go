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
	"strings"
)

// Find locates a Database Tools binary (e.g. "mongodump") by, in order:
// an override env var (MONGOBAK_MONGODUMP_PATH), the system PATH, then a
// set of common install locations per OS.
func Find(base string) (string, error) {
	envVar := "MONGOBAK_" + strings.ToUpper(base) + "_PATH"
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
			"Run `mongobak doctor` for OS-specific install instructions.",
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
			for _, e := range entries {
				if e.IsDir() {
					dirs = append(dirs, filepath.Join(base, e.Name(), "bin"))
				}
			}
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
