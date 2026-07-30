package main

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/testmongod"
)

// newTestAppWithMongoConn registers a connection named connName pointing at
// a real (test) mongod instance, for tests that need a working
// engine.DocumentSession — e.g. InsertWebhookPayload, which only mongodb
// implements.
func newTestAppWithMongoConn(t *testing.T, connName string, readOnly bool) (*App, string) {
	t.Helper()
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
	uri := testmongod.Start(t, "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Connections = append(cfg.Connections, config.Connection{
		Name: connName, URI: uri, Engine: "mongodb", ReadOnly: readOnly,
	})
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	a := NewApp()
	t.Cleanup(a.engines.Close)
	return a, uri
}

// TestStartWebhookListenerReturnsAddrAndToken guards against #43: the
// frontend needs both the bound (loopback) address and the auth token to
// show the user, so a device can be configured to send the token back.
func TestStartWebhookListenerReturnsAddrAndToken(t *testing.T) {
	a := &App{}
	info, err := a.StartWebhookListener(0)
	if err != nil {
		t.Fatalf("StartWebhookListener: %v", err)
	}
	defer a.StopWebhookListener()

	if info.Addr == "" {
		t.Error("expected a non-empty listener address")
	}
	if info.Token == "" {
		t.Error("expected a non-empty auth token")
	}
	if !a.IsWebhookListenerRunning() {
		t.Error("expected IsWebhookListenerRunning to report true")
	}
}

func TestStartWebhookListenerRestartsWithFreshToken(t *testing.T) {
	a := &App{}
	first, err := a.StartWebhookListener(0)
	if err != nil {
		t.Fatalf("StartWebhookListener: %v", err)
	}
	second, err := a.StartWebhookListener(0)
	if err != nil {
		t.Fatalf("StartWebhookListener (restart): %v", err)
	}
	defer a.StopWebhookListener()

	if first.Token == second.Token {
		t.Error("expected a fresh token on restart, got the same one")
	}
}

func TestStopWebhookListenerStopsRunningListener(t *testing.T) {
	a := &App{}
	if _, err := a.StartWebhookListener(0); err != nil {
		t.Fatalf("StartWebhookListener: %v", err)
	}
	if !a.IsWebhookListenerRunning() {
		t.Fatal("expected listener to be running before Stop")
	}

	if err := a.StopWebhookListener(); err != nil {
		t.Fatalf("StopWebhookListener: %v", err)
	}
	if a.IsWebhookListenerRunning() {
		t.Error("expected IsWebhookListenerRunning to report false after Stop")
	}
}

func TestStopWebhookListenerNoopWhenNotRunning(t *testing.T) {
	a := &App{}
	if err := a.StopWebhookListener(); err != nil {
		t.Fatalf("expected stopping a never-started listener to be a no-op, got: %v", err)
	}
}

func TestInsertWebhookPayloadInsertsDocument(t *testing.T) {
	a, uri := newTestAppWithMongoConn(t, "webhook-insert-test", false)

	docJSON := `{"deviceId":"term-1","event":"punch"}`
	if err := a.InsertWebhookPayload("webhook-insert-test", "webhookdb", "events", docJSON); err != nil {
		t.Fatalf("InsertWebhookPayload: %v", err)
	}

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	var doc bson.M
	if err := client.Database("webhookdb").Collection("events").FindOne(context.Background(), bson.D{{Key: "deviceId", Value: "term-1"}}).Decode(&doc); err != nil {
		t.Fatalf("expected the payload to be inserted and findable, got: %v", err)
	}
	if doc["event"] != "punch" {
		t.Errorf("inserted doc event = %v, want punch", doc["event"])
	}
}

func TestInsertWebhookPayloadRespectsReadOnly(t *testing.T) {
	a, _ := newTestAppWithMongoConn(t, "webhook-insert-ro-test", true)

	err := a.InsertWebhookPayload("webhook-insert-ro-test", "webhookdb", "events", `{"deviceId":"term-1"}`)
	if err == nil {
		t.Fatal("expected InsertWebhookPayload to refuse a write on a read-only connection")
	}
}
