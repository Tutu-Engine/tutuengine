package domain

import (
	"testing"
	"time"
)

// ═══════════════════════════════════════════════════════════════════════════
// Phase 7 Domain Type Tests
// ═══════════════════════════════════════════════════════════════════════════

// ─── Continent Tests ────────────────────────────────────────────────────────

func TestContinentID_IsValid(t *testing.T) {
	tests := []struct {
		name  string
		id    ContinentID
		valid bool
	}{
		{"North America", ContinentNorthAmerica, true},
		{"South America", ContinentSouthAmerica, true},
		{"Europe", ContinentEurope, true},
		{"Africa", ContinentAfrica, true},
		{"Asia", ContinentAsia, true},
		{"Oceania", ContinentOceania, true},
		{"Antarctica is excluded", ContinentAntarctica, false},
		{"empty", ContinentID(""), false},
		{"unknown", ContinentID("mars"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.id.IsValid(); got != tc.valid {
				t.Errorf("ContinentID(%q).IsValid() = %v, want %v", tc.id, got, tc.valid)
			}
		})
	}
}

func TestContinentID_String(t *testing.T) {
	if got := ContinentEurope.String(); got != "Europe" {
		t.Errorf("ContinentEurope.String() = %q, want %q", got, "Europe")
	}
	if got := ContinentID("unknown").String(); got != "unknown" {
		t.Errorf("ContinentID(unknown).String() = %q, want %q", got, "unknown")
	}
}

func TestAllContinents_Count(t *testing.T) {
	// 6 populated continents (Antarctica excluded from active list)
	if got := len(AllContinents()); got != 6 {
		t.Errorf("AllContinents() returned %d, want 6", got)
	}
}

// ─── PlanetaryRegion Tests ──────────────────────────────────────────────────

func TestPlanetaryRegion_Load(t *testing.T) {
	tests := []struct {
		name        string
		nodeCount   int64
		activeTasks int64
		wantLoad    float64
	}{
		{"zero nodes", 0, 100, 1.0},
		{"idle", 1000, 0, 0.0},
		{"half loaded", 1000, 500, 0.5},
		{"fully loaded", 100, 100, 1.0},
		{"overloaded", 100, 200, 2.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pr := PlanetaryRegion{NodeCount: tc.nodeCount}
			got := pr.Load(tc.activeTasks)
			if got != tc.wantLoad {
				t.Errorf("Load(%d) = %f, want %f", tc.activeTasks, got, tc.wantLoad)
			}
		})
	}
}

// ─── ContinentMesh Tests ────────────────────────────────────────────────────

func TestContinentMesh_TotalNodes(t *testing.T) {
	mesh := ContinentMesh{
		Regions: []PlanetaryRegion{
			{NodeCount: 100},
			{NodeCount: 200},
			{NodeCount: 50},
		},
	}
	if got := mesh.TotalNodes(); got != 350 {
		t.Errorf("TotalNodes() = %d, want 350", got)
	}
}

func TestContinentMesh_TotalNodes_Empty(t *testing.T) {
	mesh := ContinentMesh{}
	if got := mesh.TotalNodes(); got != 0 {
		t.Errorf("TotalNodes() = %d, want 0", got)
	}
}

func TestContinentMesh_HealthyRegionCount(t *testing.T) {
	mesh := ContinentMesh{
		Regions: []PlanetaryRegion{
			{Healthy: true},
			{Healthy: false},
			{Healthy: true},
			{Healthy: true},
		},
	}
	if got := mesh.HealthyRegionCount(); got != 3 {
		t.Errorf("HealthyRegionCount() = %d, want 3", got)
	}
}

// ─── PlanetaryTopology Tests ────────────────────────────────────────────────

