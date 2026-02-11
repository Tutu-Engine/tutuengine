package democracy

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
// Engine Construction
// ═══════════════════════════════════════════════════════════════════════════

func TestNewEngine(t *testing.T) {
	e := NewEngine(DefaultConfig())
	if e == nil {
		t.Fatal("expected non-nil Engine")
	}

	// Should have default params pre-registered (15 params total)
	count := e.ParamCount()
	if count != 15 {
		t.Fatalf("expected 15 default params, got %d", count)
	}
}

func TestNewEngine_DefaultParamCount(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	params := e.ListParams()
	if len(params) < 10 {
		t.Fatalf("expected at least 10 default params, got %d", len(params))
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Parameter Management Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestRegisterParam(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	err := e.RegisterParam(domain.GovernableParam{
		Key:          "custom_param",
		Category:     domain.ParamCategoryTechnical,
		CurrentValue: "42",
		Description:  "A custom parameter",
		Protection:   domain.ProtectionNormal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p, err := e.GetParam("custom_param")
	if err != nil {
		t.Fatalf("unexpected error getting param: %v", err)
	}
	if p.CurrentValue != "42" {
		t.Fatalf("expected value '42', got %q", p.CurrentValue)
	}
}

func TestRegisterParam_EmptyKey(t *testing.T) {
	e := NewEngine(DefaultConfig())
	err := e.RegisterParam(domain.GovernableParam{Key: ""})
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestGetParam_NotFound(t *testing.T) {
	e := NewEngine(DefaultConfig())
	_, err := e.GetParam("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent param")
	}
}

func TestListParamsByCategory(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	economic := e.ListParamsByCategory(domain.ParamCategoryEconomic)
	if len(economic) != 3 {
		t.Fatalf("expected 3 economic params, got %d", len(economic))
	}

	access := e.ListParamsByCategory(domain.ParamCategoryAccess)
	if len(access) != 3 {
		t.Fatalf("expected 3 access params, got %d", len(access))
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Parameter Change Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestChangeParam_Normal(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	// gossip_interval_ms is ProtectionNormal (>50% needed)
	err := e.ChangeParam("gossip_interval_ms", "2000", "proposal-1", 0.55)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p, _ := e.GetParam("gossip_interval_ms")
	if p.CurrentValue != "2000" {
		t.Fatalf("expected '2000', got %q", p.CurrentValue)
	}
	if p.ChangedBy != "proposal-1" {
		t.Fatalf("expected 'proposal-1', got %q", p.ChangedBy)
	}
}

func TestChangeParam_InsufficientVotes(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	// gossip_interval_ms is Normal (>50% needed), try with 40%
	err := e.ChangeParam("gossip_interval_ms", "2000", "proposal-1", 0.40)
	if err != domain.ErrDemocracyQuorumFailed {
		t.Fatalf("expected ErrDemocracyQuorumFailed, got: %v", err)
	}
}

func TestChangeParam_Immutable(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	// open_source_license is Immutable — cannot be changed even with 100%
	err := e.ChangeParam("open_source_license", "GPL", "proposal-evil", 1.0)
	if err != domain.ErrParameterProtected {
		t.Fatalf("expected ErrParameterProtected, got: %v", err)
	}
}

func TestChangeParam_Elevated(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	// free_tier_daily_limit is Elevated (>60% needed)
	// Try with 55% — should fail
	err := e.ChangeParam("free_tier_daily_limit", "200", "prop-1", 0.55)
	if err != domain.ErrDemocracyQuorumFailed {
		t.Fatalf("expected ErrDemocracyQuorumFailed at 55%%, got: %v", err)
	}

	// Try with 65% — should succeed
	err = e.ChangeParam("free_tier_daily_limit", "200", "prop-1", 0.65)
	if err != nil {
		t.Fatalf("unexpected error at 65%%: %v", err)
	}
}

func TestChangeParam_Critical(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	// education_tier_enabled is Critical (>67% needed)
	err := e.ChangeParam("education_tier_enabled", "false", "prop-bad", 0.65)
	if err != domain.ErrDemocracyQuorumFailed {
		t.Fatalf("expected ErrDemocracyQuorumFailed at 65%%, got: %v", err)
	}

	err = e.ChangeParam("education_tier_enabled", "false", "prop-bad", 0.70)
	if err != nil {
		t.Fatalf("unexpected error at 70%%: %v", err)
	}
}

func TestChangeParam_NotFound(t *testing.T) {
	e := NewEngine(DefaultConfig())
	err := e.ChangeParam("nonexistent", "v", "p", 1.0)
	if err == nil {
		t.Fatal("expected error for non-existent param")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Council Election Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestStartElection(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	id, err := e.StartElection(domain.ContinentNorthAmerica, 100_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty election ID")
	}

	el, err := e.GetElection(id)
	if err != nil {
		t.Fatalf("unexpected error getting election: %v", err)
	}
	if el.Status != "open" {
		t.Fatalf("expected open status, got %q", el.Status)
	}
}

func TestStartElection_InvalidContinent(t *testing.T) {
	e := NewEngine(DefaultConfig())
	_, err := e.StartElection("xx", 100)
	if err == nil {
		t.Fatal("expected error for invalid continent")
	}
}

func TestStartElection_DuplicateOpen(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	_, _ = e.StartElection(domain.ContinentEurope, 50000)
	_, err := e.StartElection(domain.ContinentEurope, 50000)
	if err == nil {
		t.Fatal("expected error for duplicate open election")
	}
}

func TestAddCandidate(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	id, _ := e.StartElection(domain.ContinentNorthAmerica, 100_000)

	err := e.AddCandidate(id, "node-alice", "More compute for all!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Duplicate candidate should fail
	err = e.AddCandidate(id, "node-alice", "Different platform")
	if err == nil {
		t.Fatal("expected error for duplicate candidate")
	}
}

func TestCastVote(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	id, _ := e.StartElection(domain.ContinentNorthAmerica, 100)
	_ = e.AddCandidate(id, "node-alice", "Platform A")
	_ = e.AddCandidate(id, "node-bob", "Platform B")

	// Cast votes
	for i := 0; i < 7; i++ {
		if err := e.CastVote(id, "node-alice"); err != nil {
			t.Fatalf("vote %d: %v", i, err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := e.CastVote(id, "node-bob"); err != nil {
			t.Fatalf("vote %d: %v", i, err)
		}
	}

	el, _ := e.GetElection(id)
	if el.TotalVotes != 10 {
		t.Fatalf("expected 10 total votes, got %d", el.TotalVotes)
	}
	if el.TurnoutPct() != 10.0 {
		t.Fatalf("expected 10%% turnout, got %f%%", el.TurnoutPct())
	}
}

func TestCastVote_InvalidCandidate(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	id, _ := e.StartElection(domain.ContinentAsia, 1000)
	err := e.CastVote(id, "nonexistent-candidate")
	if err == nil {
		t.Fatal("expected error for non-existent candidate")
	}
}

func TestCastVote_ElectionExpired(t *testing.T) {
	e := NewEngine(DefaultConfig())
	now := fixedTime()
	e.now = func() time.Time { return now }

	id, _ := e.StartElection(domain.ContinentAfrica, 1000)
	_ = e.AddCandidate(id, "node-x", "Platform")

	// Fast-forward past election close (14 days + 1 hour)
	now = now.AddDate(0, 0, 15)

	err := e.CastVote(id, "node-x")
	if err != domain.ErrVotingClosed {
		t.Fatalf("expected ErrVotingClosed, got: %v", err)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Election Certification Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestCertifyElection_Success(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	id, _ := e.StartElection(domain.ContinentEurope, 100)
	_ = e.AddCandidate(id, "node-alice", "Platform A")
	_ = e.AddCandidate(id, "node-bob", "Platform B")

	// Alice gets 7 votes, Bob gets 5 — 12% turnout (above 10% minimum)
	for i := 0; i < 7; i++ {
		_ = e.CastVote(id, "node-alice")
	}
	for i := 0; i < 5; i++ {
		_ = e.CastVote(id, "node-bob")
	}

	member, err := e.CertifyElection(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if member.NodeID != "node-alice" {
		t.Fatalf("expected alice to win, got %q", member.NodeID)
	}
	if member.Continent != domain.ContinentEurope {
		t.Fatalf("expected EU, got %s", member.Continent)
	}
	if member.VotesFor != 7 {
		t.Fatalf("expected 7 votes, got %d", member.VotesFor)
	}

	// Check council
	council := e.GetCouncil()
	if len(council) != 1 {
		t.Fatalf("expected 1 council member, got %d", len(council))
	}
	if e.ActiveCouncilCount() != 1 {
		t.Fatalf("expected 1 active council member, got %d", e.ActiveCouncilCount())
	}
}

func TestCertifyElection_InsufficientTurnout(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	id, _ := e.StartElection(domain.ContinentAsia, 1000)
	_ = e.AddCandidate(id, "node-x", "Platform")

	// Only 5 votes out of 1000 = 0.5% turnout (needs 10%)
	for i := 0; i < 5; i++ {
		_ = e.CastVote(id, "node-x")
	}

	_, err := e.CertifyElection(id)
	if err != domain.ErrCouncilElectionInvalid {
		t.Fatalf("expected ErrCouncilElectionInvalid, got: %v", err)
	}
}

func TestCertifyElection_NoCandidates(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	id, _ := e.StartElection(domain.ContinentOceania, 100)
	_, err := e.CertifyElection(id)
	if err != domain.ErrCouncilElectionInvalid {
		t.Fatalf("expected ErrCouncilElectionInvalid, got: %v", err)
	}
}

func TestCertifyElection_NotFound(t *testing.T) {
	e := NewEngine(DefaultConfig())
	_, err := e.CertifyElection("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent election")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Open-Source Compliance Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestCompliance_Full(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	e.UpdateCompliance(domain.OpenSourceCompliance{
		AllCoreCodeMIT:     true,
		NoProprietaryDeps:  true,
		CommunityGoverned:  true,
		TransparencyLogURL: "https://example.com/audit",
	})

	if !e.IsCompliant() {
		t.Fatal("expected compliant")
	}

	c := e.Compliance()
	if !c.AllCoreCodeMIT {
		t.Fatal("expected MIT license")
	}
}

func TestCompliance_NonCompliant(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	e.UpdateCompliance(domain.OpenSourceCompliance{
		AllCoreCodeMIT:    true,
		NoProprietaryDeps: false, // Proprietary dependency!
		CommunityGoverned: true,
	})

	if e.IsCompliant() {
		t.Fatal("expected non-compliant due to proprietary deps")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Gate Check Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestGateCheck(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.now = fixedTime

	// Set up compliance
	e.UpdateCompliance(domain.OpenSourceCompliance{
		AllCoreCodeMIT:    true,
		NoProprietaryDeps: true,
		CommunityGoverned: true,
	})

	// Elect a council member
	id, _ := e.StartElection(domain.ContinentNorthAmerica, 100)
	_ = e.AddCandidate(id, "node-leader", "For the people!")
	for i := 0; i < 15; i++ {
		_ = e.CastVote(id, "node-leader")
	}
	_, _ = e.CertifyElection(id)

	openSource, council, params := e.GateCheck()
	if !openSource {
		t.Fatal("expected open source compliant")
	}
	if council != 1 {
		t.Fatalf("expected 1 council member, got %d", council)
	}
	if params < 10 {
		t.Fatalf("expected at least 10 governed params, got %d", params)
	}
}
