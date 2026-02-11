// Package governance implements credit-weighted voting on network parameters.
//
// Any node with credits can create proposals like "change earning rates" or
// "add new model category." Other nodes vote, weighted by their credit balance.
// With 30% quorum and majority approval, changes auto-apply.
//
// Architecture Part X — Governance token for community decisions.
// Phase 5 spec: "Credit-weighted voting on network parameters."
package governance

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ─── Constants ──────────────────────────────────────────────────────────────

const (
	// DefaultQuorumPct is the percentage of total credits that must vote.
	// Phase 5 spec: "30% of total credits must vote."
	DefaultQuorumPct = 30

	// DefaultVotingDuration is how long a proposal stays open for voting.
	DefaultVotingDuration = 7 * 24 * time.Hour // 1 week

	// MinProposalCredits prevents spam — must have some credits to propose.
	MinProposalCredits = 100

	// MaxActiveProposals limits concurrent proposals.
	MaxActiveProposals = 50
)

// ─── Types ──────────────────────────────────────────────────────────────────

// ProposalStatus represents the lifecycle of a proposal.
type ProposalStatus int

const (
	PropDraft    ProposalStatus = iota // Created but not yet open
	PropActive                         // Open for voting
	PropPassed                         // Quorum met + majority approved
	PropRejected                       // Quorum met + majority rejected
	PropExpired                        // Voting period ended without quorum
	PropExecuted                       // Passed and auto-applied
	PropCancelled                      // Cancelled by author
)

// String returns a human-readable status.
func (s ProposalStatus) String() string {
	switch s {
	case PropDraft:
		return "DRAFT"
	case PropActive:
		return "ACTIVE"
	case PropPassed:
		return "PASSED"
	case PropRejected:
		return "REJECTED"
	case PropExpired:
		return "EXPIRED"
	case PropExecuted:
		return "EXECUTED"
	case PropCancelled:
		return "CANCELLED"
	default:
		return "UNKNOWN"
	}
}

// ProposalCategory defines what a proposal can change.
type ProposalCategory int

const (
	CatEarningRate   ProposalCategory = iota // Change credit earning rate
	CatModelPolicy                            // Add/remove model categories
	CatSLAPricing                             // Adjust SLA tier pricing
	CatNetworkParam                           // General network parameters
	CatFederation                             // Federation policy changes
	CatSecurity                               // Security policy changes
)

// String returns the category name.
func (c ProposalCategory) String() string {
	switch c {
	case CatEarningRate:
		return "EARNING_RATE"
	case CatModelPolicy:
		return "MODEL_POLICY"
	case CatSLAPricing:
		return "SLA_PRICING"
	case CatNetworkParam:
		return "NETWORK_PARAM"
	case CatFederation:
		return "FEDERATION"
	case CatSecurity:
		return "SECURITY"
	default:
		return "UNKNOWN"
	}
}

// VoteChoice represents a voter's decision.
type VoteChoice int

const (
	VoteFor     VoteChoice = iota // Support the proposal
	VoteAgainst                   // Oppose the proposal
	VoteAbstain                   // Counted for quorum but not for/against
)

// Proposal is a governance proposal that nodes vote on.
type Proposal struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Category    ProposalCategory `json:"category"`
	Author      string           `json:"author"`       // NodeID that created it
	Status      ProposalStatus   `json:"status"`
	ParamKey    string           `json:"param_key"`    // Config key to change
	ParamValue  string           `json:"param_value"`  // New value
	CreatedAt   time.Time        `json:"created_at"`
	OpenedAt    time.Time        `json:"opened_at"`    // When voting opened
	ClosedAt    time.Time        `json:"closed_at"`    // When voting closed
	ExpiresAt   time.Time        `json:"expires_at"`   // Voting deadline
}

// Vote records a single node's vote, weighted by their credit balance.
type Vote struct {
	ProposalID string     `json:"proposal_id"`
	NodeID     string     `json:"node_id"`
	Choice     VoteChoice `json:"choice"`
	Weight     int64      `json:"weight"` // Credit balance at time of vote
	CastAt     time.Time  `json:"cast_at"`
}

// VoteTally summarizes the current state of voting on a proposal.
type VoteTally struct {
	ProposalID    string  `json:"proposal_id"`
	ForWeight     int64   `json:"for_weight"`
	AgainstWeight int64   `json:"against_weight"`
	AbstainWeight int64   `json:"abstain_weight"`
	TotalWeight   int64   `json:"total_weight"`  // Sum of all votes
	QuorumWeight  int64   `json:"quorum_weight"` // Required for quorum
	VoterCount    int     `json:"voter_count"`
	QuorumReached bool    `json:"quorum_reached"`
	ApprovalPct   float64 `json:"approval_pct"` // For / (For + Against)
}

