// Package planetary implements Phase 7 planetary-scale infrastructure.
//
// This is the system that turns TuTu from a multi-region network (Phase 3, 3 regions)
// into a truly global supercomputer spanning 50+ regions across all continents.
//
// Key concepts:
//   - ContinentMesh: Each continent has a "gateway" region + multiple sub-regions
//   - Hierarchical routing: Node → Zone → Region → Continent → Global
//   - Sub-10ms routing: requests always hit the nearest continent gateway first
//   - Exabyte-scale: track model distribution across millions of nodes
//
// Architecture Reference: Part I (Vision), Part XXII (Final Vision), Phase 7 Gate Checks.
package planetary

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ═══════════════════════════════════════════════════════════════════════════
// Configuration
// ═══════════════════════════════════════════════════════════════════════════

// Config controls the planetary topology manager behavior.
type Config struct {
	// MinHealthyRegionsPerContinent is the minimum to consider a continent up.
	MinHealthyRegionsPerContinent int

	// MaxRoutingHops prevents routing loops in the continent mesh.
	MaxRoutingHops int

	// HealthCheckInterval controls how often continent health is re-evaluated.
	HealthCheckInterval time.Duration

	// MinQuorumContinents is the number of healthy continents needed for quorum.
	MinQuorumContinents int

	// GatewaySelectionStrategy controls how gateway regions are chosen:
	// "lowest-latency" or "highest-capacity"
	GatewaySelectionStrategy string
}

// DefaultConfig returns sensible defaults for planetary infrastructure.
func DefaultConfig() Config {
	return Config{
		MinHealthyRegionsPerContinent: 2,
		MaxRoutingHops:                3,
		HealthCheckInterval:           30 * time.Second,
		MinQuorumContinents:           4, // Majority of 6 continents
		GatewaySelectionStrategy:      "lowest-latency",
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Topology Manager — the brain of planetary routing
// ═══════════════════════════════════════════════════════════════════════════

// TopologyManager maintains the global view of the planetary network.
// It is the single source of truth for continent health, region status,
// routing decisions, and model distribution tracking.
type TopologyManager struct {
	mu     sync.RWMutex
	config Config

	// Continent meshes — one per continent
	continents map[domain.ContinentID]*domain.ContinentMesh

	// Inter-continent latency matrix (learned from heartbeats)
	linkLatencies map[string]int // key: "na:eu", value: ms

	// Model distribution tracking
	modelDistribution *DistributionTracker

	// Injectable clock for testing
	now func() time.Time
}

// NewTopologyManager creates a TopologyManager with the given configuration.
// Call RegisterContinent() to add continents, then use Route() for routing.
func NewTopologyManager(cfg Config) *TopologyManager {
	return &TopologyManager{
		config:            cfg,
		continents:        make(map[domain.ContinentID]*domain.ContinentMesh),
		linkLatencies:     make(map[string]int),
		modelDistribution: NewDistributionTracker(),
		now:               time.Now,
	}
}

// RegisterContinent adds or updates a continent in the topology.
// This is called by the gossip protocol when new continent data arrives.
func (tm *TopologyManager) RegisterContinent(mesh *domain.ContinentMesh) error {
	if !mesh.Continent.IsValid() {
		return fmt.Errorf("invalid continent: %q", mesh.Continent)
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	mesh.UpdatedAt = tm.now()
	tm.continents[mesh.Continent] = mesh

	// Update inter-continent links
	for _, link := range mesh.Links {
		key := linkKey(link.From, link.To)
		tm.linkLatencies[key] = link.LatencyMs
	}

	return nil
}

// RemoveContinent removes a continent from the topology.
func (tm *TopologyManager) RemoveContinent(id domain.ContinentID) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	delete(tm.continents, id)
}

// Topology returns the full global topology snapshot.
func (tm *TopologyManager) Topology() domain.PlanetaryTopology {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	topo := domain.PlanetaryTopology{
		Continents: make(map[domain.ContinentID]*domain.ContinentMesh),
		UpdatedAt:  tm.now(),
	}

	for id, mesh := range tm.continents {
		topo.Continents[id] = mesh
		topo.TotalNodes += mesh.TotalNodes()
		topo.TotalRegions += len(mesh.Regions)
	}

	// Global health = weighted average of continent health
	if len(tm.continents) > 0 {
		healthyCount := 0
		for _, mesh := range tm.continents {
			if mesh.HealthyRegionCount() >= tm.config.MinHealthyRegionsPerContinent {
				healthyCount++
			}
		}
		topo.GlobalHealth = float64(healthyCount) / float64(len(tm.continents))
	}

	return topo
}

// ContinentCount returns the number of registered continents.
func (tm *TopologyManager) ContinentCount() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return len(tm.continents)
}

// TotalNodes returns the global node count across all continents.
func (tm *TopologyManager) TotalNodes() int64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var total int64
	for _, mesh := range tm.continents {
		total += mesh.TotalNodes()
	}
	return total
}

