package nat

import (
	"context"
	"testing"
)

// ═══════════════════════════════════════════════════════════════════════════
// NAT Traversal Tests — Phase 3
// ═══════════════════════════════════════════════════════════════════════════

// ─── NATType.String ─────────────────────────────────────────────────────────

func TestNATType_String(t *testing.T) {
	tests := []struct {
		nat  NATType
		want string
	}{
		{NATUnknown, "unknown"},
		{NATNone, "none"},
		{NATFullCone, "full-cone"},
		{NATRestrictedCone, "restricted-cone"},
		{NATPortRestricted, "port-restricted"},
		{NATSymmetric, "symmetric"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.nat.String(); got != tt.want {
				t.Errorf("NATType(%d).String() = %q, want %q", tt.nat, got, tt.want)
			}
		})
	}
}

// ─── CanPunchThrough Matrix ─────────────────────────────────────────────────

func TestCanPunchThrough(t *testing.T) {
	tests := []struct {
		name string
		a, b NATType
		want bool
	}{
		{"none-none", NATNone, NATNone, true},
		{"none-symmetric", NATNone, NATSymmetric, true},
		{"fullcone-fullcone", NATFullCone, NATFullCone, true},
		{"fullcone-restricted", NATFullCone, NATRestrictedCone, true},
		{"restricted-restricted", NATRestrictedCone, NATRestrictedCone, true},
		{"restricted-port_restricted", NATRestrictedCone, NATPortRestricted, true},
		{"symmetric-symmetric", NATSymmetric, NATSymmetric, false},
		{"symmetric-port_restricted", NATSymmetric, NATPortRestricted, false},
		{"port_restricted-symmetric", NATPortRestricted, NATSymmetric, false},
		{"symmetric-fullcone", NATSymmetric, NATFullCone, true},
		{"symmetric-restricted", NATSymmetric, NATRestrictedCone, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanPunchThrough(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("CanPunchThrough(%s, %s) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// ─── ConnStrategy.String ────────────────────────────────────────────────────

func TestConnStrategy_String(t *testing.T) {
	tests := []struct {
		s    ConnStrategy
		want string
	}{
		{StrategyCloudMediated, "cloud-mediated"},
		{StrategyTURNRelay, "turn-relay"},
		{StrategyDirectP2P, "direct-p2p"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("ConnStrategy(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

// ─── NegotiateConnection ────────────────────────────────────────────────────

func TestNegotiateConnection_DirectP2P_PublicIP(t *testing.T) {
	ctx := context.Background()
	result := NegotiateConnection(ctx, NATNone, NATNone, "peer-1", DefaultTURNConfig())
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.Strategy != StrategyDirectP2P {
		t.Errorf("Strategy = %s, want direct-p2p", result.Strategy)
	}
	if result.LatencyMs > 10 {
		t.Errorf("LatencyMs = %d, want < 10 for direct P2P", result.LatencyMs)
	}
}

func TestNegotiateConnection_DirectP2P_FullCone(t *testing.T) {
	ctx := context.Background()
	result := NegotiateConnection(ctx, NATFullCone, NATRestrictedCone, "peer-2", DefaultTURNConfig())
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.Strategy != StrategyDirectP2P {
		t.Errorf("Strategy = %s, want direct-p2p (FullCone can punch through)", result.Strategy)
	}
}

func TestNegotiateConnection_SymmetricSymmetric_FallsToTURNOrCloud(t *testing.T) {
	ctx := context.Background()
	turnCfg := DefaultTURNConfig()
	result := NegotiateConnection(ctx, NATSymmetric, NATSymmetric, "peer-3", turnCfg)
	if !result.Success {
		t.Fatal("expected success (fallback)")
	}
	// Cannot punch through, so should use TURN or Cloud
	if result.Strategy == StrategyDirectP2P {
		t.Error("Symmetric↔Symmetric should NOT use direct P2P")
	}
}

func TestNegotiateConnection_AlwaysSucceeds(t *testing.T) {
	ctx := context.Background()
	// Even worst case should succeed via cloud-mediated fallback
	turnCfg := TURNConfig{ServerAddr: "", Timeout: 0} // broken TURN
	result := NegotiateConnection(ctx, NATSymmetric, NATSymmetric, "peer-4", turnCfg)
	if !result.Success {
		t.Fatal("should always succeed via cloud-mediated fallback")
	}
	if result.Strategy != StrategyCloudMediated {
		t.Errorf("Strategy = %s, want cloud-mediated when TURN is broken", result.Strategy)
	}
}

// ─── TURN Relay ─────────────────────────────────────────────────────────────

func TestTURNRelay_Allocate(t *testing.T) {
	relay := NewTURNRelay(DefaultTURNConfig())
	if relay.IsEstablished() {
		t.Error("should not be established before Allocate()")
	}

	ctx := context.Background()
	addr, err := relay.Allocate(ctx)
	if err != nil {
		t.Fatalf("Allocate() error: %v", err)
	}
	if addr == "" {
		t.Error("Allocate() returned empty address")
	}
	if !relay.IsEstablished() {
		t.Error("should be established after Allocate()")
	}
	if relay.LatencyMs() != 20 {
		t.Errorf("LatencyMs() = %d, want 20", relay.LatencyMs())
	}
}

func TestTURNRelay_AllocateFails_NoServer(t *testing.T) {
	relay := NewTURNRelay(TURNConfig{ServerAddr: ""})
	ctx := context.Background()
	_, err := relay.Allocate(ctx)
	if err == nil {
		t.Error("Allocate() should fail with empty server address")
	}
}

func TestTURNRelay_Close(t *testing.T) {
	relay := NewTURNRelay(DefaultTURNConfig())
	ctx := context.Background()
	relay.Allocate(ctx)
	if err := relay.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if relay.IsEstablished() {
		t.Error("should not be established after Close()")
	}
}

// ─── Default Configs ────────────────────────────────────────────────────────

func TestDefaultSTUNConfig(t *testing.T) {
	cfg := DefaultSTUNConfig()
	if cfg.ServerAddr == "" {
		t.Error("ServerAddr should not be empty")
	}
	if cfg.Timeout <= 0 {
		t.Error("Timeout should be > 0")
	}
}

func TestDefaultTURNConfig(t *testing.T) {
	cfg := DefaultTURNConfig()
	if cfg.ServerAddr == "" {
		t.Error("ServerAddr should not be empty")
	}
	if cfg.Timeout <= 0 {
		t.Error("Timeout should be > 0")
	}
}