// GovernanceStats provides an overview of governance activity.
type GovernanceStats struct {
	TotalProposals  int `json:"total_proposals"`
	ActiveProposals int `json:"active_proposals"`
	PassedProposals int `json:"passed_proposals"`
	RejectedProposals int `json:"rejected_proposals"`
	ExpiredProposals int `json:"expired_proposals"`
	ExecutedProposals int `json:"executed_proposals"`
	TotalVotesCast  int `json:"total_votes_cast"`
}

// ─── Configuration ──────────────────────────────────────────────────────────

// EngineConfig configures the governance engine.
type EngineConfig struct {
	QuorumPct      int           // % of total credits needed to vote (default 30)
	VotingDuration time.Duration // How long polls stay open
	MinCredits     int64         // Minimum credits to create a proposal
}

// DefaultEngineConfig returns Phase 5 defaults.
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		QuorumPct:      DefaultQuorumPct,
		VotingDuration: DefaultVotingDuration,
		MinCredits:     MinProposalCredits,
	}
}

// ─── Engine ─────────────────────────────────────────────────────────────────

// Engine implements the governance system.
// Thread-safe via RWMutex.
type Engine struct {
	mu          sync.RWMutex
	config      EngineConfig
	proposals   map[string]*Proposal         // proposalID → Proposal
	votes       map[string]map[string]*Vote  // proposalID → nodeID → Vote
	totalCredits int64                        // Total credits in network (for quorum calc)

	// now is a function that returns the current time — injectable for testing.
	now func() time.Time
}

// NewEngine creates a governance engine.
func NewEngine(cfg EngineConfig) *Engine {
	return &Engine{
		config:    cfg,
		proposals: make(map[string]*Proposal),
		votes:     make(map[string]map[string]*Vote),
		now:       time.Now,
	}
}

// SetTotalCredits updates the total credit supply (used for quorum calculation).
// Should be called periodically with the current network-wide credit total.
func (e *Engine) SetTotalCredits(total int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.totalCredits = total
}

// ─── Proposal Lifecycle ─────────────────────────────────────────────────────

// CreateProposal creates a new governance proposal.
// authorCredits is the author's current credit balance — must meet minimum.
func (e *Engine) CreateProposal(title, description string, category ProposalCategory, author string, authorCredits int64, paramKey, paramValue string) (*Proposal, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	title = strings.TrimSpace(title)
	if title == "" {
		return nil, errors.New("proposal title is required")
	}
	if author == "" {
		return nil, errors.New("proposal author is required")
	}
	if authorCredits < e.config.MinCredits {
		return nil, fmt.Errorf("need at least %d credits to propose (have %d)", e.config.MinCredits, authorCredits)
	}

	// Check active proposal limit
	activeCount := 0
	for _, p := range e.proposals {
		if p.Status == PropActive || p.Status == PropDraft {
			activeCount++
		}
	}
	if activeCount >= MaxActiveProposals {
		return nil, errors.New("maximum active proposals reached")
	}

	now := e.now()
	propID := fmt.Sprintf("prop-%d", now.UnixMilli())

	prop := &Proposal{
		ID:          propID,
		Title:       title,
		Description: description,
		Category:    category,
		Author:      author,
		Status:      PropDraft,
		ParamKey:    paramKey,
		ParamValue:  paramValue,
		CreatedAt:   now,
	}

	e.proposals[propID] = prop
	e.votes[propID] = make(map[string]*Vote)
	return prop, nil
}

// OpenProposal moves a draft proposal to active voting.
func (e *Engine) OpenProposal(propID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	prop, ok := e.proposals[propID]
	if !ok {
		return fmt.Errorf("proposal %s not found", propID)
	}
	if prop.Status != PropDraft {
		return fmt.Errorf("proposal %s is %s, expected DRAFT", propID, prop.Status)
	}

	now := e.now()
	prop.Status = PropActive
	prop.OpenedAt = now
	prop.ExpiresAt = now.Add(e.config.VotingDuration)
	return nil
}

// CancelProposal allows the author to cancel their proposal.
func (e *Engine) CancelProposal(propID, nodeID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	prop, ok := e.proposals[propID]
	if !ok {
		return fmt.Errorf("proposal %s not found", propID)
	}
	if prop.Author != nodeID {
		return errors.New("only the proposal author can cancel")
	}
	if prop.Status != PropDraft && prop.Status != PropActive {
		return fmt.Errorf("cannot cancel proposal in %s state", prop.Status)
	}

	prop.Status = PropCancelled
	prop.ClosedAt = e.now()
	return nil
}

// GetProposal returns a proposal by ID.
func (e *Engine) GetProposal(propID string) (*Proposal, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	prop, ok := e.proposals[propID]
	if !ok {
		return nil, fmt.Errorf("proposal %s not found", propID)
	}
	return prop, nil
}

