package coord

import (
	"context"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRegisterFindUnregister(t *testing.T) {
	r := NewRegistry()

	d := &Donor{
		ProviderID:    "p1",
		UserID:        1,
		Models:        []string{"llama3.2:3b", "codellama:7b"},
		MaxConcurrent: 2,
		Description:   "test donor",
		WSConn:        &websocket.Conn{},
	}
	r.Register(d)

	// Find by model.
	donors := r.FindDonorsForModel("llama3.2:3b")
	if len(donors) != 1 {
		t.Fatalf("expected 1 donor, got %d", len(donors))
	}
	if donors[0].ProviderID != "p1" {
		t.Fatalf("expected p1, got %s", donors[0].ProviderID)
	}

	// Unknown model.
	donors = r.FindDonorsForModel("nonexistent")
	if len(donors) != 0 {
		t.Fatalf("expected 0 donors for unknown model")
	}

	// Unregister.
	uid, reqs, toks := r.Unregister("p1")
	if uid != 1 || reqs != 0 || toks != 0 {
		t.Fatalf("unexpected unregister return: uid=%d reqs=%d toks=%d", uid, reqs, toks)
	}

	donors = r.FindDonorsForModel("llama3.2:3b")
	if len(donors) != 0 {
		t.Fatal("expected 0 donors after unregister")
	}

	snap := r.Snapshot()
	if snap.DonorsOnline != 0 {
		t.Fatalf("expected 0 donors in snapshot")
	}
}

func TestIncrementDecrementLoad(t *testing.T) {
	r := NewRegistry()

	d := &Donor{
		ProviderID:    "p1",
		MaxConcurrent: 2,
		WSConn:        &websocket.Conn{},
	}
	r.Register(d)

	if !r.IncrementLoad("p1") {
		t.Fatal("expected increment to succeed")
	}
	if !r.IncrementLoad("p1") {
		t.Fatal("expected second increment to succeed")
	}
	if r.IncrementLoad("p1") {
		t.Fatal("expected third increment to fail (at max)")
	}

	r.DecrementLoad("p1")
	if !r.IncrementLoad("p1") {
		t.Fatal("expected increment after decrement to succeed")
	}
}

func TestHeartbeatMonitor(t *testing.T) {
	r := NewRegistry()

	d := &Donor{
		ProviderID: "p1",
		WSConn:     &websocket.Conn{},
	}
	r.Register(d)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use short timeouts for testing.
	r.StartHeartbeatMonitor(ctx, 50*time.Millisecond, 20*time.Millisecond)

	// Verify donor exists.
	if r.GetDonor("p1") == nil {
		t.Fatal("expected donor to exist")
	}

	// Wait for eviction.
	time.Sleep(150 * time.Millisecond)

	if r.GetDonor("p1") != nil {
		t.Fatal("expected donor to be evicted")
	}
}

func TestHeartbeatKeepsAlive(t *testing.T) {
	r := NewRegistry()

	d := &Donor{
		ProviderID: "p1",
		WSConn:     &websocket.Conn{},
	}
	r.Register(d)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r.StartHeartbeatMonitor(ctx, 50*time.Millisecond, 10*time.Millisecond)

	// Keep sending heartbeats.
	for range 5 {
		time.Sleep(15 * time.Millisecond)
		r.UpdateHeartbeat("p1")
	}

	if r.GetDonor("p1") == nil {
		t.Fatal("expected donor to survive with heartbeats")
	}
}

func TestSnapshot(t *testing.T) {
	r := NewRegistry()

	r.Register(&Donor{
		ProviderID:    "p1",
		Models:        []string{"llama3.2:3b"},
		MaxConcurrent: 4,
		CurrentLoad:   1,
		WSConn:        &websocket.Conn{},
	})
	r.Register(&Donor{
		ProviderID:    "p2",
		Models:        []string{"llama3.2:3b", "mistral:7b"},
		MaxConcurrent: 2,
		CurrentLoad:   2,
		WSConn:        &websocket.Conn{},
	})

	snap := r.Snapshot()
	if snap.DonorsOnline != 2 {
		t.Fatalf("expected 2 donors online, got %d", snap.DonorsOnline)
	}
	if snap.ModelsOnline != 2 {
		t.Fatalf("expected 2 models online, got %d", snap.ModelsOnline)
	}

	llama := snap.Models["llama3.2:3b"]
	if llama.DonorsOnline != 2 {
		t.Fatalf("expected 2 donors for llama, got %d", llama.DonorsOnline)
	}
	// Load: p1=1/4=0.25, p2=2/2=1.0 → avg=0.625
	expectedLoad := 0.625
	if llama.Load < expectedLoad-0.01 || llama.Load > expectedLoad+0.01 {
		t.Fatalf("expected load ~%f, got %f", expectedLoad, llama.Load)
	}
}

func TestBadgeForTokens(t *testing.T) {
	tests := []struct {
		tokens int64
		badge  string
	}{
		{0, ""},
		{500, ""},
		{1000, "bronze"},
		{5000, "bronze"},
		{10000, "silver"},
		{50000, "silver"},
		{100000, "gold"},
		{500000, "gold"},
		{1000000, "platinum"},
		{2000000, "platinum"},
	}
	for _, tt := range tests {
		if got := BadgeForTokens(tt.tokens); got != tt.badge {
			t.Errorf("BadgeForTokens(%d) = %q, want %q", tt.tokens, got, tt.badge)
		}
	}
}
