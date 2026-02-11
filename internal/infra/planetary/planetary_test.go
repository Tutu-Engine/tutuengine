package planetary

import (
	"testing"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// fixedTime returns a deterministic time for testing.
func fixedTime() time.Time {
	return time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC)
}

// ═══════════════════════════════════════════════════════════════════════════
// TopologyManager Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestNewTopologyManager(t *testing.T) {
	tm := NewTopologyManager(DefaultConfig())
	if tm == nil {
		t.Fatal("expected non-nil TopologyManager")
	}
	if tm.ContinentCount() != 0 {
		t.Fatalf("expected 0 continents, got %d", tm.ContinentCount())
	}
}

func TestRegisterContinent(t *testing.T) {
	tm := NewTopologyManager(DefaultConfig())
	tm.now = fixedTime

	mesh := &domain.ContinentMesh{
		Continent: domain.ContinentNorthAmerica,
		Gateway:   "us-east-1",
		Regions: []domain.PlanetaryRegion{
			{Region: "us-east-1", Continent: domain.ContinentNorthAmerica, NodeCount: 500000, Healthy: true, LatencyMs: 5},
			{Region: "us-west-2", Continent: domain.ContinentNorthAmerica, NodeCount: 400000, Healthy: true, LatencyMs: 8},
		},
	}

	if err := tm.RegisterContinent(mesh); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tm.ContinentCount() != 1 {
		t.Fatalf("expected 1 continent, got %d", tm.ContinentCount())
	}
	if tm.TotalNodes() != 900000 {
		t.Fatalf("expected 900000 nodes, got %d", tm.TotalNodes())
	}
}

func TestRegisterContinent_InvalidContinent(t *testing.T) {
	tm := NewTopologyManager(DefaultConfig())
	mesh := &domain.ContinentMesh{Continent: "xx"}
	if err := tm.RegisterContinent(mesh); err == nil {
		t.Fatal("expected error for invalid continent")
	}
}

func TestRemoveContinent(t *testing.T) {
	tm := NewTopologyManager(DefaultConfig())
	tm.now = fixedTime

	mesh := &domain.ContinentMesh{
		Continent: domain.ContinentEurope,
		Gateway:   "eu-west-1",
		Regions:   []domain.PlanetaryRegion{{Region: "eu-west-1", Healthy: true, NodeCount: 300000}},
	}
	_ = tm.RegisterContinent(mesh)
	tm.RemoveContinent(domain.ContinentEurope)

	if tm.ContinentCount() != 0 {
		t.Fatal("expected 0 continents after removal")
	}
}

func TestTopology(t *testing.T) {
	tm := NewTopologyManager(DefaultConfig())
	tm.now = fixedTime

	// Register 3 continents
	continents := []struct {
		id      domain.ContinentID
		gateway string
		nodes   int64
	}{
		{domain.ContinentNorthAmerica, "us-east-1", 500000},
		{domain.ContinentEurope, "eu-west-1", 400000},
		{domain.ContinentAsia, "ap-east-1", 300000},
	}

	for _, c := range continents {
		mesh := &domain.ContinentMesh{
			Continent: c.id,
			Gateway:   domain.RegionID(c.gateway),
			Regions: []domain.PlanetaryRegion{
				{Region: domain.RegionID(c.gateway), Healthy: true, NodeCount: c.nodes / 2},
				{Region: domain.RegionID(c.gateway + "-b"), Healthy: true, NodeCount: c.nodes / 2},
			},
		}
		if err := tm.RegisterContinent(mesh); err != nil {
			t.Fatalf("register %s: %v", c.id, err)
		}
	}

	topo := tm.Topology()
	if topo.TotalNodes != 1200000 {
		t.Fatalf("expected 1200000 total nodes, got %d", topo.TotalNodes)
	}
	if topo.TotalRegions != 6 {
		t.Fatalf("expected 6 total regions, got %d", topo.TotalRegions)
	}
	if topo.GlobalHealth != 1.0 {
		t.Fatalf("expected 1.0 global health, got %f", topo.GlobalHealth)
	}
}