// IsQuorumHealthy checks whether enough continents are healthy for operation.
func (tm *TopologyManager) IsQuorumHealthy() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	healthy := 0
	for _, mesh := range tm.continents {
		if mesh.HealthyRegionCount() >= tm.config.MinHealthyRegionsPerContinent {
			healthy++
		}
	}
	return healthy >= tm.config.MinQuorumContinents
}

// ═══════════════════════════════════════════════════════════════════════════
// Routing — sub-10ms decision engine
// ═══════════════════════════════════════════════════════════════════════════

// RouteResult describes where a request should be routed.
type RouteResult struct {
	TargetContinent domain.ContinentID `json:"target_continent"`
	TargetRegion    domain.RegionID    `json:"target_region"`
	LatencyMs       int                `json:"latency_ms"`
	Hops            int                `json:"hops"`
	Reason          string             `json:"reason"`
}

// Route decides the best continent and region for a request.
// Algorithm:
//  1. If source continent is healthy → route locally (0 hops, lowest latency)
//  2. If preferred continent specified → route there if healthy
//  3. Otherwise → pick closest healthy continent by latency
//  4. Within chosen continent → pick region with lowest load
func (tm *TopologyManager) Route(sourceContinent domain.ContinentID, preferred domain.ContinentID) (RouteResult, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// Try source continent first (fastest path)
	if mesh, ok := tm.continents[sourceContinent]; ok {
		if mesh.HealthyRegionCount() >= tm.config.MinHealthyRegionsPerContinent {
			region := tm.bestRegionInMesh(mesh)
			return RouteResult{
				TargetContinent: sourceContinent,
				TargetRegion:    region,
				LatencyMs:       0, // Same continent
				Hops:            0,
				Reason:          "local_continent",
			}, nil
		}
	}

	// Try preferred continent if given
	if preferred.IsValid() {
		if mesh, ok := tm.continents[preferred]; ok {
			if mesh.HealthyRegionCount() >= tm.config.MinHealthyRegionsPerContinent {
				latency := tm.interContinentLatency(sourceContinent, preferred)
				region := tm.bestRegionInMesh(mesh)
				return RouteResult{
					TargetContinent: preferred,
					TargetRegion:    region,
					LatencyMs:       latency,
					Hops:            1,
					Reason:          "preferred_continent",
				}, nil
			}
		}
	}

	// Find closest healthy continent
	type candidate struct {
		continent domain.ContinentID
		mesh      *domain.ContinentMesh
		latency   int
	}

	var candidates []candidate
	for id, mesh := range tm.continents {
		if id == sourceContinent {
			continue // Already tried
		}
		if mesh.HealthyRegionCount() >= tm.config.MinHealthyRegionsPerContinent {
			latency := tm.interContinentLatency(sourceContinent, id)
			candidates = append(candidates, candidate{id, mesh, latency})
		}
	}

	if len(candidates) == 0 {
		return RouteResult{}, domain.ErrContinentUnavailable
	}

	// Sort by latency (closest first)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].latency < candidates[j].latency
	})

	best := candidates[0]
	region := tm.bestRegionInMesh(best.mesh)

	return RouteResult{
		TargetContinent: best.continent,
		TargetRegion:    region,
		LatencyMs:       best.latency,
		Hops:            1,
		Reason:          "closest_healthy",
	}, nil
}

