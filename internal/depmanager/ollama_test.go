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

	status := CheckOllama(context.Background())
	if !status.Running || !status.Installed {
		t.Fatalf("expected Running=true Installed=true, got %+v", status)
	}
}

func TestCheckOllamaNotRunning(t *testing.T) {
	orig := OllamaHost
	OllamaHost = "http://127.0.0.1:1" // nothing listens here
	defer func() { OllamaHost = orig }()

	status := CheckOllama(context.Background())
	if status.Running {
		t.Fatalf("expected Running=false when nothing answers, got %+v", status)
	}
}
