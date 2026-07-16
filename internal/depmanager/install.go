package depmanager

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
)

// AutoInstall attempts to install the MongoDB Database Tools automatically
// using the current OS's package manager. onOutput (may be nil) receives
// each line of command output as it runs, for a live progress view. This is
// always an explicit, user-initiated action (never run silently) — see
// `mongobak doctor install` and the TUI's dependency screen.
func AutoInstall(ctx context.Context, onOutput func(line string)) error {
	switch runtime.GOOS {
	case "darwin":
		return autoInstallBrew(ctx, onOutput)
	case "windows":
		return autoInstallWinget(ctx, onOutput)
	default:
		return mongoDBToolsErr("automatic install isn't supported on %s (too much variance across distros/package managers to do safely)", runtime.GOOS)
	}
}

// autoInstallBrew taps and installs the official mongodb/brew formula. A
// freshly-tapped formula is "untrusted" by Homebrew until explicitly
// trusted, which is why the trust step is included — this mirrors the exact
// sequence needed in practice, not just `brew install`.
func autoInstallBrew(ctx context.Context, onOutput func(string)) error {
	brew, err := exec.LookPath("brew")
	if err != nil {
		return mongoDBToolsErr("homebrew isn't installed (brew not found on PATH)")
	}

	steps := [][]string{
		{brew, "tap", "mongodb/brew"},
		{brew, "trust", "mongodb/brew"},
		{brew, "install", "mongodb-database-tools"},
	}
	for _, args := range steps {
		if err := runStreamed(ctx, args[0], args[1:], onOutput); err != nil {
			// `brew trust` errors if the tap is already trusted or the
			// subcommand doesn't apply to this brew version — not fatal,
			// the subsequent install step is the real signal.
			if args[1] == "trust" {
				continue
			}
			return mongoDBToolsErr("brew %s failed: %w", args[1], err)
		}
	}
	return nil
}

// autoInstallWinget is best-effort: the exact winget package ID MongoDB
// publishes the Database Tools under hasn't been verified against a live
// Windows machine in this codebase's development. If the ID is wrong,
// winget fails cleanly (reports no matching package, installs nothing) and
// the caller should fall back to ManualInstructions.
func autoInstallWinget(ctx context.Context, onOutput func(string)) error {
	winget, err := exec.LookPath("winget")
	if err != nil {
		return mongoDBToolsErr("winget isn't available on this system")
	}
	args := []string{"install", "--id", "MongoDB.DatabaseTools", "-e", "--accept-package-agreements", "--accept-source-agreements"}
	if err := runStreamed(ctx, winget, args, onOutput); err != nil {
		return mongoDBToolsErr("winget install failed: %w", err)
	}
	return nil
}

// runStreamed runs a command, sending each line of combined stdout/stderr
// to onOutput as it's produced.
func runStreamed(ctx context.Context, name string, args []string, onOutput func(string)) error {
	cmd := exec.CommandContext(ctx, name, args...)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			if onOutput != nil {
				onOutput(scanner.Text())
			}
		}
	}()

	if err := cmd.Start(); err != nil {
		pw.Close()
		<-done
		return err
	}
	err := cmd.Wait()
	pw.Close()
	<-done
	if err != nil {
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	return nil
}
