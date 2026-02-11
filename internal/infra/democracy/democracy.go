// Package democracy implements Phase 7 AI democracy governance.
//
// Phase 5 introduced governance (proposals + credit-weighted voting).
// Phase 7 extends this to FULL democratic control:
//
//   - ALL network parameters are governable (economic, access, technical, security)
//   - Protection levels prevent reckless parameter changes
//   - Community council: elected representatives per continent (6-month terms)
//   - Open-source compliance: automated checks ensure code stays MIT licensed
//   - No single point of control: network operates without any single entity
//
// This is what makes TuTu a true public good — not controlled by any company.
//
// Architecture Reference: Phase 7 "AI Democracy" + Phase 5 governance extension.
package democracy

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ═══════════════════════════════════════════════════════════════════════════
// Configuration
// ═══════════════════════════════════════════════════════════════════════════

// Config controls the democracy engine.
type Config struct {
	// CouncilTermMonths: how long a council member serves (default: 6).
	CouncilTermMonths int

	// MinElectionTurnout: minimum voter turnout % for valid election.
	MinElectionTurnout float64

	// ElectionDurationDays: how long elections stay open.
	ElectionDurationDays int

	// ParameterChangeQuorum: minimum % of total credit-weight needed to vote.
	ParameterChangeQuorum float64

	// ComplianceCheckInterval: how often to run open-source compliance checks.
	ComplianceCheckInterval time.Duration
}

// DefaultConfig returns sensible defaults for the democracy engine.
func DefaultConfig() Config {
	return Config{
		CouncilTermMonths:       6,
		MinElectionTurnout:      10.0,  // 10% minimum
		ElectionDurationDays:    14,    // 2 weeks
		ParameterChangeQuorum:   30.0,  // 30% of credit weight
		ComplianceCheckInterval: 24 * time.Hour,
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Democracy Engine
// ═══════════════════════════════════════════════════════════════════════════

// Engine manages all Phase 7 democratic governance functions.
type Engine struct {
	mu     sync.RWMutex
	config Config

	// Governable parameters registry
	params map[string]*domain.GovernableParam

	// Council members per continent
	council map[domain.ContinentID]*domain.CouncilMember

	// Active elections
	elections map[string]*domain.CouncilElection

	// Open-source compliance state
	compliance domain.OpenSourceCompliance

	// Injectable clock
	now func() time.Time
}

// NewEngine creates a democracy Engine with the given configuration.
func NewEngine(cfg Config) *Engine {
	e := &Engine{
		config:    cfg,
		params:    make(map[string]*domain.GovernableParam),
		council:   make(map[domain.ContinentID]*domain.CouncilMember),
		elections: make(map[string]*domain.CouncilElection),
		now:       time.Now,
	}

	// Register default governable parameters
	e.registerDefaultParams()

	return e
}

// ═══════════════════════════════════════════════════════════════════════════
// Parameter Management
// ═══════════════════════════════════════════════════════════════════════════

// RegisterParam registers a new governable parameter.
func (e *Engine) RegisterParam(param domain.GovernableParam) error {
	if param.Key == "" {
		return fmt.Errorf("parameter key cannot be empty")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	param.LastChanged = e.now()
	e.params[param.Key] = &param
	return nil
}

// GetParam returns a governable parameter by key.
func (e *Engine) GetParam(key string) (domain.GovernableParam, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	p, ok := e.params[key]
	if !ok {
		return domain.GovernableParam{}, fmt.Errorf("parameter %q not found", key)
	}
	return *p, nil
}

// ListParams returns all governable parameters.
func (e *Engine) ListParams() []domain.GovernableParam {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]domain.GovernableParam, 0, len(e.params))
	for _, p := range e.params {
		result = append(result, *p)
	}

	// Sort by key for deterministic output
	sort.Slice(result, func(i, j int) bool {
		return result[i].Key < result[j].Key
	})
	return result
}

// ListParamsByCategory returns parameters in a specific category.
func (e *Engine) ListParamsByCategory(cat domain.ParamCategory) []domain.GovernableParam {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []domain.GovernableParam
	for _, p := range e.params {
		if p.Category == cat {
			result = append(result, *p)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Key < result[j].Key
	})
	return result
}

// ChangeParam attempts to change a parameter's value.
// This validates the protection level and records who changed it.
func (e *Engine) ChangeParam(key, newValue, proposalID string, votePercentage float64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	p, ok := e.params[key]
	if !ok {
		return fmt.Errorf("parameter %q not found", key)
	}

	// Check protection level
	if p.Protection == domain.ProtectionImmutable {
		return domain.ErrParameterProtected
	}

	requiredMajority := p.Protection.RequiredMajority()
	if votePercentage < requiredMajority {
		return domain.ErrDemocracyQuorumFailed
	}

	p.CurrentValue = newValue
	p.LastChanged = e.now()
	p.ChangedBy = proposalID

	return nil
}

// ParamCount returns the total number of registered parameters.
func (e *Engine) ParamCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.params)
}