// ListProposals returns proposals filtered by status.
// Pass nil to get all proposals.
func (e *Engine) ListProposals(status *ProposalStatus) []*Proposal {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]*Proposal, 0)
	for _, p := range e.proposals {
		if status == nil || p.Status == *status {
			result = append(result, p)
		}
	}

	// Sort by creation time, newest first
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

// ─── Voting ─────────────────────────────────────────────────────────────────

// CastVote records a node's vote on an active proposal.
// weight is the voter's current credit balance.
func (e *Engine) CastVote(propID, nodeID string, choice VoteChoice, weight int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	prop, ok := e.proposals[propID]
	if !ok {
		return fmt.Errorf("proposal %s not found", propID)
	}
	if prop.Status != PropActive {
		return fmt.Errorf("proposal %s is not active (status: %s)", propID, prop.Status)
	}

	now := e.now()
	if now.After(prop.ExpiresAt) {
		return errors.New("voting period has ended")
	}

	if weight <= 0 {
		return errors.New("vote weight must be positive")
	}

	// Check for duplicate vote — update if changed
	voters := e.votes[propID]
	if existing, ok := voters[nodeID]; ok {
		// Allow changing vote — subtract old weight, add new
		existing.Choice = choice
		existing.Weight = weight
		existing.CastAt = now
		return nil
	}

	voters[nodeID] = &Vote{
		ProposalID: propID,
		NodeID:     nodeID,
		Choice:     choice,
		Weight:     weight,
		CastAt:     now,
	}
	return nil
}

// Tally computes the current vote counts for a proposal.
func (e *Engine) Tally(propID string) (*VoteTally, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	_, ok := e.proposals[propID]
	if !ok {
		return nil, fmt.Errorf("proposal %s not found", propID)
	}

	return e.tallyLocked(propID), nil
}

// tallyLocked computes tally without acquiring lock (caller must hold lock).
func (e *Engine) tallyLocked(propID string) *VoteTally {
	votes := e.votes[propID]
	tally := &VoteTally{
		ProposalID: propID,
		VoterCount: len(votes),
	}

	for _, v := range votes {
		switch v.Choice {
		case VoteFor:
			tally.ForWeight += v.Weight
		case VoteAgainst:
			tally.AgainstWeight += v.Weight
		case VoteAbstain:
			tally.AbstainWeight += v.Weight
		}
		tally.TotalWeight += v.Weight
	}

	// Quorum calculation: 30% of total network credits
	if e.totalCredits > 0 {
		tally.QuorumWeight = e.totalCredits * int64(e.config.QuorumPct) / 100
	}
	tally.QuorumReached = tally.TotalWeight >= tally.QuorumWeight

	// Approval percentage (For / (For + Against)), excluding abstentions
	decided := tally.ForWeight + tally.AgainstWeight
	if decided > 0 {
		tally.ApprovalPct = float64(tally.ForWeight) / float64(decided) * 100
	}

	return tally
}

// ─── Resolution ─────────────────────────────────────────────────────────────

// ResolveExpired checks all active proposals and closes those past deadline.
// Call this periodically (e.g. every hour).
// Returns list of proposals that changed state.
func (e *Engine) ResolveExpired() []*Proposal {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := e.now()
	var changed []*Proposal

	for propID, prop := range e.proposals {
		if prop.Status != PropActive {
			continue
		}
		if now.Before(prop.ExpiresAt) {
			continue
		}

		// Voting period ended — tally results
		tally := e.tallyLocked(propID)
		prop.ClosedAt = now

		if !tally.QuorumReached {
			prop.Status = PropExpired
		} else if tally.ApprovalPct > 50 {
			prop.Status = PropPassed
		} else {
			prop.Status = PropRejected
		}

		changed = append(changed, prop)
	}

	return changed
}

// MarkExecuted marks a passed proposal as executed (config applied).
func (e *Engine) MarkExecuted(propID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	prop, ok := e.proposals[propID]
	if !ok {
		return fmt.Errorf("proposal %s not found", propID)
	}
	if prop.Status != PropPassed {
		return fmt.Errorf("proposal %s is %s, expected PASSED", propID, prop.Status)
	}

	prop.Status = PropExecuted
	return nil
}

// ─── Statistics ─────────────────────────────────────────────────────────────

// Stats returns aggregate governance metrics.
func (e *Engine) Stats() GovernanceStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var stats GovernanceStats
	stats.TotalProposals = len(e.proposals)

	for _, p := range e.proposals {
		switch p.Status {
		case PropActive, PropDraft:
			stats.ActiveProposals++
		case PropPassed:
			stats.PassedProposals++
		case PropRejected:
			stats.RejectedProposals++
		case PropExpired:
			stats.ExpiredProposals++
		case PropExecuted:
			stats.ExecutedProposals++
		}
	}

	for _, voters := range e.votes {
		stats.TotalVotesCast += len(voters)
	}

	return stats
}

// ProposalCount returns the total number of proposals.
func (e *Engine) ProposalCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.proposals)
}
