// Package domain — Phase 7: Event Horizon domain types.
//
// Phase 7 is the capstone: "World's Largest Distributed AI Supercomputer."
// It adds 4 core systems on top of Phases 0-6:
//
//  1. Planetary-Scale Infrastructure — 50+ regions, continental mesh, exabyte distribution
//  2. Universal Access — Free/Education/Pro/Enterprise tiers with quota enforcement
//  3. Economic Flywheel — Self-sustaining economy tracking and health indicators
//  4. AI Democracy — Community governance for ALL parameters, open-source compliance
//
// Architecture References: Part I (Vision), Part XXII (Final Vision), Phase 7 Gate Checks.
// All types are pure — zero infrastructure imports.
package domain

import "time"

// ═══════════════════════════════════════════════════════════════════════════
// Section 1: Planetary-Scale Infrastructure Types
// ═══════════════════════════════════════════════════════════════════════════

// ContinentID identifies a physical continent for hierarchical routing.
// Phase 7 extends the 3-region model (Phase 3) to 7 continents, 50+ regions.
type ContinentID string

const (
	ContinentNorthAmerica ContinentID = "na"
	ContinentSouthAmerica ContinentID = "sa"
	ContinentEurope       ContinentID = "eu"
	ContinentAfrica       ContinentID = "af"
	ContinentAsia         ContinentID = "as"
	ContinentOceania      ContinentID = "oc"
	ContinentAntarctica   ContinentID = "aq" // Future: research stations
)

// AllContinents returns every supported continent.
func AllContinents() []ContinentID {
	return []ContinentID{
		ContinentNorthAmerica, ContinentSouthAmerica,
		ContinentEurope, ContinentAfrica,
		ContinentAsia, ContinentOceania,
	}
}

// IsValid reports whether c is a recognized continent.
func (c ContinentID) IsValid() bool {
	for _, valid := range AllContinents() {
		if c == valid {
			return true
		}
	}
	return false
}

// String returns the continent as a human-readable label.
func (c ContinentID) String() string {
	labels := map[ContinentID]string{
		ContinentNorthAmerica: "North America",
		ContinentSouthAmerica: "South America",
		ContinentEurope:       "Europe",
		ContinentAfrica:       "Africa",
		ContinentAsia:         "Asia",
		ContinentOceania:      "Oceania",
		ContinentAntarctica:   "Antarctica",
	}
	if s, ok := labels[c]; ok {
		return s
	}
	return string(c)
}

// PlanetaryRegion extends RegionID with continent and zone hierarchy.
// Hierarchy: Continent → Region → Zone → Node
type PlanetaryRegion struct {
	Region    RegionID    `json:"region"`
	Continent ContinentID `json:"continent"`
	Zone      string      `json:"zone"`      // e.g., "us-east-1a"
	Country   string      `json:"country"`   // ISO 3166-1 alpha-2: "US", "DE", "JP"
	City      string      `json:"city"`      // Nearest city for latency estimation
	NodeCount int64       `json:"node_count"`
	Healthy   bool        `json:"healthy"`
	LatencyMs float64     `json:"latency_ms"` // Avg intra-region latency
	UpdatedAt time.Time   `json:"updated_at"`
}

// Load returns the region's load factor based on active tasks and capacity.
func (pr PlanetaryRegion) Load(activeTasks int64) float64 {
	if pr.NodeCount == 0 {
		return 1.0
	}
	return float64(activeTasks) / float64(pr.NodeCount)
}

// ContinentMesh represents the inter-continent routing topology.
// Each continent has a "gateway" region that routes to other continents.
type ContinentMesh struct {
	Continent ContinentID        `json:"continent"`
	Gateway   RegionID           `json:"gateway"`    // Primary gateway region
	Regions   []PlanetaryRegion  `json:"regions"`
	Links     []ContinentLink    `json:"links"`      // Links to other continents
	UpdatedAt time.Time          `json:"updated_at"`
}

// TotalNodes returns the sum of nodes across all regions in this continent.
func (cm ContinentMesh) TotalNodes() int64 {
	var total int64
	for _, r := range cm.Regions {
		total += r.NodeCount
	}
	return total
}

// HealthyRegionCount returns the number of healthy regions.
func (cm ContinentMesh) HealthyRegionCount() int {
	count := 0
	for _, r := range cm.Regions {
		if r.Healthy {
			count++
		}
	}
	return count
}