// ═══════════════════════════════════════════════════════════════════════════
// Council Elections
// ═══════════════════════════════════════════════════════════════════════════

// StartElection opens a council election for a continent.
func (e *Engine) StartElection(continent domain.ContinentID, eligibleVoters int64) (string, error) {
	if !continent.IsValid() {
		return "", fmt.Errorf("invalid continent: %q", continent)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Check for existing active election on this continent
	for _, el := range e.elections {
		if el.Continent == continent && el.Status == "open" {
			return "", fmt.Errorf("election already open for %s", continent)
		}
	}

	now := e.now()
	id := fmt.Sprintf("election-%s-%d", continent, now.Unix())

	election := &domain.CouncilElection{
		ID:             id,
		Continent:      continent,
		Candidates:     []domain.CouncilCandidate{},
		EligibleVoters: eligibleVoters,
		Status:         "open",
		OpensAt:        now,
		ClosesAt:       now.AddDate(0, 0, e.config.ElectionDurationDays),
	}

	e.elections[id] = election
	return id, nil
}

// AddCandidate adds a node as a candidate in an election.
func (e *Engine) AddCandidate(electionID, nodeID, platform string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	el, ok := e.elections[electionID]
	if !ok {
		return fmt.Errorf("election %q not found", electionID)
	}

	if el.Status != "open" {
		return fmt.Errorf("election is not open")
	}

	// Check for duplicate candidate
	for _, c := range el.Candidates {
		if c.NodeID == nodeID {
			return fmt.Errorf("node %q is already a candidate", nodeID)
		}
	}

	el.Candidates = append(el.Candidates, domain.CouncilCandidate{
		NodeID:   nodeID,
		Platform: platform,
	})

	return nil
}

// CastVote records a vote for a candidate in an election.
func (e *Engine) CastVote(electionID, candidateNodeID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	el, ok := e.elections[electionID]
	if !ok {
		return fmt.Errorf("election %q not found", electionID)
	}

	if el.Status != "open" {
		return domain.ErrVotingClosed
	}

	// Check election hasn't expired
	if e.now().After(el.ClosesAt) {
		el.Status = "closed"
		return domain.ErrVotingClosed
	}

	// Find candidate and add vote
	for i := range el.Candidates {
		if el.Candidates[i].NodeID == candidateNodeID {
			el.Candidates[i].VotesFor++
			el.TotalVotes++
			return nil
		}
	}

	return fmt.Errorf("candidate %q not found in election %q", candidateNodeID, electionID)
}

// CertifyElection closes an election and seats the winner on the council.
func (e *Engine) CertifyElection(electionID string) (*domain.CouncilMember, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	el, ok := e.elections[electionID]
	if !ok {
		return nil, fmt.Errorf("election %q not found", electionID)
	}

	// Check minimum turnout
	if !el.IsValidElection() {
		el.Status = "closed"
		return nil, domain.ErrCouncilElectionInvalid
	}

	// Find winner (highest votes)
	if len(el.Candidates) == 0 {
		el.Status = "closed"
		return nil, domain.ErrCouncilElectionInvalid
	}

	var winner domain.CouncilCandidate
	for _, c := range el.Candidates {
		if c.VotesFor > winner.VotesFor {
			winner = c
		}
	}

	// Seat on council
	now := e.now()
	member := &domain.CouncilMember{
		NodeID:      winner.NodeID,
		Continent:   el.Continent,
		VotesFor:    winner.VotesFor,
		ElectedAt:   now,
		TermExpires: now.AddDate(0, e.config.CouncilTermMonths, 0),
	}

	e.council[el.Continent] = member
	el.Status = "certified"

	return member, nil
}

// GetCouncil returns all current council members.
func (e *Engine) GetCouncil() []domain.CouncilMember {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]domain.CouncilMember, 0, len(e.council))
	for _, m := range e.council {
		result = append(result, *m)
	}
	return result
}

// ActiveCouncilCount returns how many council members have active terms.
func (e *Engine) ActiveCouncilCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()

	now := e.now()
	count := 0
	for _, m := range e.council {
		if now.Before(m.TermExpires) {
			count++
		}
	}
	return count
}

// GetElection returns an election by ID.
func (e *Engine) GetElection(id string) (domain.CouncilElection, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	el, ok := e.elections[id]
	if !ok {
		return domain.CouncilElection{}, fmt.Errorf("election %q not found", id)
	}
	return *el, nil
}

// ═══════════════════════════════════════════════════════════════════════════
// Open-Source Compliance
// ═══════════════════════════════════════════════════════════════════════════

