package depmanager

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// ollamaInstallScriptURL is the official Ollama install script location. A
// var (not a const) so tests can point it at a fake server.
var ollamaInstallScriptURL = "https://ollama.com/install.sh"

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
// run manually. This function is only ever reached via an explicit
// "Install" button click (never run on startup or silently), so the user's
// intent is already established before it executes.
//
// The script is downloaded to a local file and sanity-checked before being
// executed, rather than piped directly from curl into sh. A straight
// `curl | sh` pipe has two problems: sh starts executing lines as they
// arrive, so a connection drop mid-transfer can execute a truncated script
// instead of failing cleanly; and there's no way to inspect what's about to
// run before it runs. Downloading first (over TLS, via Go's http client so
// certificate verification is enforced) means the full script is in hand
// and passes a basic integrity check — non-empty and shell-shebang-shaped,
// so a corrupted download or an HTML error page served over a broken
// redirect can't silently execute as shell commands — before anything runs.
func autoInstallOllamaLinuxScript(ctx context.Context, onOutput func(string)) error {
	if _, err := exec.LookPath("curl"); err != nil {
		return fmt.Errorf("curl isn't available (the Ollama install script needs it to fetch the binary) — see https://ollama.com/download")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		return fmt.Errorf("sh isn't available — see https://ollama.com/download for manual install instructions")
	}

	scriptPath, err := downloadOllamaInstallScript(ctx)
	if err != nil {
		return err
	}
	defer os.Remove(scriptPath)

	if err := runStreamed(ctx, "sh", []string{scriptPath}, onOutput); err != nil {
		return fmt.Errorf("ollama install script failed: %w", err)
	}
	return nil
}

// maxOllamaInstallScriptSize bounds how much of the response body
// downloadOllamaInstallScript will read, so a misbehaving or compromised
// server can't exhaust memory by streaming an unbounded response.
const maxOllamaInstallScriptSize = 1 << 20 // 1 MiB — the real script is a few KB

// downloadOllamaInstallScript fetches the install script to a private
// temp file and returns its path, or an error if the download failed or
// the content doesn't pass validateInstallScript's sanity check.
func downloadOllamaInstallScript(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ollamaInstallScriptURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build install script request: %w", err)
	}
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download ollama install script: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download ollama install script: server returned %s", resp.Status)
	}

	data := make([]byte, maxOllamaInstallScriptSize+1)
	n, err := io.ReadFull(resp.Body, data)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", fmt.Errorf("failed to download ollama install script: %w", err)
	}
	data = data[:n]
	if n > maxOllamaInstallScriptSize {
		return "", fmt.Errorf("ollama install script exceeded the expected size limit — aborting rather than executing an unexpectedly large payload")
	}

	if err := validateInstallScript(data); err != nil {
		return "", err
	}

	f, err := os.CreateTemp("", "ollama-install-*.sh")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file for install script: %w", err)
	}
	defer f.Close()
	if err := f.Chmod(0o700); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("failed to set permissions on install script temp file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("failed to write install script temp file: %w", err)
	}
	return f.Name(), nil
}

// validateInstallScript is a minimal integrity check on downloaded script
// content: it must be non-empty and start with a shell shebang. This isn't
// a substitute for a published checksum (Ollama doesn't publish one for a
// script that changes over time), but it does catch the common failure
// modes of a broken download — an empty body, an HTML error page from a
// misconfigured redirect, or truncated content — before any of it is
// handed to sh for execution.
func validateInstallScript(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return fmt.Errorf("downloaded ollama install script was empty — aborting rather than executing it")
	}
	if !bytes.HasPrefix(trimmed, []byte("#!")) {
		return fmt.Errorf("downloaded ollama install script failed a basic integrity check (doesn't start with a shebang) — aborting rather than executing unexpected content")
	}
	return nil
}