// ContinentLink describes the network path between two continents.
type ContinentLink struct {
	From      ContinentID `json:"from"`
	To        ContinentID `json:"to"`
	LatencyMs int         `json:"latency_ms"` // Round-trip
	Bandwidth float64     `json:"bandwidth"`   // Gbps estimate
	Healthy   bool        `json:"healthy"`
}

// PlanetaryTopology is the full global view of the network.
type PlanetaryTopology struct {
	Continents   map[ContinentID]*ContinentMesh `json:"continents"`
	TotalNodes   int64                           `json:"total_nodes"`
	TotalRegions int                             `json:"total_regions"`
	GlobalHealth float64                         `json:"global_health"` // 0.0 = dead, 1.0 = perfect
	UpdatedAt    time.Time                       `json:"updated_at"`
}

// IsQuorumHealthy reports whether a majority of continents are reachable.
// Quorum = more than half of all continents must be healthy.
func (pt PlanetaryTopology) IsQuorumHealthy() bool {
	healthy := 0
	total := len(pt.Continents)
	for _, cm := range pt.Continents {
		if cm.HealthyRegionCount() > 0 {
			healthy++
		}
	}
	return healthy > total/2
}

// ModelDistributionStats tracks exabyte-scale model distribution.
type ModelDistributionStats struct {
	TotalModelsDistributed int64   `json:"total_models_distributed"`
	TotalBytesDistributed  int64   `json:"total_bytes_distributed"`  // Across all nodes
	P2PShareRatio          float64 `json:"p2p_share_ratio"`          // % of downloads via P2P
	CDNCostSavings         float64 `json:"cdn_cost_savings_percent"` // % saved vs pure CDN
	AvgDistributionTimeSec float64 `json:"avg_distribution_time_sec"`
	ContinentCoverage      map[ContinentID]float64 `json:"continent_coverage"` // % of popular models cached
}

// ═══════════════════════════════════════════════════════════════════════════
// Section 2: Universal Access Tier Types
// ═══════════════════════════════════════════════════════════════════════════

// AccessTier defines the user's access level for inference.
// Architecture: Free tier funded by enterprise MCP revenue.
type AccessTier string

const (
	AccessTierFree       AccessTier = "free"       // 100 inferences/day, no account
	AccessTierEducation  AccessTier = "education"  // Unlimited, verified students/researchers
	AccessTierPro        AccessTier = "pro"        // 10K inferences/day, credit-funded
	AccessTierEnterprise AccessTier = "enterprise" // Unlimited, SLA-backed, paid
)

// AllAccessTiers returns every access tier in priority order.
func AllAccessTiers() []AccessTier {
	return []AccessTier{
		AccessTierFree, AccessTierEducation, AccessTierPro, AccessTierEnterprise,
	}
}

// IsValid reports whether t is a recognized access tier.
func (t AccessTier) IsValid() bool {
	for _, valid := range AllAccessTiers() {
		if t == valid {
			return true
		}
	}
	return false
}

// TierQuota defines the daily inference limits for an access tier.
type TierQuota struct {
	Tier               AccessTier `json:"tier"`
	MaxInferencesPerDay int64     `json:"max_inferences_per_day"` // -1 = unlimited
	MaxTokensPerRequest int       `json:"max_tokens_per_request"`
	MaxModels           int       `json:"max_models"`             // Concurrent model slots
	Priority            int       `json:"priority"`               // Higher = faster scheduling
	RateLimitPerMin     int       `json:"rate_limit_per_min"`
}

// DefaultTierQuotas returns the architecture-defined tier limits.
func DefaultTierQuotas() map[AccessTier]TierQuota {
	return map[AccessTier]TierQuota{
		AccessTierFree: {
			Tier:                AccessTierFree,
			MaxInferencesPerDay: 100,
			MaxTokensPerRequest: 2048,
			MaxModels:           1,
			Priority:            10,
			RateLimitPerMin:     5,
		},
		AccessTierEducation: {
			Tier:                AccessTierEducation,
			MaxInferencesPerDay: -1, // unlimited
			MaxTokensPerRequest: 8192,
			MaxModels:           3,
			Priority:            50,
			RateLimitPerMin:     30,
		},
		AccessTierPro: {
			Tier:                AccessTierPro,
			MaxInferencesPerDay: 10000,
			MaxTokensPerRequest: 16384,
			MaxModels:           5,
			Priority:            100,
			RateLimitPerMin:     60,
		},
		AccessTierEnterprise: {
			Tier:                AccessTierEnterprise,
			MaxInferencesPerDay: -1, // unlimited
			MaxTokensPerRequest: 32768,
			MaxModels:           -1, // unlimited
			Priority:            255,
			RateLimitPerMin:     300,
		},
	}
}

