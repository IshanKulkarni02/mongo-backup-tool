package depmanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

// TestValidateInstallScript is the regression test for #36: the Ollama
// Linux auto-install downloaded and piped a remote script straight into sh
// with no check on what was actually received. validateInstallScript is
// the sanity gate added to catch the common ways that download can go
// wrong before any of it reaches a shell.
func TestValidateInstallScript(t *testing.T) {
	cases := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{"valid shebang script", []byte("#!/bin/sh\necho hi\n"), false},
		{"leading whitespace before shebang", []byte("\n\n  #!/bin/sh\necho hi\n"), false},
		{"empty body", []byte(""), true},
		{"whitespace only", []byte("   \n\t  "), true},
		{"html error page", []byte("<html><body>404 Not Found</body></html>"), true},
		{"truncated body missing shebang", []byte("in/sh\necho hi\n"), true},
		{"plain text no shebang", []byte("echo hi\n"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateInstallScript(c.data)
			if (err != nil) != c.wantErr {
				t.Fatalf("validateInstallScript(%q) error = %v, wantErr %v", c.data, err, c.wantErr)
			}
		})
	}
}

// TestDownloadOllamaInstallScriptRejectsInvalidContent confirms the
// download path refuses to hand a bad response to sh: an HTML error page
// (e.g. from a broken redirect or CDN failure) must be rejected rather
// than written out as an executable script.
func TestDownloadOllamaInstallScriptRejectsInvalidContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><body>not the script you're looking for</body></html>"))
	}))
	defer srv.Close()

	orig := ollamaInstallScriptURL
	ollamaInstallScriptURL = srv.URL
	defer func() { ollamaInstallScriptURL = orig }()

	path, err := downloadOllamaInstallScript(context.Background())
	if err == nil {
		os.Remove(path)
		t.Fatalf("expected an error for non-script content, got a script written to %q", path)
	}
	if !strings.Contains(err.Error(), "integrity check") {
		t.Fatalf("expected an integrity-check error, got: %v", err)
	}
}

// TestDownloadOllamaInstallScriptRejectsHTTPError confirms a non-200
// response (e.g. a 404 after Ollama moves the script) is rejected instead
// of writing the error body out as a script.
func TestDownloadOllamaInstallScriptRejectsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	orig := ollamaInstallScriptURL
	ollamaInstallScriptURL = srv.URL
	defer func() { ollamaInstallScriptURL = orig }()

	if _, err := downloadOllamaInstallScript(context.Background()); err == nil {
		t.Fatal("expected an error for a non-200 response, got nil")
	}
}

// TestDownloadOllamaInstallScriptWritesValidScript confirms the happy
// path: a well-formed script is downloaded to a private temp file whose
// content matches exactly what the server sent.
func TestDownloadOllamaInstallScriptWritesValidScript(t *testing.T) {
	const script = "#!/bin/sh\necho installing ollama\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(script))
	}))
	defer srv.Close()

	orig := ollamaInstallScriptURL
	ollamaInstallScriptURL = srv.URL
	defer func() { ollamaInstallScriptURL = orig }()

	path, err := downloadOllamaInstallScript(context.Background())
	if err != nil {
		t.Fatalf("downloadOllamaInstallScript failed: %v", err)
	}
	defer os.Remove(path)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read downloaded script: %v", err)
	}
	if string(got) != script {
		t.Fatalf("downloaded script content = %q, want %q", got, script)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat downloaded script: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("downloaded script has permissions %o, want 0700", perm)
	}
}

// TestDownloadOllamaInstallScriptRejectsOversized confirms a response
// larger than the expected size limit is rejected rather than silently
// truncated and executed.
func TestDownloadOllamaInstallScriptRejectsOversized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("#!/bin/sh\n"))
		w.Write(make([]byte, maxOllamaInstallScriptSize+1))
	}))
	defer srv.Close()

	orig := ollamaInstallScriptURL
	ollamaInstallScriptURL = srv.URL
	defer func() { ollamaInstallScriptURL = orig }()

	if _, err := downloadOllamaInstallScript(context.Background()); err == nil {
		t.Fatal("expected an error for an oversized response, got nil")
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