// bestRegionInMesh returns the healthiest, lowest-load region in a mesh.
func (tm *TopologyManager) bestRegionInMesh(mesh *domain.ContinentMesh) domain.RegionID {
	if len(mesh.Regions) == 0 {
		return mesh.Gateway
	}

	best := mesh.Regions[0]
	bestScore := math.MaxFloat64

	for _, r := range mesh.Regions {
		if !r.Healthy {
			continue
		}
		// Score: lower is better (latency + load penalty)
		score := r.LatencyMs + r.Load(0)*100
		if score < bestScore {
			bestScore = score
			best = r
		}
	}

	return best.Region
}

// interContinentLatency returns the known latency between two continents.
func (tm *TopologyManager) interContinentLatency(from, to domain.ContinentID) int {
	key := linkKey(from, to)
	if lat, ok := tm.linkLatencies[key]; ok {
		return lat
	}
	return 200 // Conservative default for unknown paths
}

// linkKey normalizes continent pair ordering so (a,b) == (b,a).
func linkKey(a, b domain.ContinentID) string {
	if a > b {
		a, b = b, a
	}
	return string(a) + ":" + string(b)
}

// ═══════════════════════════════════════════════════════════════════════════
// Distribution Tracker — exabyte-scale model distribution
// ═══════════════════════════════════════════════════════════════════════════

// DistributionTracker monitors model distribution across the global network.
// It tracks which models are cached on which continents and regions,
// enabling the P2P distribution system (Phase 4) to work at planetary scale.
type DistributionTracker struct {
	mu sync.RWMutex

	// modelCoverage: model_name → continent → ratio of nodes with model cached
	modelCoverage map[string]map[domain.ContinentID]float64

	// global statistics
	totalBytesDistributed int64
	totalModels           int64
	p2pShareRatio         float64
}

// NewDistributionTracker creates a fresh distribution tracker.
func NewDistributionTracker() *DistributionTracker {
	return &DistributionTracker{
		modelCoverage: make(map[string]map[domain.ContinentID]float64),
	}
}

// RecordDistribution updates the coverage data for a model on a continent.
func (dt *DistributionTracker) RecordDistribution(model string, continent domain.ContinentID, coverageRatio float64, bytesTransferred int64) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	if _, ok := dt.modelCoverage[model]; !ok {
		dt.modelCoverage[model] = make(map[domain.ContinentID]float64)
		dt.totalModels++
	}
	dt.modelCoverage[model][continent] = coverageRatio
	dt.totalBytesDistributed += bytesTransferred
}

// SetP2PShareRatio updates the global P2P share ratio metric.
func (dt *DistributionTracker) SetP2PShareRatio(ratio float64) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.p2pShareRatio = ratio
}

// ModelCoverage returns the coverage ratio for a model on a specific continent.
func (dt *DistributionTracker) ModelCoverage(model string, continent domain.ContinentID) float64 {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	if cov, ok := dt.modelCoverage[model]; ok {
		if ratio, ok := cov[continent]; ok {
			return ratio
		}
	}
	return 0
}

// Stats returns the global distribution statistics.
func (dt *DistributionTracker) Stats() domain.ModelDistributionStats {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	coverage := make(map[domain.ContinentID]float64)
	for _, cov := range dt.modelCoverage {
		for continent, ratio := range cov {
			coverage[continent] += ratio
		}
	}

	// Average coverage per continent
	if dt.totalModels > 0 {
		for continent := range coverage {
			coverage[continent] /= float64(dt.totalModels)
		}
	}

	savingsRatio := dt.p2pShareRatio * 0.80 // Each P2P download saves ~80% CDN cost

	return domain.ModelDistributionStats{
		TotalModelsDistributed: dt.totalModels,
		TotalBytesDistributed:  dt.totalBytesDistributed,
		P2PShareRatio:          dt.p2pShareRatio,
		CDNCostSavings:         savingsRatio * 100,
		ContinentCoverage:      coverage,
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Gate Check Support
// ═══════════════════════════════════════════════════════════════════════════

// GateCheck reports whether the planetary infrastructure meets Phase 7 targets.
func (tm *TopologyManager) GateCheck() (totalNodes int64, totalRegions int, continentsHealthy int) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, mesh := range tm.continents {
		totalNodes += mesh.TotalNodes()
		totalRegions += len(mesh.Regions)
		if mesh.HealthyRegionCount() >= tm.config.MinHealthyRegionsPerContinent {
			continentsHealthy++
		}
	}
	return
}