// TierUsage tracks a user's consumption against their quota for the current day.
type TierUsage struct {
	UserID          string     `json:"user_id"`
	Tier            AccessTier `json:"tier"`
	InferencesToday int64      `json:"inferences_today"`
	TokensToday     int64      `json:"tokens_today"`
	ResetAt         time.Time  `json:"reset_at"` // Midnight UTC
}

// RemainingInferences returns how many inferences the user has left today.
// Returns -1 for unlimited tiers.
func (u TierUsage) RemainingInferences(quota TierQuota) int64 {
	if quota.MaxInferencesPerDay < 0 {
		return -1 // unlimited
	}
	remaining := quota.MaxInferencesPerDay - u.InferencesToday
	if remaining < 0 {
		return 0
	}
	return remaining
}

// IsExhausted reports whether the user has exceeded their daily quota.
func (u TierUsage) IsExhausted(quota TierQuota) bool {
	if quota.MaxInferencesPerDay < 0 {
		return false // unlimited
	}
	return u.InferencesToday >= quota.MaxInferencesPerDay
}

// EducationVerification represents a student/researcher verification request.
type EducationVerification struct {
	UserID       string    `json:"user_id"`
	Institution  string    `json:"institution"`
	Email        string    `json:"email"`         // Must be .edu or recognized academic domain
	Status       string    `json:"status"`        // "pending", "verified", "rejected"
	VerifiedAt   time.Time `json:"verified_at,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"` // Yearly re-verification
}

// IsVerified reports whether the education verification is currently active.
func (ev EducationVerification) IsVerified() bool {
	return ev.Status == "verified" && time.Now().Before(ev.ExpiresAt)
}

// ═══════════════════════════════════════════════════════════════════════════
// Section 3: Economic Flywheel Types
// ═══════════════════════════════════════════════════════════════════════════

// FlywheelHealth represents the overall health of the self-sustaining economy.
// The flywheel: Users contribute idle compute → earn credits → spend on AI
// → more demand → more contributors → network effects.
type FlywheelHealth struct {
	// Supply side — contributors
	TotalContributors    int64   `json:"total_contributors"`
	ActiveContributors   int64   `json:"active_contributors"`   // Active in last 24h
	AvgContributionHours float64 `json:"avg_contribution_hours"` // Per day per node
	TotalComputeHours    float64 `json:"total_compute_hours"`    // Global 24h
	SupplyGrowthRate     float64 `json:"supply_growth_rate"`     // % growth/week

	// Demand side — consumers
	TotalConsumers      int64   `json:"total_consumers"`
	ActiveConsumers     int64   `json:"active_consumers"` // Active in last 24h
	InferencesPerDay    int64   `json:"inferences_per_day"`
	DemandGrowthRate    float64 `json:"demand_growth_rate"` // % growth/week

	// Economy balance
	CreditsInCirculation int64   `json:"credits_in_circulation"`
	CreditsEarnedToday   int64   `json:"credits_earned_today"`
	CreditsSpentToday    int64   `json:"credits_spent_today"`
	SupplyDemandRatio    float64 `json:"supply_demand_ratio"` // >1 = surplus, <1 = deficit
	EnterpriseRevenue    int64   `json:"enterprise_revenue"`  // Credits from enterprise MCP

	// Network effect metrics
	NetworkEffectIndex float64 `json:"network_effect_index"` // Composite health score 0-100
	ViralCoefficient   float64 `json:"viral_coefficient"`    // >1 = organic growth
	RetentionRate7d    float64 `json:"retention_rate_7d"`    // 7-day retention %
	RetentionRate30d   float64 `json:"retention_rate_30d"`   // 30-day retention %

	MeasuredAt time.Time `json:"measured_at"`
}

