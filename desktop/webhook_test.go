package main

import "testing"

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
