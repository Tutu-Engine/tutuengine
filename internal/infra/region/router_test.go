package region

import (
	"testing"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ═══════════════════════════════════════════════════════════════════════════
// Router Tests — Phase 3
// ═══════════════════════════════════════════════════════════════════════════

func newTestRouter(t *testing.T, local domain.RegionID) *Router {
	t.Helper()
	return NewRouter(Config{
		LocalRegion:   local,
		LoadThreshold: 0.8,
		MaxLatencyMs:  200,
	})
}

// ─── Construction ───────────────────────────────────────────────────────────

func TestNewRouter_InitializesAllRegions(t *testing.T) {
	r := newTestRouter(t, domain.RegionUSEast)
	statuses := r.AllRegionStatuses()
	if len(statuses) != len(domain.AllRegions()) {
		t.Fatalf("want %d regions, got %d", len(domain.AllRegions()), len(statuses))
	}
	for _, s := range statuses {
		if !s.Healthy {
			t.Errorf("region %s should be healthy by default", s.Region)
		}
	}
}

func TestNewRouter_DefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.LoadThreshold != 0.8 {
		t.Errorf("LoadThreshold = %f, want 0.8", cfg.LoadThreshold)
	}
	if cfg.MaxLatencyMs != 200 {
		t.Errorf("MaxLatencyMs = %d, want 200", cfg.MaxLatencyMs)
	}
}

// ─── Status Updates ─────────────────────────────────────────────────────────

func TestRouter_UpdateRegion(t *testing.T) {
	r := newTestRouter(t, domain.RegionUSEast)
	r.UpdateRegion(domain.RegionStatus{
		Region:      domain.RegionEUWest,
		Healthy:     true,
		NodeCount:   50,
		ActiveTasks: 20,
		UpdatedAt:   time.Now(),
	})
	s, ok := r.RegionStatus(domain.RegionEUWest)
	if !ok {
		t.Fatal("RegionStatus() returned false")
	}
	if s.NodeCount != 50 {
		t.Errorf("NodeCount = %d, want 50", s.NodeCount)
	}
	if s.ActiveTasks != 20 {
		t.Errorf("ActiveTasks = %d, want 20", s.ActiveTasks)
	}
}

func TestRouter_HealthyRegionCount(t *testing.T) {
	r := newTestRouter(t, domain.RegionUSEast)
	if got := r.HealthyRegionCount(); got != 3 {
		t.Errorf("HealthyRegionCount() = %d, want 3", got)
	}

	r.UpdateRegion(domain.RegionStatus{
		Region:  domain.RegionAPSouth,
		Healthy: false,
	})
	if got := r.HealthyRegionCount(); got != 2 {
		t.Errorf("after unhealthy, HealthyRegionCount() = %d, want 2", got)
	}
}

// ─── Routing Priority 1: Data Residency ─────────────────────────────────────

func TestRouter_Route_DataResidency(t *testing.T) {
	r := newTestRouter(t, domain.RegionUSEast)
	routing := domain.TaskRouting{DataResidency: domain.RegionEUWest}

	decision := r.Route(routing)
	if decision.Reason != "data-residency" {
		t.Errorf("Reason = %q, want %q", decision.Reason, "data-residency")
	}
	if decision.TargetRegion != domain.RegionEUWest {
		t.Errorf("TargetRegion = %s, want %s", decision.TargetRegion, domain.RegionEUWest)
	}
	if decision.SourceRegion != domain.RegionUSEast {
		t.Errorf("SourceRegion = %s, want %s", decision.SourceRegion, domain.RegionUSEast)
	}
}

// ─── Routing Priority 2: Preferred Region ───────────────────────────────────

