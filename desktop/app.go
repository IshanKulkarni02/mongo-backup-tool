package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/config"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
	_ "github.com/IshanKulkarni02/mongo-backup-tool/internal/engine/mongodb"
	_ "github.com/IshanKulkarni02/mongo-backup-tool/internal/engine/mysql"
	_ "github.com/IshanKulkarni02/mongo-backup-tool/internal/engine/postgres"
	_ "github.com/IshanKulkarni02/mongo-backup-tool/internal/engine/sqlite"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine/tunnel"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/listener"
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
	a.engines = engine.NewManager(resolveEngineConn)
	return a
}

// resolveEngineConn maps a saved connection name to its engine and config
// for the session manager.
func resolveEngineConn(name string) (engine.ConnConfig, engine.Engine, error) {
	cfg, err := config.Load()
	if err != nil {
		return engine.ConnConfig{}, nil, err
	}
	conn, ok := cfg.Find(name)
	if !ok {
		return engine.ConnConfig{}, nil, fmt.Errorf("no connection named %q", name)
	}
	eng, err := engine.Lookup(conn.EngineID())
	if err != nil {
		return engine.ConnConfig{}, nil, err
	}
	connCfg := engine.ConnConfig{
		Name: conn.Name, URI: conn.URI, ReadOnly: conn.ReadOnly,
		TenantSessionVar: conn.TenantSessionVar, TenantValue: conn.TenantValue,
	}
	if conn.SSHHost != "" {
		connCfg.SSHTunnel = &tunnel.Config{
			Host:          conn.SSHHost,
			User:          conn.SSHUser,
			Password:      conn.SSHPassword,
			PrivateKeyPEM: conn.SSHPrivateKey,
		}
	}
	return connCfg, eng, nil
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

func (a *App) shutdown(ctx context.Context) {
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
