package coord

import (
	"context"
	"testing"
	"time"

	"github.com/r00takaspin/gpumesh/internal/proto"
)

func TestRegistryRegisterUnregister(t *testing.T) {
	r := NewRegistry()
	r.Register(&MachineSession{
		MachineID:     "mch_1",
		SessionID:     "s1",
		UserID:        1,
		Models:        []string{"llama3.2:3b"},
		MaxConcurrent: 1,
		Description:   "test machine",
	})

	if !r.HasModel("mch_1", "llama3.2:3b") {
		t.Fatal("expected model on machine")
	}
	if r.GetSession("mch_1") == nil {
		t.Fatal("expected session")
	}
	if r.OnlineCount() != 1 {
		t.Fatalf("expected 1 online, got %d", r.OnlineCount())
	}

	r.Unregister("mch_1")
	if r.GetSession("mch_1") != nil {
		t.Fatal("expected nil after unregister")
	}
	if r.HasModel("mch_1", "llama3.2:3b") {
		t.Fatal("expected no model after unregister")
	}
	if r.OnlineCount() != 0 {
		t.Fatalf("expected 0 online")
	}
}

func TestRegistryHeartbeatEviction(t *testing.T) {
	r := NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.StartHeartbeatMonitor(ctx, 50*time.Millisecond, 20*time.Millisecond)

	r.Register(&MachineSession{
		MachineID:     "mch_1",
		UserID:        1,
		Models:        []string{"m"},
		MaxConcurrent: 1,
	})
	if r.GetSession("mch_1") == nil {
		t.Fatal("expected session to exist")
	}

	time.Sleep(120 * time.Millisecond)
	if r.GetSession("mch_1") != nil {
		t.Fatal("expected session to be evicted")
	}
}

func TestRegistryHeartbeatKeepsAlive(t *testing.T) {
	r := NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.StartHeartbeatMonitor(ctx, 80*time.Millisecond, 20*time.Millisecond)

	r.Register(&MachineSession{
		MachineID:     "mch_1",
		UserID:        1,
		Models:        []string{"m"},
		MaxConcurrent: 1,
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5; i++ {
			r.UpdateHeartbeat("mch_1")
			time.Sleep(30 * time.Millisecond)
		}
	}()
	<-done
	time.Sleep(50 * time.Millisecond)

	if r.GetSession("mch_1") == nil {
		t.Fatal("expected session to survive with heartbeats")
	}
}

func TestRegistrySnapshot(t *testing.T) {
	r := NewRegistry()
	r.Register(&MachineSession{
		MachineID: "mch_1", UserID: 1, Models: []string{"llama3.2:3b"}, MaxConcurrent: 2,
	})
	r.Register(&MachineSession{
		MachineID: "mch_2", UserID: 2, Models: []string{"llama3.2:3b", "mistral"}, MaxConcurrent: 1,
	})
	snap := r.Snapshot()
	if snap.DonorsOnline != 2 {
		t.Fatalf("expected 2 online, got %d", snap.DonorsOnline)
	}
	llama := snap.Models["llama3.2:3b"]
	if llama.DonorsOnline != 2 {
		t.Fatalf("expected 2 for llama, got %d", llama.DonorsOnline)
	}
	_ = proto.ScopeProvider
}

func TestRegistryLoad(t *testing.T) {
	r := NewRegistry()
	r.Register(&MachineSession{
		MachineID: "mch_1", Models: []string{"m"}, MaxConcurrent: 1,
	})
	if !r.IncrementLoad("mch_1") {
		t.Fatal("expected increment ok")
	}
	if r.IncrementLoad("mch_1") {
		t.Fatal("expected increment fail at capacity")
	}
	r.DecrementLoad("mch_1")
	if !r.IncrementLoad("mch_1") {
		t.Fatal("expected increment ok after decrement")
	}
}
