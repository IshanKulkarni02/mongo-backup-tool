package depmanager

import "runtime"

// ManualInstructions returns copy-pasteable install instructions for the
// current OS, for the "install it yourself" path.
func ManualInstructions() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"brew tap mongodb/brew && brew install mongodb-database-tools",
			"(if that fails to build from source, download a prebuilt binary from",
			" https://www.mongodb.com/try/download/database-tools and put mongodump/",
			" mongorestore on your PATH, e.g. in ~/.local/bin)",
		}
	case "windows":
		return []string{
			"winget install MongoDB.DatabaseTools",
			"Or see: https://www.mongodb.com/docs/database-tools/installation/installation-windows/",
			"Or download directly: https://www.mongodb.com/try/download/database-tools",
		}
	default: // linux and other unix
		return []string{
			"See: https://www.mongodb.com/docs/database-tools/installation/installation-linux/",
			"Or download directly: https://www.mongodb.com/try/download/database-tools",
		}
	}
}

// AutoInstallAvailable reports whether AutoInstall has a real implementation
// for the current OS. Linux distros vary too much (apt vs. yum vs. others,
// and MongoDB's tools aren't in default repos) to safely automate without
// per-distro testing, so it's manual-only there.
func AutoInstallAvailable() bool {
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	default:
		return false
	}
}