func TestPlanetaryTopology_IsQuorumHealthy(t *testing.T) {
	tests := []struct {
		name    string
		healthy []bool // one per continent
		want    bool
	}{
		{"all healthy", []bool{true, true, true, true}, true},
		{"majority healthy", []bool{true, true, true, false}, true},
		{"exactly half", []bool{true, true, false, false}, false},
		{"minority healthy", []bool{true, false, false, false}, false},
		{"none healthy", []bool{false, false, false, false}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pt := PlanetaryTopology{
				Continents: make(map[ContinentID]*ContinentMesh),
			}
			continents := AllContinents()
			for i, h := range tc.healthy {
				regions := []PlanetaryRegion{}
				if h {
					regions = append(regions, PlanetaryRegion{Healthy: true})
				} else {
					regions = append(regions, PlanetaryRegion{Healthy: false})
				}
				pt.Continents[continents[i]] = &ContinentMesh{Regions: regions}
			}

			if got := pt.IsQuorumHealthy(); got != tc.want {
				t.Errorf("IsQuorumHealthy() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ─── Access Tier Tests ──────────────────────────────────────────────────────

func TestAccessTier_IsValid(t *testing.T) {
	tests := []struct {
		tier  AccessTier
		valid bool
	}{
		{AccessTierFree, true},
		{AccessTierEducation, true},
		{AccessTierPro, true},
		{AccessTierEnterprise, true},
		{AccessTier("premium"), false},
		{AccessTier(""), false},
	}

	for _, tc := range tests {
		t.Run(string(tc.tier), func(t *testing.T) {
			if got := tc.tier.IsValid(); got != tc.valid {
				t.Errorf("AccessTier(%q).IsValid() = %v, want %v", tc.tier, got, tc.valid)
			}
		})
	}
}

func TestDefaultTierQuotas(t *testing.T) {
	quotas := DefaultTierQuotas()

	// Free tier: 100 inferences/day
	if q := quotas[AccessTierFree]; q.MaxInferencesPerDay != 100 {
		t.Errorf("Free tier limit = %d, want 100", q.MaxInferencesPerDay)
	}

	// Education tier: unlimited
	if q := quotas[AccessTierEducation]; q.MaxInferencesPerDay != -1 {
		t.Errorf("Education tier limit = %d, want -1 (unlimited)", q.MaxInferencesPerDay)
	}

	// Enterprise tier: unlimited + highest priority
	q := quotas[AccessTierEnterprise]
	if q.MaxInferencesPerDay != -1 {
		t.Errorf("Enterprise limit = %d, want -1", q.MaxInferencesPerDay)
	}
	if q.Priority != 255 {
		t.Errorf("Enterprise priority = %d, want 255", q.Priority)
	}
}

func TestTierUsage_RemainingInferences(t *testing.T) {
	tests := []struct {
		name    string
		used    int64
		limit   int64
		want    int64
	}{
		{"fresh user", 0, 100, 100},
		{"half used", 50, 100, 50},
		{"fully used", 100, 100, 0},
		{"over used", 150, 100, 0},
		{"unlimited tier", 999999, -1, -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			usage := TierUsage{InferencesToday: tc.used}
			quota := TierQuota{MaxInferencesPerDay: tc.limit}
			if got := usage.RemainingInferences(quota); got != tc.want {
				t.Errorf("RemainingInferences() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestTierUsage_IsExhausted(t *testing.T) {
	tests := []struct {
		name  string
		used  int64
		limit int64
		want  bool
	}{
		{"within limit", 50, 100, false},
		{"at limit", 100, 100, true},
		{"over limit", 150, 100, true},
		{"unlimited", 999999, -1, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			usage := TierUsage{InferencesToday: tc.used}
			quota := TierQuota{MaxInferencesPerDay: tc.limit}
			if got := usage.IsExhausted(quota); got != tc.want {
				t.Errorf("IsExhausted() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEducationVerification_IsVerified(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name   string
		status string
		exp    time.Time
		want   bool
	}{
		{"verified and active", "verified", now.Add(30 * 24 * time.Hour), true},
		{"verified but expired", "verified", now.Add(-1 * time.Hour), false},
		{"pending", "pending", now.Add(30 * 24 * time.Hour), false},
		{"rejected", "rejected", now.Add(30 * 24 * time.Hour), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := EducationVerification{Status: tc.status, ExpiresAt: tc.exp}
			if got := ev.IsVerified(); got != tc.want {
				t.Errorf("IsVerified() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ─── Flywheel Health Tests ──────────────────────────────────────────────────

func TestFlywheelHealth_IsSustainable(t *testing.T) {
	tests := []struct {
		name string
		fh   FlywheelHealth
		want bool
	}{
		{
			"healthy economy",
			FlywheelHealth{
				SupplyDemandRatio:  1.2,
				NetworkEffectIndex: 75.0,
				EnterpriseRevenue:  10000,
				SupplyGrowthRate:   5.0,
			},
			true,
		},
		{
			"no enterprise revenue",
			FlywheelHealth{
				SupplyDemandRatio:  1.0,
				NetworkEffectIndex: 75.0,
				EnterpriseRevenue:  0,
				SupplyGrowthRate:   5.0,
			},
			false,
		},
		{
			"low network effect",
			FlywheelHealth{
				SupplyDemandRatio:  1.0,
				NetworkEffectIndex: 30.0,
				EnterpriseRevenue:  10000,
				SupplyGrowthRate:   5.0,
			},
			false,
		},
		{
			"negative growth",
			FlywheelHealth{
				SupplyDemandRatio:  1.0,
				NetworkEffectIndex: 75.0,
				EnterpriseRevenue:  10000,
				SupplyGrowthRate:   -2.0,
			},
			false,
		},
		{
			"supply deficit",
			FlywheelHealth{
				SupplyDemandRatio:  0.5,
				NetworkEffectIndex: 75.0,
				EnterpriseRevenue:  10000,
				SupplyGrowthRate:   5.0,
			},
			false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fh.IsSustainable(); got != tc.want {
				t.Errorf("IsSustainable() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFlywheelHealth_GrowthStatus(t *testing.T) {
	tests := []struct {
		viral float64
		want  string
	}{
		{2.0, "hypergrowth"},
		{1.3, "organic_growth"},
		{0.9, "stable"},
		{0.6, "declining"},
		{0.3, "critical"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			fh := FlywheelHealth{ViralCoefficient: tc.viral}
			if got := fh.GrowthStatus(); got != tc.want {
				t.Errorf("GrowthStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ─── AI Democracy Tests ─────────────────────────────────────────────────────

func TestProtectionLevel_RequiredMajority(t *testing.T) {
	tests := []struct {
		level ProtectionLevel
		want  float64
	}{
		{ProtectionNormal, 0.50},
		{ProtectionElevated, 0.60},
		{ProtectionCritical, 0.67},
		{ProtectionImmutable, 2.0},
	}

	for _, tc := range tests {
		t.Run(tc.level.String(), func(t *testing.T) {
			if got := tc.level.RequiredMajority(); got != tc.want {
				t.Errorf("RequiredMajority() = %f, want %f", got, tc.want)
			}
		})
	}
}

func TestProtectionLevel_String(t *testing.T) {
	if got := ProtectionCritical.String(); got != "critical" {
		t.Errorf("ProtectionCritical.String() = %q, want %q", got, "critical")
	}
}

func TestCouncilMember_IsTermActive(t *testing.T) {
	active := CouncilMember{TermExpires: time.Now().Add(30 * 24 * time.Hour)}
	expired := CouncilMember{TermExpires: time.Now().Add(-1 * time.Hour)}

	if !active.IsTermActive() {
		t.Error("expected active council member to have active term")
	}
	if expired.IsTermActive() {
		t.Error("expected expired council member to not have active term")
	}
}

func TestCouncilElection_TurnoutPct(t *testing.T) {
	tests := []struct {
		name     string
		votes    int64
		eligible int64
		want     float64
	}{
		{"full turnout", 100, 100, 100.0},
		{"half turnout", 50, 100, 50.0},
		{"low turnout", 5, 100, 5.0},
		{"no voters", 0, 0, 0.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ce := CouncilElection{TotalVotes: tc.votes, EligibleVoters: tc.eligible}
			if got := ce.TurnoutPct(); got != tc.want {
				t.Errorf("TurnoutPct() = %f, want %f", got, tc.want)
			}
		})
	}
}

func TestCouncilElection_IsValidElection(t *testing.T) {
	valid := CouncilElection{TotalVotes: 15, EligibleVoters: 100}   // 15%
	invalid := CouncilElection{TotalVotes: 5, EligibleVoters: 100}  // 5%
	empty := CouncilElection{TotalVotes: 0, EligibleVoters: 0}

	if !valid.IsValidElection() {
		t.Error("15% turnout should be valid")
	}
	if invalid.IsValidElection() {
		t.Error("5% turnout should be invalid")
	}
	if empty.IsValidElection() {
		t.Error("0 voters should be invalid")
	}
}

func TestOpenSourceCompliance_IsCompliant(t *testing.T) {
	compliant := OpenSourceCompliance{
		AllCoreCodeMIT:    true,
		NoProprietaryDeps: true,
		CommunityGoverned: true,
	}
	if !compliant.IsCompliant() {
		t.Error("should be compliant when all checks pass")
	}

	nonCompliant := OpenSourceCompliance{
		AllCoreCodeMIT:    true,
		NoProprietaryDeps: false, // Has proprietary deps
		CommunityGoverned: true,
	}
	if nonCompliant.IsCompliant() {
		t.Error("should not be compliant with proprietary deps")
	}
}

// ─── Phase 7 Gate Check Tests ───────────────────────────────────────────────

func TestPhase7GateCheck_Passed(t *testing.T) {
	// All conditions met
	passing := Phase7GateCheck{
		TotalNodes:            15_000_000,
		CountriesReached:      200,
		FreeTierOperational:   true,
		EconomySustainable:    true,
		OpenSourceCompliant:   true,
		UptimePct:             99.995,
		P99InferenceLatencyMs: 500,
		InferencesPerDay:      2_000_000_000,
	}
	if !passing.Passed() {
		t.Error("all conditions met but Passed() = false")
	}

	// Below node threshold
	failing := passing
	failing.TotalNodes = 5_000_000
	if failing.Passed() {
		t.Error("below 10M nodes should fail")
	}
}

func TestPhase7GateCheck_Summary(t *testing.T) {
	gc := Phase7GateCheck{
		TotalNodes:            15_000_000,
		CountriesReached:      200,
		FreeTierOperational:   true,
		EconomySustainable:    false,
		OpenSourceCompliant:   true,
		UptimePct:             99.995,
		P99InferenceLatencyMs: 500,
		InferencesPerDay:      2_000_000_000,
	}

	summary := gc.Summary()
	if len(summary) != 8 {
		t.Errorf("Summary() returned %d checks, want 8", len(summary))
	}

	// Check that the unsustainable economy is flagged
	found := false
	for _, s := range summary {
		if s == "FAIL: Network self-sustaining economically" {
			found = true
		}
	}
	if !found {
		t.Error("expected FAIL for unsustainable economy in summary")
	}
}