// IsSustainable reports whether the economy is self-sustaining.
// Sustainable = enterprise revenue covers free tier + supply > demand + positive growth.
func (fh FlywheelHealth) IsSustainable() bool {
	return fh.SupplyDemandRatio >= 0.8 &&
		fh.NetworkEffectIndex >= 50.0 &&
		fh.EnterpriseRevenue > 0 &&
		fh.SupplyGrowthRate > 0
}

// GrowthStatus categorizes the network's growth trajectory.
func (fh FlywheelHealth) GrowthStatus() string {
	if fh.ViralCoefficient > 1.5 {
		return "hypergrowth"
	}
	if fh.ViralCoefficient > 1.0 {
		return "organic_growth"
	}
	if fh.ViralCoefficient > 0.8 {
		return "stable"
	}
	if fh.ViralCoefficient > 0.5 {
		return "declining"
	}
	return "critical"
}

// FlywheelSnapshot is a time-series data point for economic tracking.
type FlywheelSnapshot struct {
	Timestamp    time.Time `json:"timestamp"`
	Nodes        int64     `json:"nodes"`
	Inferences   int64     `json:"inferences"`
	Credits      int64     `json:"credits"`
	Revenue      int64     `json:"revenue"`
	HealthIndex  float64   `json:"health_index"`
}

// ═══════════════════════════════════════════════════════════════════════════
// Section 4: AI Democracy Types
// ═══════════════════════════════════════════════════════════════════════════

// GovernableParam defines a network parameter that can be changed by vote.
// Phase 5 governance handled proposals; Phase 7 extends to ALL parameters
// with a formal category system and protection levels.
type GovernableParam struct {
	Key          string         `json:"key"`          // e.g., "free_tier_daily_limit"
	Category     ParamCategory  `json:"category"`
	CurrentValue string         `json:"current_value"`
	Description  string         `json:"description"`
	Protection   ProtectionLevel `json:"protection"`
	LastChanged  time.Time      `json:"last_changed"`
	ChangedBy    string         `json:"changed_by"` // Proposal ID that changed it
}

// ParamCategory groups governable parameters.
type ParamCategory string

const (
	ParamCategoryEconomic  ParamCategory = "economic"  // Credit rates, earning caps
	ParamCategoryAccess    ParamCategory = "access"     // Tier limits, quotas
	ParamCategoryTechnical ParamCategory = "technical"  // Timeouts, thresholds
	ParamCategorySecurity  ParamCategory = "security"   // Security policies
	ParamCategoryNetwork   ParamCategory = "network"    // Routing, replication
)

// ProtectionLevel determines the voting threshold needed to change a parameter.
type ProtectionLevel int

const (
	ProtectionNormal      ProtectionLevel = iota // Simple majority (>50%)
	ProtectionElevated                           // Supermajority (>60%)
	ProtectionCritical                           // Supermajority (>67%)
	ProtectionImmutable                          // Cannot be changed by vote
)

// RequiredMajority returns the vote percentage required for this protection level.
func (p ProtectionLevel) RequiredMajority() float64 {
	switch p {
	case ProtectionNormal:
		return 0.50
	case ProtectionElevated:
		return 0.60
	case ProtectionCritical:
		return 0.67
	case ProtectionImmutable:
		return 2.0 // Impossible to achieve (>100%)
	default:
		return 0.50
	}
}

// String returns a human-readable name for the protection level.
func (p ProtectionLevel) String() string {
	names := [...]string{"normal", "elevated", "critical", "immutable"}
	if int(p) < len(names) {
		return names[p]
	}
	return "unknown"
}

// CouncilMember represents an elected community council member.
// Council members can fast-track proposals and represent their continent.
type CouncilMember struct {
	NodeID      string      `json:"node_id"`
	Continent   ContinentID `json:"continent"`
	VotesFor    int64       `json:"votes_for"`
	ElectedAt   time.Time   `json:"elected_at"`
	TermExpires time.Time   `json:"term_expires"` // 6-month terms
}

// IsTermActive reports whether the council member's term is still valid.
func (cm CouncilMember) IsTermActive() bool {
	return time.Now().Before(cm.TermExpires)
}