func TestRouter_Route_PreferredRegion(t *testing.T) {
	r := newTestRouter(t, domain.RegionUSEast)
	// Preferred region healthy + low load → should pick it
	r.UpdateRegion(domain.RegionStatus{
		Region:      domain.RegionAPSouth,
		Healthy:     true,
		NodeCount:   100,
		ActiveTasks: 10,
	})
	routing := domain.TaskRouting{
		RegionAffinity: []domain.RegionID{domain.RegionAPSouth},
	}
	decision := r.Route(routing)
	if decision.Reason != "preferred-region" {
		t.Errorf("Reason = %q, want %q", decision.Reason, "preferred-region")
	}
	if decision.TargetRegion != domain.RegionAPSouth {
		t.Errorf("TargetRegion = %s, want %s", decision.TargetRegion, domain.RegionAPSouth)
	}
}

func TestRouter_Route_PreferredRegion_Overloaded(t *testing.T) {
	r := newTestRouter(t, domain.RegionUSEast)
	// Preferred region overloaded → should fallback
	r.UpdateRegion(domain.RegionStatus{
		Region:      domain.RegionAPSouth,
		Healthy:     true,
		NodeCount:   10,
		ActiveTasks: 50, // load 5.0 >> 0.8
	})
	routing := domain.TaskRouting{
		RegionAffinity: []domain.RegionID{domain.RegionAPSouth},
	}
	decision := r.Route(routing)
	if decision.Reason == "preferred-region" {
		t.Error("should NOT route to overloaded preferred region")
	}
}

// ─── Routing Priority 3: Same Region ────────────────────────────────────────

func TestRouter_Route_SameRegion(t *testing.T) {
	r := newTestRouter(t, domain.RegionUSEast)
	// Local region healthy, low load, no preference → same-region
	r.UpdateRegion(domain.RegionStatus{
		Region:      domain.RegionUSEast,
		Healthy:     true,
		NodeCount:   100,
		ActiveTasks: 10,
	})
	routing := domain.TaskRouting{} // no constraints
	decision := r.Route(routing)
	if decision.Reason != "same-region" {
		t.Errorf("Reason = %q, want %q", decision.Reason, "same-region")
	}
	if decision.LatencyPenalty != 0 {
		t.Errorf("LatencyPenalty = %d, want 0 for same-region", decision.LatencyPenalty)
	}
}

// ─── Routing Priority 4: Lowest Load Failover ──────────────────────────────

func TestRouter_Route_LowestLoad_Failover(t *testing.T) {
	r := newTestRouter(t, domain.RegionUSEast)
	// Local region overloaded
	r.UpdateRegion(domain.RegionStatus{
		Region:      domain.RegionUSEast,
		Healthy:     true,
		NodeCount:   10,
		ActiveTasks: 50,
	})
	// EU-West has low load
	r.UpdateRegion(domain.RegionStatus{
		Region:      domain.RegionEUWest,
		Healthy:     true,
		NodeCount:   100,
		ActiveTasks: 5,
	})
	routing := domain.TaskRouting{}
	decision := r.Route(routing)
	if decision.Reason != "lowest-load" {
		t.Errorf("Reason = %q, want %q", decision.Reason, "lowest-load")
	}
	if decision.TargetRegion != domain.RegionEUWest {
		t.Errorf("TargetRegion = %s, want %s", decision.TargetRegion, domain.RegionEUWest)
	}
}

// ─── Routing Fallback ───────────────────────────────────────────────────────

func TestRouter_Route_Fallback_AllUnhealthy(t *testing.T) {
	r := newTestRouter(t, domain.RegionUSEast)
	// Mark all regions as unhealthy
	for _, reg := range domain.AllRegions() {
		r.UpdateRegion(domain.RegionStatus{Region: reg, Healthy: false})
	}
	routing := domain.TaskRouting{}
	decision := r.Route(routing)
	if decision.Reason != "fallback" {
		t.Errorf("Reason = %q, want %q", decision.Reason, "fallback")
	}
	if decision.TargetRegion != domain.RegionUSEast {
		t.Errorf("TargetRegion = %s, want local region %s", decision.TargetRegion, domain.RegionUSEast)
	}
}
