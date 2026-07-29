package depmanager

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"time"
)

// OllamaHost is the default local Ollama REST API endpoint. A var (not a
// const) so tests can point it at a fake server.
var OllamaHost = "http://localhost:11434"

// OllamaStatus is Ollama's detection result — a separate, HTTP-based check
// from Required's binary-on-PATH checks above, since Ollama runs as a
// background service that's pinged rather than a CLI tool invoked per
// command. Kept out of Required/Check() so the existing MongoDB Tools
// dependency modal is unaffected; AI settings UI calls this directly.
type OllamaStatus struct {
	Installed bool // the ollama binary is on PATH
	Running   bool // the local API answered
}

// CheckOllama pings the local Ollama API and, if that doesn't answer,
// falls back to checking whether the binary is merely installed but not
// running (e.g. the user hasn't launched the app yet).
func CheckOllama(ctx context.Context) OllamaStatus {
	client := http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, OllamaHost+"/api/version", nil)
	if err == nil {
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return OllamaStatus{Installed: true, Running: true}
			}
		}
	}
	_, lookErr := exec.LookPath("ollama")
	return OllamaStatus{Installed: lookErr == nil, Running: false}
}

// AutoInstallOllama installs the ollama binary via the OS's package
// manager, mirroring AutoInstall's brew/winget dispatch for the MongoDB
// tools. Always an explicit, user-initiated action — never run silently.
func AutoInstallOllama(ctx context.Context, onOutput func(line string)) error {
	switch runtime.GOOS {
	case "darwin":
		return autoInstallOllamaBrew(ctx, onOutput)
	case "windows":
		return autoInstallOllamaWinget(ctx, onOutput)
	case "linux":
		return autoInstallOllamaLinuxScript(ctx, onOutput)
	default:
		return fmt.Errorf("automatic install isn't supported on %s — see https://ollama.com/download", runtime.GOOS)
	}
}

func autoInstallOllamaBrew(ctx context.Context, onOutput func(string)) error {
	brew, err := exec.LookPath("brew")
	if err != nil {
		return fmt.Errorf("homebrew isn't installed (brew not found on PATH) — see https://ollama.com/download")
	}
	if err := runStreamed(ctx, brew, []string{"install", "ollama"}, onOutput); err != nil {
		return fmt.Errorf("brew install ollama failed: %w", err)
	}
	return nil
}

func autoInstallOllamaWinget(ctx context.Context, onOutput func(string)) error {
	winget, err := exec.LookPath("winget")
	if err != nil {
		return fmt.Errorf("winget isn't available on this system — see https://ollama.com/download")
	}
	args := []string{"install", "--id", "Ollama.Ollama", "-e", "--accept-package-agreements", "--accept-source-agreements"}
	if err := runStreamed(ctx, winget, args, onOutput); err != nil {
		return fmt.Errorf("winget install failed: %w", err)
	}
	return nil
}

// autoInstallOllamaLinuxScript runs Ollama's own official install script —
// the same command https://ollama.com/download itself instructs users to
// run manually. Piping a remote script into a shell is normally something
// to avoid automating, but this function is only ever reached via an
// explicit "Install" button click (never run on startup or silently), so
// the user's intent is already established before it executes.
func autoInstallOllamaLinuxScript(ctx context.Context, onOutput func(string)) error {
	if _, err := exec.LookPath("curl"); err != nil {
		return fmt.Errorf("curl isn't available — see https://ollama.com/download for manual install instructions")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		return fmt.Errorf("sh isn't available — see https://ollama.com/download for manual install instructions")
	}
	cmd := "curl -fsSL https://ollama.com/install.sh | sh"
	if err := runStreamed(ctx, "sh", []string{"-c", cmd}, onOutput); err != nil {
		return fmt.Errorf("ollama install script failed: %w", err)
	}
	return nil
}
