package depmanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckOllamaRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"0.1.0"}`))
	}))
	defer srv.Close()

	orig := OllamaHost
	OllamaHost = srv.URL
	defer func() { OllamaHost = orig }()

	status := CheckOllama(context.Background(), "")
	if !status.Running || !status.Installed {
		t.Fatalf("expected Running=true Installed=true, got %+v", status)
	}
}

func TestCheckOllamaNotRunning(t *testing.T) {
	orig := OllamaHost
	OllamaHost = "http://127.0.0.1:1" // nothing listens here
	defer func() { OllamaHost = orig }()

	status := CheckOllama(context.Background(), "")
	if status.Running {
		t.Fatalf("expected Running=false when nothing answers, got %+v", status)
	}
}

// TestCheckOllamaUsesConfiguredHost is the regression test for #35:
// CheckOllama always probed the hardcoded package-level OllamaHost var
// and took no host parameter at all, so a user-configured custom Ollama
// host (e.g. a remote GPU box) was silently ignored — the check always
// reported "not running" against localhost regardless of where the real
// instance actually lived. Passing a host explicitly must probe *that*
// host, not the default.
func TestCheckOllamaUsesConfiguredHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"0.1.0"}`))
	}))
	defer srv.Close()

	// Deliberately leave the package-level default pointed somewhere dead
	// — CheckOllama must still find the running instance because it was
	// told exactly where to look, not because of the default.
	orig := OllamaHost
	OllamaHost = "http://127.0.0.1:1"
	defer func() { OllamaHost = orig }()

	status := CheckOllama(context.Background(), srv.URL)
	if !status.Running {
		t.Fatalf("expected Running=true for the explicitly configured host, got %+v", status)
	}
}

// TestCheckOllamaConfiguredHostNotRunningDoesNotFallBackToLocalBinary
// confirms a configured remote host that doesn't answer is reported as
// not installed either, regardless of whether *this* machine happens to
// have the ollama binary on PATH — that fact says nothing about whether
// the configured remote host is real.
func TestCheckOllamaConfiguredHostNotRunningDoesNotFallBackToLocalBinary(t *testing.T) {
	status := CheckOllama(context.Background(), "http://127.0.0.1:1")
	if status.Running {
		t.Fatalf("expected Running=false for an unreachable configured host, got %+v", status)
	}
	if status.Installed {
		t.Fatalf("expected Installed=false for a configured remote host that didn't answer (a local PATH lookup isn't meaningful here), got %+v", status)
	}
}
