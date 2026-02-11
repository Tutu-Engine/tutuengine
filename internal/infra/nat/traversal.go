// Package nat implements NAT traversal for direct node-to-node connections.
// Architecture Part XIV: Phase 2 (TURN relay) + Phase 3 (ICE punch-through).
//
// TuTu uses a 3-level fallback strategy:
//   1. Cloud-mediated (Phase 1): all traffic through Cloud Core (always works)
//   2. TURN relay (Phase 2): relay server adds ~20ms, 95% success rate
//   3. Direct P2P (Phase 3): UDP hole punching, <5ms, ~70% success rate
//
// This package provides the abstractions for levels 2 and 3.
package nat

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// ─── NAT Types ──────────────────────────────────────────────────────────────

// NATType classifies the type of NAT a node is behind.
type NATType int

const (
	NATUnknown           NATType = iota
	NATNone                      // Public IP — no NAT
	NATFullCone                  // Any external host can reach mapped port
	NATRestrictedCone            // Only hosts we've sent to can reach us
	NATPortRestricted            // Only host:port we've sent to can reach us
	NATSymmetric                 // Different mapping per destination (hardest to traverse)
)

// String returns a human-readable NAT type name.
func (n NATType) String() string {
	switch n {
	case NATNone:
		return "none"
	case NATFullCone:
		return "full-cone"
	case NATRestrictedCone:
		return "restricted-cone"
	case NATPortRestricted:
		return "port-restricted"
	case NATSymmetric:
		return "symmetric"
	default:
		return "unknown"
	}
}

// CanPunchThrough reports whether direct P2P is possible between two NAT types.
// Symmetric ↔ Symmetric is the only completely impossible combination.
func CanPunchThrough(a, b NATType) bool {
	if a == NATNone || b == NATNone {
		return true
	}
	if a == NATSymmetric && b == NATSymmetric {
		return false
	}
	if a == NATSymmetric && b == NATPortRestricted {
		return false
	}
	if b == NATSymmetric && a == NATPortRestricted {
		return false
	}
	return true
}

// ─── STUN Discovery ─────────────────────────────────────────────────────────

// STUNResult holds the result of a STUN NAT type detection.
type STUNResult struct {
	PublicAddr string  `json:"public_addr"`
	NATType    NATType `json:"nat_type"`
	LatencyMs  int     `json:"latency_ms"`
}

// STUNConfig configures STUN discovery.
type STUNConfig struct {
	ServerAddr string        // e.g., "stun.tutu.network:3478"
	Timeout    time.Duration // default 3s
}

// DefaultSTUNConfig returns sensible defaults.
func DefaultSTUNConfig() STUNConfig {
	return STUNConfig{
		ServerAddr: "stun.tutu.network:3478",
		Timeout:    3 * time.Second,
	}
}

// DiscoverNAT performs STUN-based NAT type detection.
// In this Phase 3 implementation, this is a local simulation that tests
// the local UDP endpoint. Real STUN requires a deployed server.
func DiscoverNAT(ctx context.Context, cfg STUNConfig) (*STUNResult, error) {
	start := time.Now()

	// Bind a local UDP socket to test connectivity
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, fmt.Errorf("nat: failed to bind UDP socket: %w", err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	// Attempt to resolve the STUN server to determine reachability
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(cfg.Timeout)
	}

	resolved, err := net.ResolveUDPAddr("udp4", cfg.ServerAddr)
	if err != nil {
		// Cannot reach STUN server — assume we're behind NAT
		return &STUNResult{
			PublicAddr: localAddr.String(),
			NATType:    NATUnknown,
			LatencyMs:  int(time.Since(start).Milliseconds()),
		}, nil
	}

	// Set deadline for the probe
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("nat: set deadline: %w", err)
	}

	// Send a minimal STUN binding request (RFC 5389 minimal format)
	// Magic cookie: 0x2112A442
	stunReq := []byte{
		0x00, 0x01, // Binding Request
		0x00, 0x00, // Length: 0 (no attributes)
		0x21, 0x12, 0xA4, 0x42, // Magic Cookie
		0x00, 0x00, 0x00, 0x00, // Transaction ID (12 bytes)
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x01,
	}

	_, err = conn.WriteTo(stunReq, resolved)
	if err != nil {
		return &STUNResult{
			PublicAddr: localAddr.String(),
			NATType:    NATSymmetric, // assume worst case if can't reach STUN
			LatencyMs:  int(time.Since(start).Milliseconds()),
		}, nil
	}

	// Try to read response
	buf := make([]byte, 1024)
	n, _, err := conn.ReadFrom(buf)
	latency := int(time.Since(start).Milliseconds())

	if err != nil || n < 20 {
		// Timeout or invalid response — behind restrictive NAT
		return &STUNResult{
			PublicAddr: localAddr.String(),
			NATType:    NATPortRestricted,
			LatencyMs:  latency,
		}, nil
	}

	// If we got a valid STUN response, we have at least restricted cone NAT
	return &STUNResult{
		PublicAddr: localAddr.String(),
		NATType:    NATRestrictedCone,
		LatencyMs:  latency,
	}, nil
}

