package main

import (
	"context"
	"net/url"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the single facade bound to the frontend. Every exported method
// becomes a callable window.go.main.App.* function in the frontend, all
// thin wrappers over internal/* so the desktop app never diverges from the
// CLI/TUI's behavior.
type App struct {
	ctx  context.Context
	jobs *jobManager
}

func NewApp() *App {
	return &App{jobs: newJobManager()}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.jobs.onUpdate = func(j Job) {
		runtime.EventsEmit(a.ctx, "job:update", j)
	}
}

// redactURI masks a URI's password for safe display in the frontend.
// Duplicated (not imported) from cmd.redactURI, which is unexported and in
// a package the desktop module doesn't otherwise need to depend on.
func redactURI(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if _, hasPass := u.User.Password(); hasPass {
		u.User = url.UserPassword(u.User.Username(), "****")
	}
	return u.String()
}
