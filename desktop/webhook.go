package main

import (
	"context"
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/listener"
)

// StartWebhookListener starts (or restarts, if already running) a local
// HTTP server on port that logs every request it receives via the
// "webhook:request" event — for debugging a device integration (e.g. a
// ZKTeco/eSSL biometric terminal's ADMS push protocol) by pointing it at
// this listener instead of production.
func (a *App) StartWebhookListener(port int) (string, error) {
	a.webhookM.Lock()
	defer a.webhookM.Unlock()
	if a.webhookL != nil {
		a.webhookL.Stop(context.Background())
		a.webhookL = nil
	}
	l, err := listener.Start(port, func(r listener.Request) {
		runtime.EventsEmit(a.ctx, "webhook:request", r)
	})
	if err != nil {
		return "", fmt.Errorf("starting listener on port %d: %w", port, err)
	}
	a.webhookL = l
	return l.Addr(), nil
}

// StopWebhookListener stops the running listener, if any.
func (a *App) StopWebhookListener() error {
	a.webhookM.Lock()
	defer a.webhookM.Unlock()
	if a.webhookL == nil {
		return nil
	}
	err := a.webhookL.Stop(context.Background())
	a.webhookL = nil
	return err
}

// IsWebhookListenerRunning reports whether a listener is currently active.
func (a *App) IsWebhookListenerRunning() bool {
	a.webhookM.Lock()
	defer a.webhookM.Unlock()
	return a.webhookL != nil
}

// InsertWebhookPayload inserts a captured request's raw body as a document
// into a MongoDB collection (Extended JSON) — the "map to DB insert" path
// for turning a captured device payload into a real row, gated by the same
// Safe Mode check every other write goes through.
func (a *App) InsertWebhookPayload(connectionName, database, collection, docJSON string) error {
	if err := a.requireWritable(connectionName); err != nil {
		return err
	}
	sess, release, err := a.docSession(connectionName)
	if err != nil {
		return err
	}
	defer release()
	return sess.InsertDocument(context.Background(), database, collection, docJSON)
}
