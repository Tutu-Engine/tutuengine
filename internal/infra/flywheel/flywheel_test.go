package flywheel

import (
	"testing"
	"time"
)

// fixedTime returns a deterministic time for testing.
func fixedTime() time.Time {
	return time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC)
}

// ═══════════════════════════════════════════════════════════════════════════
// Tracker Construction
// ═══════════════════════════════════════════════════════════════════════════

func TestNewTracker(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	if tr == nil {
		t.Fatal("expected non-nil Tracker")
	}

	h := tr.Health()
	if h.NetworkEffectIndex != 0 {
		t.Fatalf("expected 0 health index initially, got %f", h.NetworkEffectIndex)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Supply/Demand Updates
// ═══════════════════════════════════════════════════════════════════════════

func TestUpdateSupply(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	tr.now = fixedTime

	tr.UpdateSupply(1_000_000, 500_000, 4.5, 2_250_000)

	h := tr.Health()
	if h.TotalContributors != 1_000_000 {
		t.Fatalf("expected 1M contributors, got %d", h.TotalContributors)
	}
	if h.ActiveContributors != 500_000 {
		t.Fatalf("expected 500K active, got %d", h.ActiveContributors)
	}
	if h.AvgContributionHours != 4.5 {
		t.Fatalf("expected 4.5 avg hours, got %f", h.AvgContributionHours)
	}
}

func TestUpdateDemand(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	tr.now = fixedTime

	tr.UpdateDemand(2_000_000, 800_000, 50_000_000)

	h := tr.Health()
	if h.TotalConsumers != 2_000_000 {
		t.Fatalf("expected 2M consumers, got %d", h.TotalConsumers)
	}
	if h.InferencesPerDay != 50_000_000 {
		t.Fatalf("expected 50M inferences/day, got %d", h.InferencesPerDay)
	}
}

func TestUpdateEconomy(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	tr.now = fixedTime

	tr.UpdateEconomy(10_000_000, 500_000, 400_000, 100_000)

	h := tr.Health()
	if h.CreditsInCirculation != 10_000_000 {
		t.Fatalf("expected 10M credits, got %d", h.CreditsInCirculation)
	}
	// Supply/demand ratio: 500000/400000 = 1.25
	if h.SupplyDemandRatio != 1.25 {
		t.Fatalf("expected 1.25 ratio, got %f", h.SupplyDemandRatio)
	}
}

func TestUpdateEconomy_ZeroSpent(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	tr.now = fixedTime

	tr.UpdateEconomy(10_000_000, 500_000, 0, 100_000)

	h := tr.Health()
	if h.SupplyDemandRatio != 2.0 {
		t.Fatalf("expected 2.0 surplus ratio, got %f", h.SupplyDemandRatio)
	}
}

func TestUpdateRetention(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	tr.now = fixedTime

	tr.UpdateRetention(85.5, 72.3)

	h := tr.Health()
	if h.RetentionRate7d != 85.5 {
		t.Fatalf("expected 85.5 7d retention, got %f", h.RetentionRate7d)
	}
	if h.RetentionRate30d != 72.3 {
		t.Fatalf("expected 72.3 30d retention, got %f", h.RetentionRate30d)
	}
}

func TestUpdateViralCoefficient(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	tr.now = fixedTime

	tr.UpdateViralCoefficient(1.3)

	h := tr.Health()
	if h.ViralCoefficient != 1.3 {
		t.Fatalf("expected 1.3 viral k, got %f", h.ViralCoefficient)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Health Assessment
// ═══════════════════════════════════════════════════════════════════════════

func TestHealth_NetworkEffectIndex(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	tr.now = fixedTime

	// Set up a healthy economy
	tr.UpdateSupply(1_000_000, 500_000, 4.5, 2_250_000)
	tr.UpdateDemand(2_000_000, 800_000, 50_000_000)
	tr.UpdateEconomy(10_000_000, 500_000, 400_000, 100_000)
	tr.UpdateRetention(85.0, 72.0)
	tr.UpdateViralCoefficient(1.2)

	h := tr.Health()
	if h.NetworkEffectIndex <= 0 {
		t.Fatalf("expected positive network effect index, got %f", h.NetworkEffectIndex)
	}
	if h.NetworkEffectIndex > 100 {
		t.Fatalf("expected index <= 100, got %f", h.NetworkEffectIndex)
	}
}

func TestIsSustainable(t *testing.T) {
	tests := []struct {
		name       string
		sdRatio    float64
		revenue    int64
		viralK     float64
		retention  float64
		growthRate float64
		want       bool
	}{
		{
			name:       "healthy economy",
			sdRatio:    1.2,
			revenue:    100_000,
			viralK:     1.3,
			retention:  72.0,
			growthRate: 5.0,
			want:       true,
		},
		{
			name:       "no revenue",
			sdRatio:    1.2,
			revenue:    0,
			viralK:     1.3,
			retention:  72.0,
			growthRate: 5.0,
			want:       false,
		},
		{
			name:       "low supply demand ratio",
			sdRatio:    0.5,
			revenue:    100_000,
			viralK:     0.3,
			retention:  20.0,
			growthRate: -5.0,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewTracker(DefaultConfig())
			tr.now = fixedTime

			// Set up week boundary first so growth rate works
			tr.MarkWeekBoundary()
			earned := int64(tt.sdRatio * 400_000)
			tr.UpdateEconomy(10_000_000, earned, 400_000, tt.revenue)
			tr.UpdateViralCoefficient(tt.viralK)
			tr.UpdateRetention(85.0, tt.retention)

			// Set supply growth rate directly
			tr.mu.Lock()
			tr.current.SupplyGrowthRate = tt.growthRate
			tr.mu.Unlock()

			if got := tr.IsSustainable(); got != tt.want {
				t.Errorf("IsSustainable() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Snapshot Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestTakeSnapshot(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	tr.now = fixedTime

	tr.UpdateSupply(1000, 500, 4.0, 2000)
	tr.UpdateDemand(2000, 800, 50000)
	tr.TakeSnapshot()

	snaps := tr.Snapshots()
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	if snaps[0].Nodes != 1000 {
		t.Fatalf("expected 1000 nodes in snapshot, got %d", snaps[0].Nodes)
	}
	if snaps[0].Inferences != 50000 {
		t.Fatalf("expected 50000 inferences, got %d", snaps[0].Inferences)
	}
}

func TestSnapshot_RingBuffer(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxSnapshots = 3
	tr := NewTracker(cfg)
	tr.now = fixedTime

	// Take 5 snapshots (ring buffer holds 3)
	for i := int64(1); i <= 5; i++ {
		tr.UpdateSupply(i*100, i*50, 4.0, float64(i*200))
		tr.TakeSnapshot()
	}

	snaps := tr.Snapshots()
	if len(snaps) != 3 {
		t.Fatalf("expected 3 snapshots (ring buffer), got %d", len(snaps))
	}

	// The three remaining should be nodes 300, 400, 500
	if snaps[0].Nodes != 300 {
		t.Fatalf("expected oldest snapshot nodes=300, got %d", snaps[0].Nodes)
	}
	if snaps[2].Nodes != 500 {
		t.Fatalf("expected newest snapshot nodes=500, got %d", snaps[2].Nodes)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Week Boundary Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestMarkWeekBoundary_GrowthRate(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	tr.now = fixedTime

	// Week 1 baseline
	tr.UpdateSupply(1000, 500, 4.0, 2000)
	tr.UpdateDemand(2000, 800, 50000)
	tr.MarkWeekBoundary()

	// Week 2 — 10% growth
	tr.UpdateSupply(1100, 550, 4.0, 2200)
	tr.UpdateDemand(2200, 880, 55000)

	h := tr.Health()
	if h.SupplyGrowthRate != 10.0 {
		t.Fatalf("expected 10%% supply growth, got %f%%", h.SupplyGrowthRate)
	}
	if h.DemandGrowthRate != 10.0 {
		t.Fatalf("expected 10%% demand growth, got %f%%", h.DemandGrowthRate)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Gate Check Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestGateCheck(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	tr.now = fixedTime

	// Set up a sustainable economy
	tr.UpdateSupply(1_000_000, 500_000, 4.5, 2_250_000)
	tr.UpdateDemand(2_000_000, 800_000, 50_000_000)
	tr.UpdateEconomy(10_000_000, 500_000, 400_000, 100_000)
	tr.UpdateRetention(85.0, 72.0)
	tr.UpdateViralCoefficient(1.3)

	// Need positive growth rate
	tr.mu.Lock()
	tr.current.SupplyGrowthRate = 5.0
	tr.mu.Unlock()

	sustainable, index, viralK := tr.GateCheck()
	if !sustainable {
		t.Fatal("expected sustainable economy")
	}
	if index <= 0 {
		t.Fatalf("expected positive health index, got %f", index)
	}
	if viralK != 1.3 {
		t.Fatalf("expected 1.3 viral k, got %f", viralK)
	}
}
