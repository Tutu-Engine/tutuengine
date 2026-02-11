// Package domain — Phase 3 multi-region types.
// Defines regions, cross-region routing, and geo-aware task assignment.
// Architecture Part IX (Advanced Scheduling) + Part XXI (Multi-Region Deployment).
package domain

import "time"

// ─── Region Types ───────────────────────────────────────────────────────────

// RegionID uniquely identifies a deployment region.
type RegionID string

const (
	RegionUSEast  RegionID = "us-east"
	RegionEUWest  RegionID = "eu-west"
	RegionAPSouth RegionID = "ap-south"
)

// AllRegions returns all supported deployment regions.
func AllRegions() []RegionID {
	return []RegionID{RegionUSEast, RegionEUWest, RegionAPSouth}
}

// IsValid reports whether r is a recognized region.
func (r RegionID) IsValid() bool {
	switch r {
	case RegionUSEast, RegionEUWest, RegionAPSouth:
		return true
	}
	return false
}

// String returns the region as a human-readable string.
func (r RegionID) String() string { return string(r) }

// ─── Cross-Region Latency Map ───────────────────────────────────────────────
// Known inter-region latencies in milliseconds.
// Used by the scheduler to add latency penalties for cross-region routing.

// RegionLatencyMs returns the approximate round-trip latency between two regions.
// Same-region returns 0. Unknown pairs return a high default.
func RegionLatencyMs(from, to RegionID) int {
	if from == to {
		return 0
	}
	key := regionPairKey(from, to)
	if lat, ok := crossRegionLatency[key]; ok {
		return lat
	}
	return 200 // conservative default for unknown pairs
}

// regionPairKey normalizes pair ordering so (a,b) == (b,a).
func regionPairKey(a, b RegionID) string {
	if a > b {
		a, b = b, a
	}
	return string(a) + ":" + string(b)
}

var crossRegionLatency = map[string]int{
	regionPairKey(RegionUSEast, RegionEUWest):   85,  // transatlantic
	regionPairKey(RegionUSEast, RegionAPSouth):   180, // US to Asia
	regionPairKey(RegionEUWest, RegionAPSouth):   120, // Europe to Asia
}

// ─── Region Status ──────────────────────────────────────────────────────────

// RegionStatus is a snapshot of a region's operational health and capacity.
type RegionStatus struct {
	Region       RegionID  `json:"region"`
	Healthy      bool      `json:"healthy"`
	NodeCount    int       `json:"node_count"`
	ActiveTasks  int       `json:"active_tasks"`
	QueueDepth   int       `json:"queue_depth"`
	AvgLatencyMs float64   `json:"avg_latency_ms"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Load returns the region's load factor (0.0 = idle, 1.0+ = overloaded).
func (rs RegionStatus) Load() float64 {
	if rs.NodeCount == 0 {
		return 1.0 // no nodes = infinitely loaded
	}
	return float64(rs.ActiveTasks) / float64(rs.NodeCount)
}

// ─── Routing Decision ───────────────────────────────────────────────────────

// RouteDecision captures where and why a task was routed.
type RouteDecision struct {
	TargetRegion   RegionID `json:"target_region"`
	SourceRegion   RegionID `json:"source_region"`
	LatencyPenalty int      `json:"latency_penalty_ms"`
	Reason         string   `json:"reason"` // "same-region", "lowest-load", "data-residency", "failover"
}

// ─── Task Routing Extension ─────────────────────────────────────────────────

// TaskRouting extends the Phase 1 Task with multi-region routing metadata.
type TaskRouting struct {
	RegionAffinity []RegionID `json:"region_affinity,omitempty"`
	DataResidency  RegionID   `json:"data_residency,omitempty"` // required jurisdiction
	NodeWhitelist  []string   `json:"node_whitelist,omitempty"`
	NodeBlacklist  []string   `json:"node_blacklist,omitempty"`
}

// PreferredRegion returns the highest-priority region affinity, or empty.
func (tr TaskRouting) PreferredRegion() RegionID {
	if len(tr.RegionAffinity) > 0 {
		return tr.RegionAffinity[0]
	}
	return ""
}

// RequiresRegion returns true if data residency restricts region placement.
func (tr TaskRouting) RequiresRegion() bool {
	return tr.DataResidency != ""
}