// UpdateCompliance records the latest open-source compliance check.
func (e *Engine) UpdateCompliance(c domain.OpenSourceCompliance) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c.LastAuditDate = e.now()
	e.compliance = c
}

// Compliance returns the current open-source compliance state.
func (e *Engine) Compliance() domain.OpenSourceCompliance {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.compliance
}

// IsCompliant returns whether the network passes all open-source checks.
func (e *Engine) IsCompliant() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.compliance.IsCompliant()
}

// ═══════════════════════════════════════════════════════════════════════════
// Gate Check Support
// ═══════════════════════════════════════════════════════════════════════════

// GateCheck reports whether the democracy system meets Phase 7 targets.
func (e *Engine) GateCheck() (openSource bool, councilActive int, paramsGoverned int) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	now := e.now()
	council := 0
	for _, m := range e.council {
		if now.Before(m.TermExpires) {
			council++
		}
	}

	return e.compliance.IsCompliant(), council, len(e.params)
}

// ═══════════════════════════════════════════════════════════════════════════
// Default Parameters
// ═══════════════════════════════════════════════════════════════════════════

// registerDefaultParams registers all default governable parameters.
// These are the parameters the community can vote to change.
func (e *Engine) registerDefaultParams() {
	defaults := []domain.GovernableParam{
		// Economic parameters
		{Key: "earning_rate_base", Category: domain.ParamCategoryEconomic, CurrentValue: "1.0", Description: "Base credit earning rate per task", Protection: domain.ProtectionElevated},
		{Key: "earning_cap_hourly", Category: domain.ParamCategoryEconomic, CurrentValue: "100", Description: "Maximum credits earnable per hour", Protection: domain.ProtectionElevated},
		{Key: "streak_bonus_cap", Category: domain.ParamCategoryEconomic, CurrentValue: "0.50", Description: "Maximum streak bonus multiplier", Protection: domain.ProtectionNormal},

		// Access parameters
		{Key: "free_tier_daily_limit", Category: domain.ParamCategoryAccess, CurrentValue: "100", Description: "Free tier daily inference limit", Protection: domain.ProtectionElevated},
		{Key: "education_tier_enabled", Category: domain.ParamCategoryAccess, CurrentValue: "true", Description: "Whether education tier is available", Protection: domain.ProtectionCritical},
		{Key: "pro_tier_daily_limit", Category: domain.ParamCategoryAccess, CurrentValue: "10000", Description: "Pro tier daily inference limit", Protection: domain.ProtectionNormal},

		// Technical parameters
		{Key: "task_timeout_seconds", Category: domain.ParamCategoryTechnical, CurrentValue: "300", Description: "Maximum task execution time", Protection: domain.ProtectionNormal},
		{Key: "heartbeat_interval_seconds", Category: domain.ParamCategoryTechnical, CurrentValue: "10", Description: "Node heartbeat interval", Protection: domain.ProtectionNormal},
		{Key: "gossip_interval_ms", Category: domain.ParamCategoryTechnical, CurrentValue: "1000", Description: "SWIM gossip probe interval", Protection: domain.ProtectionNormal},

		// Security parameters
		{Key: "quarantine_duration_hours", Category: domain.ParamCategorySecurity, CurrentValue: "1", Description: "Default quarantine duration for failing nodes", Protection: domain.ProtectionElevated},
		{Key: "min_reputation_threshold", Category: domain.ParamCategorySecurity, CurrentValue: "0.3", Description: "Minimum reputation to accept tasks", Protection: domain.ProtectionCritical},

		// Network parameters
		{Key: "max_routing_hops", Category: domain.ParamCategoryNetwork, CurrentValue: "3", Description: "Maximum inter-continent routing hops", Protection: domain.ProtectionNormal},
		{Key: "replication_factor", Category: domain.ParamCategoryNetwork, CurrentValue: "3", Description: "Task result verification redundancy", Protection: domain.ProtectionElevated},

		// Immutable parameters (cannot be changed by vote — hardcoded safety)
		{Key: "open_source_license", Category: domain.ParamCategorySecurity, CurrentValue: "MIT", Description: "Core code license — cannot be changed", Protection: domain.ProtectionImmutable},
		{Key: "max_quarantine_ban_days", Category: domain.ParamCategorySecurity, CurrentValue: "30", Description: "Maximum ban duration — cannot be increased", Protection: domain.ProtectionImmutable},
	}

	now := e.now()
	for _, p := range defaults {
		p.LastChanged = now
		e.params[p.Key] = &domain.GovernableParam{
			Key:          p.Key,
			Category:     p.Category,
			CurrentValue: p.CurrentValue,
			Description:  p.Description,
			Protection:   p.Protection,
			LastChanged:  p.LastChanged,
		}
	}
}
