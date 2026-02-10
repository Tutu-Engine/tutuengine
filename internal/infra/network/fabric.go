// Package network provides the node-side network fabric.
// Architecture Parts VIII, IX, XIV: Node registration, heartbeat, task subscription.
//
// The Fabric manages the node's connection to Cloud Core and the local
// SWIM gossip for peer discovery. It supports graceful offline mode
// (Architecture Part XVIII) — if Cloud Core is unreachable, the node
// continues serving local tasks and reconnects automatically.
package network

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/infra/gossip"
	"github.com/tutu-network/tutu/internal/infra/resource"
	"github.com/tutu-network/tutu/internal/security"
)

// FabricConfig configures the network fabric.
type FabricConfig struct {
	Enabled           bool
	CloudCoreEndpoint string
	HeartbeatInterval time.Duration
	Region            string
	GossipConfig      gossip.Config
}

// DefaultFabricConfig returns defaults matching Architecture Part VIII.
func DefaultFabricConfig() FabricConfig {
	return FabricConfig{
		Enabled:           false, // Opt-in for Phase 1
		CloudCoreEndpoint: "https://api.tutu.network",
		HeartbeatInterval: 10 * time.Second,
		Region:            "auto",
		GossipConfig:      gossip.DefaultConfig(),
	}
}

// NodeStatus represents the node's current operational state.
type NodeStatus struct {
	IsOnline     bool          `json:"is_online"`
	NodeID       string        `json:"node_id"`
	Region       string        `json:"region"`
	Uptime       time.Duration `json:"uptime"`
	ActiveTasks  int           `json:"active_tasks"`
	PeerCount    int           `json:"peer_count"`
	IdleLevel    string        `json:"idle_level"`
}

// Fabric manages the node's network connections.
type Fabric struct {
	mu          sync.RWMutex
	config      FabricConfig
	nodeID      string
	keypair     *security.Keypair
	governor    *resource.Governor
	swim        *gossip.SWIM
	isOnline    bool
	stopped     bool // Prevents re-registration after Stop()
	startedAt   time.Time
	activeTasks int

	// Task handler receives task assignments from Cloud Core
	taskHandler func(task domain.Task) error
}

// NewFabric creates a network fabric.
func NewFabric(cfg FabricConfig, kp *security.Keypair, gov *resource.Governor) *Fabric {
	nodeID := kp.PublicKeyHex()

	f := &Fabric{
		config:    cfg,
		nodeID:    nodeID,
		keypair:   kp,
		governor:  gov,
		startedAt: time.Now(),
	}

	// Initialize SWIM gossip
	f.swim = gossip.New(nodeID, cfg.GossipConfig, kp)
	f.swim.OnJoin(func(id string) {
		log.Printf("[network] peer joined: %s", id)
	})
	f.swim.OnLeave(func(id string) {
		log.Printf("[network] peer left: %s", id)
	})

	return f
}

// OnTaskAssigned sets the handler for incoming task assignments.
func (f *Fabric) OnTaskAssigned(handler func(task domain.Task) error) {
	f.taskHandler = handler
}

// NodeID returns this node's public key hex identifier.
func (f *Fabric) NodeID() string {
	return f.nodeID
}

// Start begins the network fabric. Returns immediately if network is disabled.
func (f *Fabric) Start(ctx context.Context) error {
	if !f.config.Enabled {
		log.Println("[network] disabled — running in local-only mode")
		return nil
	}

	log.Printf("[network] starting node %s in region %s", f.nodeID[:16], f.config.Region)

	// Register with Cloud Core
	if err := f.register(ctx); err != nil {
		log.Printf("[network] registration failed (offline mode): %v", err)
		// Continue in offline mode — Architecture Part XVIII
	}

	// Start SWIM gossip in background
	go func() {
		if err := f.swim.Start(ctx); err != nil {
			log.Printf("[network] gossip error: %v", err)
		}
	}()

	// Start heartbeat in background
	go f.heartbeatLoop(ctx)

	return nil
}

// Stop gracefully disconnects the node.
func (f *Fabric) Stop() {
	f.mu.Lock()
	f.stopped = true
	f.isOnline = false
	f.mu.Unlock()

	log.Printf("[network] node %s going offline", f.nodeID[:16])
}

// Status returns the node's current network status.
func (f *Fabric) Status() NodeStatus {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return NodeStatus{
		IsOnline:    f.isOnline,
		NodeID:      f.nodeID,
		Region:      f.config.Region,
		Uptime:      time.Since(f.startedAt),
		ActiveTasks: f.activeTasks,
		PeerCount:   f.swim.AliveCount(),
		IdleLevel:   f.governor.IdleLevel().String(),
	}
}

// Peers returns known peers from SWIM gossip.
func (f *Fabric) Peers() []domain.Peer {
	return f.swim.Members()
}

// JoinPeers seeds the gossip layer with known peer addresses.
func (f *Fabric) JoinPeers(addrs []string) error {
	return f.swim.Join(addrs)
}

// IsOnline returns whether the node is connected to Cloud Core.
func (f *Fabric) IsOnline() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.isOnline
}

// ─── Cloud Core Communication ───────────────────────────────────────────────
// Phase 1: Stub implementations. Full gRPC client added when Cloud Core is built.

// register sends the registration request to Cloud Core.
func (f *Fabric) register(ctx context.Context) error {
	// Phase 1: Stub — Cloud Core not yet deployed
	// When implemented, this sends: NodeID, PublicKey, Hardware, Region
	// and receives: assigned region, bootstrap peers, initial credits

	log.Printf("[network] registration stub — cloud core at %s", f.config.CloudCoreEndpoint)

	f.mu.Lock()
	if !f.stopped {
		f.isOnline = true
	}
	f.mu.Unlock()

	return nil
}

// heartbeatLoop sends periodic heartbeats to Cloud Core.
func (f *Fabric) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(f.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.sendHeartbeat(ctx)
		}
	}
}

// sendHeartbeat sends a single heartbeat.
func (f *Fabric) sendHeartbeat(ctx context.Context) {
	f.mu.RLock()
	stopped := f.stopped
	f.mu.RUnlock()
	if stopped {
		return
	}

	if !f.IsOnline() {
		// Try to reconnect
		if err := f.register(ctx); err != nil {
			return
		}
	}

	budget := f.governor.Budget()
	status := f.Status()

	_ = budget  // Will be sent in heartbeat payload
	_ = status  // Will be sent in heartbeat payload

	// Phase 1: Heartbeat stub
	// When implemented, sends: NodeID, CPUUsage, GPUUsage, MemoryAvailable,
	// ActiveTasks, IdleLevel, GPUTemp
}

// ─── Task Management ────────────────────────────────────────────────────────

// IncrActiveTasks increments the active task counter.
func (f *Fabric) IncrActiveTasks() {
	f.mu.Lock()
	f.activeTasks++
	f.mu.Unlock()
}

// DecrActiveTasks decrements the active task counter.
func (f *Fabric) DecrActiveTasks() {
	f.mu.Lock()
	if f.activeTasks > 0 {
		f.activeTasks--
	}
	f.mu.Unlock()
}

// SubmitTaskResult reports a completed task to Cloud Core.
func (f *Fabric) SubmitTaskResult(ctx context.Context, task domain.Task) error {
	if !f.IsOnline() {
		return fmt.Errorf("node is offline")
	}

	// Phase 1: Stub
	log.Printf("[network] task result stub: %s status=%s", task.ID, task.Status)
	return nil
}
