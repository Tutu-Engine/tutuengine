// Package region implements multi-region task routing with geo-aware assignment.
// Architecture Part IX (Advanced Scheduling) + Part XXI (Multi-Region Deployment).
//
// Routing priority:
//  1. Data residency constraint (hard requirement — if set, must be honored)
//  2. Same-region preference (lowest latency)
//  3. Lowest-load failover (if home region is overloaded)
//  4. Cross-region with latency penalty
package region

import (
	"sort"
	"sync"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ─── Router ─────────────────────────────────────────────────────────────────

// Router makes geo-aware task routing decisions across regions.
// It maintains a snapshot of each region's health and routes tasks
// to minimize latency while respecting data-residency requirements.
type Router struct {
	mu       sync.RWMutex
	regions  map[domain.RegionID]*domain.RegionStatus
	localReg domain.RegionID

	// Configuration thresholds
	loadThreshold float64 // above this, prefer cross-region routing
	maxLatencyMs  int     // reject routes above this latency
}

// Config holds router configuration.
type Config struct {
	LocalRegion   domain.RegionID
	LoadThreshold float64 // default 0.8 — route away if load > 80%
	MaxLatencyMs  int     // default 200ms — reject routes with higher penalty
}

// DefaultConfig returns sensible router defaults.
func DefaultConfig() Config {
	return Config{
		LocalRegion:   domain.RegionUSEast,
		LoadThreshold: 0.8,
		MaxLatencyMs:  200,
	}
}

// NewRouter creates a multi-region router.
func NewRouter(cfg Config) *Router {
	if cfg.LoadThreshold <= 0 {
		cfg.LoadThreshold = 0.8
	}
	if cfg.MaxLatencyMs <= 0 {
		cfg.MaxLatencyMs = 200
	}

	r := &Router{
		regions:       make(map[domain.RegionID]*domain.RegionStatus),
		localReg:      cfg.LocalRegion,
		loadThreshold: cfg.LoadThreshold,
		maxLatencyMs:  cfg.MaxLatencyMs,
	}

	// Initialize all known regions as healthy with zero load.
	for _, reg := range domain.AllRegions() {
		r.regions[reg] = &domain.RegionStatus{
			Region:    reg,
			Healthy:   true,
			UpdatedAt: time.Now(),
		}
	}

	return r
}

// ─── Status Updates ─────────────────────────────────────────────────────────

// UpdateRegion applies a fresh status snapshot for a region.
func (r *Router) UpdateRegion(status domain.RegionStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := status // copy
	r.regions[status.Region] = &s
}

// RegionStatus returns the current status of a specific region.
func (r *Router) RegionStatus(id domain.RegionID) (domain.RegionStatus, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if s, ok := r.regions[id]; ok {
		return *s, true
	}
	return domain.RegionStatus{}, false
}

// AllRegionStatuses returns a snapshot of all region statuses.
func (r *Router) AllRegionStatuses() []domain.RegionStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.RegionStatus, 0, len(r.regions))
	for _, s := range r.regions {
		out = append(out, *s)
	}
	return out
}

// ─── Routing ────────────────────────────────────────────────────────────────

// Route determines the best region for a task, returning a RouteDecision.
//
// Algorithm (in priority order):
//  1. If DataResidency is set → must use that region (hard constraint).
//  2. If preferred region is healthy and below load threshold → use it.
//  3. Otherwise, pick the healthy region with lowest load + latency score.
//  4. If no healthy region exists → fallback to local region.
func (r *Router) Route(routing domain.TaskRouting) domain.RouteDecision {
	r.mu.RLock()
	defer r.mu.RUnlock()

	source := r.localReg

	// Priority 1: Data residency hard constraint
	if routing.RequiresRegion() {
		target := routing.DataResidency
		return domain.RouteDecision{
			TargetRegion:   target,
			SourceRegion:   source,
			LatencyPenalty: domain.RegionLatencyMs(source, target),
			Reason:         "data-residency",
		}
	}

	// Priority 2: Preferred region (if healthy + not overloaded)
	preferred := routing.PreferredRegion()
	if preferred != "" {
		if s, ok := r.regions[preferred]; ok && s.Healthy && s.Load() < r.loadThreshold {
			return domain.RouteDecision{
				TargetRegion:   preferred,
				SourceRegion:   source,
				LatencyPenalty: domain.RegionLatencyMs(source, preferred),
				Reason:         "preferred-region",
			}
		}
	}

	// Priority 3: Same region if healthy and below threshold
	if s, ok := r.regions[source]; ok && s.Healthy && s.Load() < r.loadThreshold {
		return domain.RouteDecision{
			TargetRegion:   source,
			SourceRegion:   source,
			LatencyPenalty: 0,
			Reason:         "same-region",
		}
	}

	// Priority 4: Find best alternative — scored by (low load + low latency)
	type candidate struct {
		region  domain.RegionID
		score   float64
		latency int
	}

	candidates := make([]candidate, 0, len(r.regions))
	for id, s := range r.regions {
		if !s.Healthy {
			continue
		}
		latency := domain.RegionLatencyMs(source, id)
		if latency > r.maxLatencyMs {
			continue
		}
		// Score: lower is better. Weighted: 70% load + 30% latency-normalized.
		loadScore := s.Load()
		latencyScore := float64(latency) / float64(r.maxLatencyMs)
		score := 0.7*loadScore + 0.3*latencyScore
		candidates = append(candidates, candidate{region: id, score: score, latency: latency})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score < candidates[j].score
	})

	if len(candidates) > 0 {
		best := candidates[0]
		return domain.RouteDecision{
			TargetRegion:   best.region,
			SourceRegion:   source,
			LatencyPenalty: best.latency,
			Reason:         "lowest-load",
		}
	}

	// Fallback: local region regardless of health (best effort)
	return domain.RouteDecision{
		TargetRegion:   source,
		SourceRegion:   source,
		LatencyPenalty: 0,
		Reason:         "fallback",
	}
}

// HealthyRegionCount returns how many regions are currently marked healthy.
func (r *Router) HealthyRegionCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, s := range r.regions {
		if s.Healthy {
			count++
		}
	}
	return count
}