// CouncilElection tracks a community council election.
type CouncilElection struct {
	ID           string                  `json:"id"`
	Continent    ContinentID             `json:"continent"`
	Candidates   []CouncilCandidate      `json:"candidates"`
	TotalVotes   int64                   `json:"total_votes"`
	EligibleVoters int64                 `json:"eligible_voters"`
	Status       string                  `json:"status"` // "open", "closed", "certified"
	OpensAt      time.Time               `json:"opens_at"`
	ClosesAt     time.Time               `json:"closes_at"`
}

// CouncilCandidate is a node running for council.
type CouncilCandidate struct {
	NodeID    string `json:"node_id"`
	Platform  string `json:"platform"` // Short statement of intent
	VotesFor  int64  `json:"votes_for"`
}

// TurnoutPct returns the voter turnout percentage.
func (ce CouncilElection) TurnoutPct() float64 {
	if ce.EligibleVoters == 0 {
		return 0
	}
	return float64(ce.TotalVotes) / float64(ce.EligibleVoters) * 100
}

// IsValidElection reports whether minimum turnout (10%) was achieved.
func (ce CouncilElection) IsValidElection() bool {
	return ce.TurnoutPct() >= 10.0
}

// OpenSourceCompliance checks that the network remains open-source.
type OpenSourceCompliance struct {
	AllCoreCodeMIT      bool   `json:"all_core_code_mit"`
	NoProprietaryDeps   bool   `json:"no_proprietary_deps"`
	CommunityGoverned   bool   `json:"community_governed"`
	TransparencyLogURL  string `json:"transparency_log_url"`
	LastAuditDate       time.Time `json:"last_audit_date"`
}

// IsCompliant reports whether the network passes open-source compliance checks.
func (osc OpenSourceCompliance) IsCompliant() bool {
	return osc.AllCoreCodeMIT && osc.NoProprietaryDeps && osc.CommunityGoverned
}

// ═══════════════════════════════════════════════════════════════════════════
// Section 5: Phase 7 Gate Check Types
// ═══════════════════════════════════════════════════════════════════════════

// Phase7GateCheck captures whether the Phase 7 gate requirements are met.
// From phases.md:
//   - 10M+ registered nodes
//   - Available in every country
//   - Free tier operational
//   - Network is self-sustaining economically
//   - All code open source
//   - 99.99% uptime globally
//   - Sub-second inference for all model sizes
//   - Billions of inferences per day
type Phase7GateCheck struct {
	TotalNodes             int64   `json:"total_nodes"`
	CountriesReached       int     `json:"countries_reached"`
	FreeTierOperational    bool    `json:"free_tier_operational"`
	EconomySustainable     bool    `json:"economy_sustainable"`
	OpenSourceCompliant    bool    `json:"open_source_compliant"`
	UptimePct              float64 `json:"uptime_pct"` // Target: 99.99
	P99InferenceLatencyMs  float64 `json:"p99_inference_latency_ms"`
	InferencesPerDay       int64   `json:"inferences_per_day"`
}

// Passed reports whether all Phase 7 gate checks pass.
func (g Phase7GateCheck) Passed() bool {
	return g.TotalNodes >= 10_000_000 &&
		g.CountriesReached >= 195 &&
		g.FreeTierOperational &&
		g.EconomySustainable &&
		g.OpenSourceCompliant &&
		g.UptimePct >= 99.99 &&
		g.P99InferenceLatencyMs <= 1000 &&
		g.InferencesPerDay >= 1_000_000_000
}

// Summary returns a human-readable check status.
func (g Phase7GateCheck) Summary() []string {
	checks := []string{}
	appendCheck := func(pass bool, msg string) {
		prefix := "PASS"
		if !pass {
			prefix = "FAIL"
		}
		checks = append(checks, prefix+": "+msg)
	}

	appendCheck(g.TotalNodes >= 10_000_000, "10M+ registered nodes")
	appendCheck(g.CountriesReached >= 195, "Available in every country")
	appendCheck(g.FreeTierOperational, "Free tier operational")
	appendCheck(g.EconomySustainable, "Network self-sustaining economically")
	appendCheck(g.OpenSourceCompliant, "All code open source")
	appendCheck(g.UptimePct >= 99.99, "99.99% uptime globally")
	appendCheck(g.P99InferenceLatencyMs <= 1000, "Sub-second inference (p99)")
	appendCheck(g.InferencesPerDay >= 1_000_000_000, "Billions of inferences/day")

	return checks
}