// ─── TURN Relay ─────────────────────────────────────────────────────────────

// TURNConfig configures the TURN relay fallback.
type TURNConfig struct {
	ServerAddr string        // e.g., "turn.tutu.network:3478"
	Username   string        // TURN credentials
	Password   string        // TURN credentials
	Timeout    time.Duration // default 5s
}

// DefaultTURNConfig returns sensible defaults.
func DefaultTURNConfig() TURNConfig {
	return TURNConfig{
		ServerAddr: "turn.tutu.network:3478",
		Timeout:    5 * time.Second,
	}
}

// TURNRelay manages a TURN-relayed connection between two peers.
type TURNRelay struct {
	mu          sync.Mutex
	config      TURNConfig
	localAddr   string
	relayAddr   string
	established bool
	latencyMs   int
}

// NewTURNRelay creates a TURN relay connection manager.
func NewTURNRelay(cfg TURNConfig) *TURNRelay {
	return &TURNRelay{config: cfg}
}

// Allocate requests a relay allocation from the TURN server.
// In production, this sends TURN Allocate request (RFC 5766).
// Phase 3 stub: validates config and simulates allocation.
func (t *TURNRelay) Allocate(ctx context.Context) (relayAddr string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.config.ServerAddr == "" {
		return "", fmt.Errorf("nat: TURN server address not configured")
	}

	// In production: full TURN allocate handshake.
	// Phase 3: test local UDP connectivity as proof of concept.
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return "", fmt.Errorf("nat: TURN allocate failed: %w", err)
	}
	defer conn.Close()

	t.localAddr = conn.LocalAddr().String()
	t.relayAddr = t.config.ServerAddr // would be the assigned relay address
	t.established = true
	t.latencyMs = 20 // TURN adds ~20ms (Architecture Part XIV)

	return t.relayAddr, nil
}

// IsEstablished reports whether the relay is active.
func (t *TURNRelay) IsEstablished() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.established
}

// LatencyMs returns the relay overhead in milliseconds.
func (t *TURNRelay) LatencyMs() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.latencyMs
}

// Close tears down the TURN relay.
func (t *TURNRelay) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.established = false
	return nil
}

// ─── Connection Manager ─────────────────────────────────────────────────────

// ConnStrategy represents the chosen connection method.
type ConnStrategy int

const (
	StrategyCloudMediated ConnStrategy = iota // Phase 1: all through Cloud Core
	StrategyTURNRelay                         // Phase 2: TURN relay (~20ms overhead)
	StrategyDirectP2P                         // Phase 3: UDP hole punch (<5ms)
)

// String returns a human-readable strategy name.
func (s ConnStrategy) String() string {
	switch s {
	case StrategyCloudMediated:
		return "cloud-mediated"
	case StrategyTURNRelay:
		return "turn-relay"
	case StrategyDirectP2P:
		return "direct-p2p"
	default:
		return "unknown"
	}
}

// ConnResult captures the outcome of a connection attempt.
type ConnResult struct {
	Strategy  ConnStrategy `json:"strategy"`
	PeerID    string       `json:"peer_id"`
	LocalNAT  NATType      `json:"local_nat"`
	RemoteNAT NATType      `json:"remote_nat"`
	LatencyMs int          `json:"latency_ms"`
	Success   bool         `json:"success"`
	Error     string       `json:"error,omitempty"`
}

// NegotiateConnection attempts to establish the best possible connection
// to a remote peer, trying Direct P2P first, then TURN, then Cloud-mediated.
func NegotiateConnection(ctx context.Context, localNAT, remoteNAT NATType, peerID string, turnCfg TURNConfig) ConnResult {
	// Strategy 1: Try direct P2P if NAT types allow it
	if CanPunchThrough(localNAT, remoteNAT) {
		// In production: full ICE candidate exchange + UDP hole punching.
		// Phase 3: we simulate success based on NAT compatibility.
		if localNAT == NATNone || remoteNAT == NATNone || localNAT == NATFullCone || remoteNAT == NATFullCone {
			return ConnResult{
				Strategy:  StrategyDirectP2P,
				PeerID:    peerID,
				LocalNAT:  localNAT,
				RemoteNAT: remoteNAT,
				LatencyMs: 3,
				Success:   true,
			}
		}
	}

	// Strategy 2: TURN relay fallback
	relay := NewTURNRelay(turnCfg)
	_, err := relay.Allocate(ctx)
	if err == nil && relay.IsEstablished() {
		return ConnResult{
			Strategy:  StrategyTURNRelay,
			PeerID:    peerID,
			LocalNAT:  localNAT,
			RemoteNAT: remoteNAT,
			LatencyMs: relay.LatencyMs(),
			Success:   true,
		}
	}

	// Strategy 3: Cloud-mediated (always works)
	return ConnResult{
		Strategy:  StrategyCloudMediated,
		PeerID:    peerID,
		LocalNAT:  localNAT,
		RemoteNAT: remoteNAT,
		LatencyMs: 50,
		Success:   true,
	}
}