func TestIsQuorumHealthy(t *testing.T) {
	tests := []struct {
		name    string
		healthy int // Number of continents with enough healthy regions
		want    bool
	}{
		{"all 6 continents healthy", 6, true},
		{"4 of 6 healthy (meets quorum)", 4, true},
		{"3 of 6 healthy (below quorum)", 3, false},
		{"0 healthy", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.MinQuorumContinents = 4
			cfg.MinHealthyRegionsPerContinent = 1
			tm := NewTopologyManager(cfg)
			tm.now = fixedTime

			all := domain.AllContinents()
			for i := 0; i < len(all); i++ {
				healthy := i < tt.healthy
				mesh := &domain.ContinentMesh{
					Continent: all[i],
					Gateway:   domain.RegionID("gw-" + string(all[i])),
					Regions: []domain.PlanetaryRegion{
						{Region: domain.RegionID("r-" + string(all[i])), Healthy: healthy, NodeCount: 100000},
					},
				}
				_ = tm.RegisterContinent(mesh)
			}

			if got := tm.IsQuorumHealthy(); got != tt.want {
				t.Errorf("IsQuorumHealthy() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Routing Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestRoute_LocalContinent(t *testing.T) {
	tm := NewTopologyManager(DefaultConfig())
	tm.now = fixedTime

	// Register healthy North America
	mesh := &domain.ContinentMesh{
		Continent: domain.ContinentNorthAmerica,
		Gateway:   "us-east-1",
		Regions: []domain.PlanetaryRegion{
			{Region: "us-east-1", Healthy: true, NodeCount: 500000, LatencyMs: 5},
			{Region: "us-west-2", Healthy: true, NodeCount: 400000, LatencyMs: 3},
		},
	}
	_ = tm.RegisterContinent(mesh)

	result, err := tm.Route(domain.ContinentNorthAmerica, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TargetContinent != domain.ContinentNorthAmerica {
		t.Fatalf("expected NA, got %s", result.TargetContinent)
	}
	if result.Hops != 0 {
		t.Fatalf("expected 0 hops for local, got %d", result.Hops)
	}
	if result.Reason != "local_continent" {
		t.Fatalf("expected local_continent reason, got %s", result.Reason)
	}
}

func TestRoute_PreferredContinent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinHealthyRegionsPerContinent = 1
	tm := NewTopologyManager(cfg)
	tm.now = fixedTime

	// Register EU and Asia (but NA is unhealthy)
	eu := &domain.ContinentMesh{
		Continent: domain.ContinentEurope,
		Gateway:   "eu-west-1",
		Regions:   []domain.PlanetaryRegion{{Region: "eu-west-1", Healthy: true, NodeCount: 300000}},
		Links:     []domain.ContinentLink{{From: domain.ContinentNorthAmerica, To: domain.ContinentEurope, LatencyMs: 80}},
	}
	as := &domain.ContinentMesh{
		Continent: domain.ContinentAsia,
		Gateway:   "ap-east-1",
		Regions:   []domain.PlanetaryRegion{{Region: "ap-east-1", Healthy: true, NodeCount: 200000}},
	}
	_ = tm.RegisterContinent(eu)
	_ = tm.RegisterContinent(as)

	// Route from NA, prefer EU
	result, err := tm.Route(domain.ContinentNorthAmerica, domain.ContinentEurope)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TargetContinent != domain.ContinentEurope {
		t.Fatalf("expected EU, got %s", result.TargetContinent)
	}
	if result.Reason != "preferred_continent" {
		t.Fatalf("expected preferred_continent, got %s", result.Reason)
	}
}

func TestRoute_ClosestHealthy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinHealthyRegionsPerContinent = 1
	tm := NewTopologyManager(cfg)
	tm.now = fixedTime

	// Register EU (close) and Asia (far) — source NA not registered
	eu := &domain.ContinentMesh{
		Continent: domain.ContinentEurope,
		Gateway:   "eu-west-1",
		Regions:   []domain.PlanetaryRegion{{Region: "eu-west-1", Healthy: true, NodeCount: 300000}},
		Links:     []domain.ContinentLink{{From: domain.ContinentNorthAmerica, To: domain.ContinentEurope, LatencyMs: 80}},
	}
	as := &domain.ContinentMesh{
		Continent: domain.ContinentAsia,
		Gateway:   "ap-east-1",
		Regions:   []domain.PlanetaryRegion{{Region: "ap-east-1", Healthy: true, NodeCount: 200000}},
		Links:     []domain.ContinentLink{{From: domain.ContinentNorthAmerica, To: domain.ContinentAsia, LatencyMs: 150}},
	}
	_ = tm.RegisterContinent(eu)
	_ = tm.RegisterContinent(as)

	result, err := tm.Route(domain.ContinentNorthAmerica, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TargetContinent != domain.ContinentEurope {
		t.Fatalf("expected EU (closest), got %s", result.TargetContinent)
	}
	if result.Reason != "closest_healthy" {
		t.Fatalf("expected closest_healthy, got %s", result.Reason)
	}
}

func TestRoute_NoContinentsAvailable(t *testing.T) {
	tm := NewTopologyManager(DefaultConfig())
	tm.now = fixedTime

	_, err := tm.Route(domain.ContinentNorthAmerica, "")
	if err != domain.ErrContinentUnavailable {
		t.Fatalf("expected ErrContinentUnavailable, got %v", err)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Distribution Tracker Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestDistributionTracker_RecordAndQuery(t *testing.T) {
	dt := NewDistributionTracker()

	dt.RecordDistribution("llama-70b", domain.ContinentNorthAmerica, 0.85, 50_000_000_000)
	dt.RecordDistribution("llama-70b", domain.ContinentEurope, 0.72, 40_000_000_000)

	cov := dt.ModelCoverage("llama-70b", domain.ContinentNorthAmerica)
	if cov != 0.85 {
		t.Fatalf("expected 0.85 coverage, got %f", cov)
	}

	cov = dt.ModelCoverage("unknown-model", domain.ContinentNorthAmerica)
	if cov != 0 {
		t.Fatalf("expected 0 for unknown model, got %f", cov)
	}
}

func TestDistributionTracker_Stats(t *testing.T) {
	dt := NewDistributionTracker()

	dt.RecordDistribution("llama-70b", domain.ContinentNorthAmerica, 0.85, 50_000_000_000)
	dt.RecordDistribution("llama-7b", domain.ContinentNorthAmerica, 0.95, 5_000_000_000)
	dt.SetP2PShareRatio(0.7)

	stats := dt.Stats()
	if stats.TotalModelsDistributed != 2 {
		t.Fatalf("expected 2 models, got %d", stats.TotalModelsDistributed)
	}
	if stats.TotalBytesDistributed != 55_000_000_000 {
		t.Fatalf("expected 55B bytes, got %d", stats.TotalBytesDistributed)
	}
	if stats.P2PShareRatio != 0.7 {
		t.Fatalf("expected 0.7 P2P ratio, got %f", stats.P2PShareRatio)
	}
	// CDN savings = 0.7 * 0.80 * 100 ≈ 56% (use epsilon for floats)
	if stats.CDNCostSavings < 55.9 || stats.CDNCostSavings > 56.1 {
		t.Fatalf("expected ~56.0%% CDN savings, got %f", stats.CDNCostSavings)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Gate Check Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestGateCheck(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinHealthyRegionsPerContinent = 1
	tm := NewTopologyManager(cfg)
	tm.now = fixedTime

	// Register 3 healthy continents
	for _, c := range []domain.ContinentID{domain.ContinentNorthAmerica, domain.ContinentEurope, domain.ContinentAsia} {
		mesh := &domain.ContinentMesh{
			Continent: c,
			Gateway:   domain.RegionID("gw-" + string(c)),
			Regions: []domain.PlanetaryRegion{
				{Region: domain.RegionID("r1-" + string(c)), Healthy: true, NodeCount: 3_000_000},
				{Region: domain.RegionID("r2-" + string(c)), Healthy: true, NodeCount: 1_000_000},
			},
		}
		_ = tm.RegisterContinent(mesh)
	}

	nodes, regions, healthy := tm.GateCheck()
	if nodes != 12_000_000 {
		t.Fatalf("expected 12M nodes, got %d", nodes)
	}
	if regions != 6 {
		t.Fatalf("expected 6 regions, got %d", regions)
	}
	if healthy != 3 {
		t.Fatalf("expected 3 healthy continents, got %d", healthy)
	}
}
