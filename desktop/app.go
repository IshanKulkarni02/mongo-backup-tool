package main

import (
	"context"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/mongodb"
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/mysql"
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/postgres"
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/sqlite"
	"github.com/IshanKulkarni02/dbhelm/internal/listener"
	"github.com/IshanKulkarni02/dbhelm/internal/service"
)

// App is the single facade bound to the frontend. Every exported method
// becomes a callable window.go.main.App.* function in the frontend, all
// thin wrappers over internal/* so the desktop app never diverges from the
// CLI/TUI's behavior.
type App struct {
	ctx      context.Context
	jobs     *jobManager
	engines  *engine.Manager
	webhookL *listener.Listener
	webhookM sync.Mutex
}

func NewApp() *App {
	a := &App{jobs: newJobManager()}
	a.engines = engine.NewManager(service.ResolveEngineConn)
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.jobs.onUpdate = func(j Job) {
		runtime.EventsEmit(a.ctx, "job:update", j)
	}
	a.jobs.onProgress = func(p JobProgress) {
		runtime.EventsEmit(a.ctx, "job:progress", p)
	}
	// Move any plaintext passwords from config.json into the OS keychain.
	// Best-effort: a failure just leaves the pre-keychain behavior.
	go func() {
		if _, err := config.MigrateCredentials(); err != nil {
			runtime.LogWarningf(ctx, "credential migration: %v", err)
		}
	}()
}

// shutdownJobWaitTimeout bounds how long shutdown waits for in-flight
// background jobs (backups, restores, ad-hoc queries) to finish before
// closing engine connections out from under them. A stuck job shouldn't be
// able to hang app exit forever, so this is a best-effort grace period, not
// a guarantee every job completes.
const shutdownJobWaitTimeout = 30 * time.Second

func (a *App) shutdown(ctx context.Context) {
	waitCtx, cancel := context.WithTimeout(context.Background(), shutdownJobWaitTimeout)
	defer cancel()
	a.jobs.waitAll(waitCtx)
	if waitCtx.Err() != nil {
		runtime.LogWarningf(ctx, "shutdown: in-flight jobs did not finish within %s; closing connections anyway", shutdownJobWaitTimeout)
	}

	a.engines.Close()
	a.webhookM.Lock()
	if a.webhookL != nil {
		a.webhookL.Stop(context.Background())
		a.webhookL = nil
	}
	a.webhookM.Unlock()
}

// CancelJob cancels a running cancelable job (currently: ad-hoc SQL
// queries started via RunSQLQueryJob), reporting whether one was found.
func (a *App) CancelJob(id string) bool {
	return a.jobs.cancel(id)
}

// redactURI masks a URI's password for safe display in the frontend.
func redactURI(raw string) string {
	return config.RedactURI(raw)
}
