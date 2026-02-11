// Package flywheel implements Phase 7 economic flywheel tracking.
//
// The flywheel is the self-reinforcing cycle that makes TuTu self-sustaining:
//
//	Users contribute idle compute → earn credits → spend on AI
//	→ more demand → more contributors → network effects → stronger network
//
// This package monitors the health of this cycle and raises alarms when the
// economy becomes unsustainable. It does NOT set economic parameters (that's
// governance's job) — it only measures and reports.
//
// Key metrics tracked:
//   - Supply vs demand ratio
//   - Viral coefficient (>1 = organic growth, <1 = needs intervention)
//   - Enterprise revenue (funds the free tier)
//   - Network effect index (composite health score 0-100)
//
// Architecture Reference: Phase 7 Gate Check "Network is self-sustaining economically".
package flywheel

import (
	"math"
	"sync"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ═══════════════════════════════════════════════════════════════════════════
// Configuration
// ═══════════════════════════════════════════════════════════════════════════

// Config controls the flywheel health tracker.
type Config struct {
	// SnapshotInterval controls how often snapshots are taken.
	SnapshotInterval time.Duration

	// MaxSnapshots is the ring buffer size for historical data.
	MaxSnapshots int

	// SustainabilityThreshold: minimum NetworkEffectIndex for "healthy".
	SustainabilityThreshold float64

	// MinViralCoefficient: below this, growth is considered stalled.
	MinViralCoefficient float64

	// MinSupplyDemandRatio: below this, there's a contribution deficit.
	MinSupplyDemandRatio float64
}

// DefaultConfig returns sensible defaults for flywheel tracking.
func DefaultConfig() Config {
	return Config{
		SnapshotInterval:        1 * time.Hour,
		MaxSnapshots:            168, // 7 days of hourly snapshots
		SustainabilityThreshold: 50.0,
		MinViralCoefficient:     0.8,
		MinSupplyDemandRatio:    0.8,
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Flywheel Tracker — the core engine
// ═══════════════════════════════════════════════════════════════════════════

// Tracker monitors the health of the economic flywheel.
// It collects metrics from other subsystems and computes composite health.
type Tracker struct {
	mu     sync.RWMutex
	config Config

	// Current health state
	current domain.FlywheelHealth

	// Historical snapshots (ring buffer)
	snapshots []domain.FlywheelSnapshot
	snapIdx   int
	snapFull  bool

	// Counters for computing growth rates
	prevWeekNodes      int64
	prevWeekInferences int64

	// Injectable clock
	now func() time.Time
}

// NewTracker creates a Tracker with the given configuration.
func NewTracker(cfg Config) *Tracker {
	return &Tracker{
		config:    cfg,
		snapshots: make([]domain.FlywheelSnapshot, cfg.MaxSnapshots),
		now:       time.Now,
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Health Updates — called by other subsystems
// ═══════════════════════════════════════════════════════════════════════════

// UpdateSupply records supply-side metrics (contributors).
func (t *Tracker) UpdateSupply(total, active int64, avgHours, totalComputeHours float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.current.TotalContributors = total
	t.current.ActiveContributors = active
	t.current.AvgContributionHours = avgHours
	t.current.TotalComputeHours = totalComputeHours

	// Compute weekly growth rate
	if t.prevWeekNodes > 0 {
		t.current.SupplyGrowthRate = float64(total-t.prevWeekNodes) / float64(t.prevWeekNodes) * 100
	}
}

// UpdateDemand records demand-side metrics (consumers).
func (t *Tracker) UpdateDemand(total, active, inferencesPerDay int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.current.TotalConsumers = total
	t.current.ActiveConsumers = active
	t.current.InferencesPerDay = inferencesPerDay

	// Compute weekly growth rate
	if t.prevWeekInferences > 0 {
		t.current.DemandGrowthRate = float64(inferencesPerDay-t.prevWeekInferences) / float64(t.prevWeekInferences) * 100
	}
}

// UpdateEconomy records economic balance metrics.
func (t *Tracker) UpdateEconomy(circulating, earnedToday, spentToday, enterpriseRevenue int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.current.CreditsInCirculation = circulating
	t.current.CreditsEarnedToday = earnedToday
	t.current.CreditsSpentToday = spentToday
	t.current.EnterpriseRevenue = enterpriseRevenue

	// Supply/demand ratio: earned vs spent
	if spentToday > 0 {
		t.current.SupplyDemandRatio = float64(earnedToday) / float64(spentToday)
	} else if earnedToday > 0 {
		t.current.SupplyDemandRatio = 2.0 // Surplus (max 2.0)
	} else {
		t.current.SupplyDemandRatio = 1.0 // No activity
	}
}

// UpdateRetention records user retention metrics.
func (t *Tracker) UpdateRetention(rate7d, rate30d float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.current.RetentionRate7d = rate7d
	t.current.RetentionRate30d = rate30d
}

// UpdateViralCoefficient records the viral growth metric.
// viralK = (invites_sent * conversion_rate) per user.
// >1.0 = each user brings >1 new user = organic growth.
func (t *Tracker) UpdateViralCoefficient(viralK float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.current.ViralCoefficient = viralK
}

// ═══════════════════════════════════════════════════════════════════════════
// Health Assessment
// ═══════════════════════════════════════════════════════════════════════════

// Health returns the current flywheel health snapshot.
// The NetworkEffectIndex is computed as a weighted composite:
//
//	0.25 × supply_demand_balance (0-100)
//	0.25 × viral_coefficient_score (0-100)
//	0.20 × retention_score (0-100)
//	0.15 × revenue_score (0-100)
//	0.15 × growth_score (0-100)
func (t *Tracker) Health() domain.FlywheelHealth {
	t.mu.Lock()
	defer t.mu.Unlock()

	h := t.current
	h.NetworkEffectIndex = t.computeNetworkEffectIndex()
	h.MeasuredAt = t.now()
	return h
}

// IsSustainable reports whether the economy is self-sustaining.
func (t *Tracker) IsSustainable() bool {
	return t.Health().IsSustainable()
}

// TakeSnapshot records the current state for historical tracking.
func (t *Tracker) TakeSnapshot() {
	t.mu.Lock()
	defer t.mu.Unlock()

	snap := domain.FlywheelSnapshot{
		Timestamp:   t.now(),
		Nodes:       t.current.TotalContributors,
		Inferences:  t.current.InferencesPerDay,
		Credits:     t.current.CreditsInCirculation,
		Revenue:     t.current.EnterpriseRevenue,
		HealthIndex: t.computeNetworkEffectIndex(),
	}

	t.snapshots[t.snapIdx] = snap
	t.snapIdx = (t.snapIdx + 1) % t.config.MaxSnapshots
	if t.snapIdx == 0 {
		t.snapFull = true
	}
}

// Snapshots returns all recorded snapshots in chronological order.
func (t *Tracker) Snapshots() []domain.FlywheelSnapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if !t.snapFull {
		result := make([]domain.FlywheelSnapshot, t.snapIdx)
		copy(result, t.snapshots[:t.snapIdx])
		return result
	}

	result := make([]domain.FlywheelSnapshot, t.config.MaxSnapshots)
	copy(result, t.snapshots[t.snapIdx:])
	copy(result[t.config.MaxSnapshots-t.snapIdx:], t.snapshots[:t.snapIdx])
	return result
}

// MarkWeekBoundary saves current values for computing weekly growth rates.
// Call this once per week (e.g., every Monday at midnight UTC).
func (t *Tracker) MarkWeekBoundary() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.prevWeekNodes = t.current.TotalContributors
	t.prevWeekInferences = t.current.InferencesPerDay
}

// ═══════════════════════════════════════════════════════════════════════════
// Network Effect Index Computation
// ═══════════════════════════════════════════════════════════════════════════

// computeNetworkEffectIndex calculates the composite health score (0-100).
// Must be called with lock held.
func (t *Tracker) computeNetworkEffectIndex() float64 {
	// Supply/demand balance: 1.0 = perfect, score tapers off
	sdScore := clamp(t.current.SupplyDemandRatio/1.5*100, 0, 100)

	// Viral coefficient: 1.0 = breakeven growth, 2.0 = perfect
	viralScore := clamp(t.current.ViralCoefficient/2.0*100, 0, 100)

	// Retention: 30-day retention as a percentage (directly maps to 0-100)
	retentionScore := clamp(t.current.RetentionRate30d, 0, 100)

	// Revenue: log-scaled (revenue of 1M credits = 100%)
	revenueScore := 0.0
	if t.current.EnterpriseRevenue > 0 {
		revenueScore = clamp(math.Log10(float64(t.current.EnterpriseRevenue))/6.0*100, 0, 100)
	}

	// Growth: average of supply and demand growth rates (10% = perfect)
	growthAvg := (t.current.SupplyGrowthRate + t.current.DemandGrowthRate) / 2.0
	growthScore := clamp(growthAvg/10.0*100, 0, 100)

	// Weighted composite
	index := 0.25*sdScore +
		0.25*viralScore +
		0.20*retentionScore +
		0.15*revenueScore +
		0.15*growthScore

	return math.Round(index*100) / 100 // 2 decimal places
}

// clamp restricts v between lo and hi.
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ═══════════════════════════════════════════════════════════════════════════
// Gate Check Support
// ═══════════════════════════════════════════════════════════════════════════

// GateCheck reports whether the economic flywheel meets Phase 7 targets.
func (t *Tracker) GateCheck() (sustainable bool, networkEffectIndex float64, viralK float64) {
	h := t.Health()
	return h.IsSustainable(), h.NetworkEffectIndex, h.ViralCoefficient
}
