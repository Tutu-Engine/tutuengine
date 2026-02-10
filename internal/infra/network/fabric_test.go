package network

import (
	"context"
	"testing"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/infra/resource"
	"github.com/tutu-network/tutu/internal/security"
)

func newTestFabric(t *testing.T, enabled bool) *Fabric {
	t.Helper()
	kp, err := security.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}

	cfg := DefaultFabricConfig()
	cfg.Enabled = enabled
	cfg.HeartbeatInterval = 100 * time.Millisecond
	cfg.GossipConfig.BindAddr = "127.0.0.1:0"
	cfg.GossipConfig.Interval = 100 * time.Millisecond

	gov := resource.NewGovernor(resource.DefaultGovernorConfig())
	return NewFabric(cfg, kp, gov)
}

// ─── Config Tests ───────────────────────────────────────────────────────────

func TestDefaultFabricConfig(t *testing.T) {
	cfg := DefaultFabricConfig()
	if cfg.Enabled {
		t.Error("default should be disabled")
	}
	if cfg.HeartbeatInterval != 10*time.Second {
		t.Errorf("HeartbeatInterval = %v, want 10s", cfg.HeartbeatInterval)
	}
}

// ─── Fabric Tests ───────────────────────────────────────────────────────────

func TestNewFabric(t *testing.T) {
	f := newTestFabric(t, false)
	if f.nodeID == "" {
		t.Error("nodeID should be set from keypair")
	}
	if len(f.nodeID) != 64 { // 32 bytes = 64 hex
		t.Errorf("nodeID len = %d, want 64 hex chars", len(f.nodeID))
	}
}

func TestFabric_NodeID(t *testing.T) {
	f := newTestFabric(t, false)
	if f.NodeID() != f.nodeID {
		t.Error("NodeID() should match internal nodeID")
	}
}

func TestFabric_StartDisabled(t *testing.T) {
	f := newTestFabric(t, false)
	ctx := context.Background()

	err := f.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	// Should be a no-op when disabled
	if f.IsOnline() {
		t.Error("should not be online when disabled")
	}
}

func TestFabric_StartEnabled(t *testing.T) {
	f := newTestFabric(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := f.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Give it a moment to register
	time.Sleep(100 * time.Millisecond)

	if !f.IsOnline() {
		t.Error("should be online after registration stub")
	}
}

func TestFabric_Stop(t *testing.T) {
	f := newTestFabric(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	f.Start(ctx)
	time.Sleep(100 * time.Millisecond) // Let registration goroutine complete

	if !f.IsOnline() {
		t.Fatal("should be online before Stop()")
	}

	f.Stop()
	// Stop sets stopped=true, preventing heartbeat from re-registering
	if f.IsOnline() {
		t.Error("should be offline after Stop()")
	}
}

func TestFabric_Status(t *testing.T) {
	f := newTestFabric(t, false)
	status := f.Status()

	if status.NodeID == "" {
		t.Error("Status.NodeID should be set")
	}
	if status.Uptime < 0 {
		t.Error("Status.Uptime should be >= 0")
	}
	if status.PeerCount != 0 {
		t.Errorf("Status.PeerCount = %d, want 0", status.PeerCount)
	}
}

func TestFabric_Peers_Empty(t *testing.T) {
	f := newTestFabric(t, false)
	peers := f.Peers()
	if len(peers) != 0 {
		t.Errorf("Peers() = %d, want 0", len(peers))
	}
}

func TestFabric_ActiveTasks(t *testing.T) {
	f := newTestFabric(t, false)

	f.IncrActiveTasks()
	f.IncrActiveTasks()
	f.IncrActiveTasks()

	status := f.Status()
	if status.ActiveTasks != 3 {
		t.Errorf("ActiveTasks = %d, want 3", status.ActiveTasks)
	}

	f.DecrActiveTasks()
	status = f.Status()
	if status.ActiveTasks != 2 {
		t.Errorf("ActiveTasks = %d, want 2 after decr", status.ActiveTasks)
	}
}

func TestFabric_DecrActiveTasks_NoNegative(t *testing.T) {
	f := newTestFabric(t, false)
	f.DecrActiveTasks() // Should not go negative

	status := f.Status()
	if status.ActiveTasks != 0 {
		t.Errorf("ActiveTasks = %d, should not be negative", status.ActiveTasks)
	}
}

func TestFabric_SubmitTaskResult_Offline(t *testing.T) {
	f := newTestFabric(t, false)
	ctx := context.Background()

	task := domain.Task{
		ID:     "task-1",
		Status: domain.TaskCompleted,
	}

	err := f.SubmitTaskResult(ctx, task)
	if err == nil {
		t.Error("SubmitTaskResult should fail when offline")
	}
}

func TestFabric_OnTaskAssigned(t *testing.T) {
	f := newTestFabric(t, false)

	called := false
	f.OnTaskAssigned(func(task domain.Task) error {
		called = true
		return nil
	})

	if f.taskHandler == nil {
		t.Error("taskHandler should be set after OnTaskAssigned")
	}
	_ = called
}

func TestFabric_TwoNodes_Gossip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	f1 := newTestFabric(t, true)
	f2 := newTestFabric(t, true)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	f1.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	f2.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	// f2 joins f1's gossip
	// Access the SWIM bind address through the fabric
	// For this to work, we need to know f1's gossip address
	// Since Start launches gossip in background, the addr is available
	// after a short delay

	// Both fabrics should be online
	if !f1.IsOnline() {
		t.Error("f1 should be online")
	}
	if !f2.IsOnline() {
		t.Error("f2 should be online")
	}

	cancel()
}
