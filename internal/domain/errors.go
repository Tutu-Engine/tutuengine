package domain

import "errors"

// ─── Sentinel Errors ────────────────────────────────────────────────────────
// Domain errors are pure — no infrastructure dependency.

var (
	// Model errors
	ErrModelNotFound  = errors.New("model not found")
	ErrModelExists    = errors.New("model already exists")
	ErrModelCorrupted = errors.New("model integrity check failed")
	ErrModelTooLarge  = errors.New("insufficient storage for model")

	// Inference errors
	ErrInferenceTimeout = errors.New("inference request timed out")
	ErrModelNotLoaded   = errors.New("model not loaded in memory")
	ErrContextExceeded  = errors.New("context length exceeded")

	// TuTufile errors
	ErrNoFromDirective  = errors.New("TuTufile must include FROM directive")
	ErrInvalidDirective = errors.New("invalid TuTufile directive")
	ErrBaseModelMissing = errors.New("base model specified in FROM not found")

	// Network errors (prepared for Phase 1)
	ErrOffline      = errors.New("no internet connection available")
	ErrRegistryDown = errors.New("model registry is unreachable")

	// Pool errors
	ErrPoolExhausted = errors.New("model pool memory exhausted — all models in use")

	// Phase 3: Scheduler back-pressure errors
	ErrBackPressureSoft   = errors.New("back-pressure: soft limit — spot tasks rejected")
	ErrBackPressureMedium = errors.New("back-pressure: medium limit — only realtime accepted")
	ErrBackPressureHard   = errors.New("back-pressure: hard limit — all tasks rejected")

	// Phase 3: Circuit breaker errors
	ErrCircuitOpen     = errors.New("circuit breaker is open — service unavailable")
	ErrCircuitHalfOpen = errors.New("circuit breaker is half-open — limited traffic")

	// Phase 3: Quarantine errors
	ErrNodeQuarantined = errors.New("node is quarantined — cannot accept tasks")

	// Phase 3: NAT traversal errors
	ErrNATTraversalFailed = errors.New("NAT traversal failed — no direct connection possible")
	ErrTURNUnavailable    = errors.New("TURN relay server unavailable")

	// Phase 4: Fine-tuning errors
	ErrFineTuneJobNotFound = errors.New("fine-tune job not found")
	ErrFineTuneInProgress  = errors.New("fine-tune job already running")
	ErrInsufficientNodes   = errors.New("not enough capable nodes for fine-tuning")
	ErrGradientMismatch    = errors.New("gradient dimensions do not match")
	ErrCheckpointMissing   = errors.New("checkpoint not available")
	ErrEpochTimeout        = errors.New("epoch exceeded time limit")

	// Phase 4: Marketplace errors
	ErrListingNotFound   = errors.New("marketplace listing not found")
	ErrAlreadyPublished  = errors.New("model already published")
	ErrSelfReview        = errors.New("cannot review your own model")
	ErrDuplicateReview   = errors.New("already reviewed this model")
	ErrModelUnverified   = errors.New("model has not passed quality checks")
	ErrInsufficientFunds = errors.New("insufficient credits for download")

	// Phase 4: P2P distribution errors
	ErrChunkCorrupted    = errors.New("chunk integrity check failed")
	ErrManifestInvalid   = errors.New("manifest signature invalid")
	ErrNoPeersAvailable  = errors.New("no peers have required chunk")
	ErrTransferCancelled = errors.New("transfer was cancelled")

	// Phase 5: Federation errors
	ErrFederationNotFound  = errors.New("federation not found")
	ErrFederationFull      = errors.New("federation has reached maximum member count")
	ErrAlreadyFederated    = errors.New("node already belongs to a federation")
	ErrNotFederated        = errors.New("node is not a member of this federation")
	ErrAdminCannotLeave    = errors.New("admin cannot leave — transfer admin first or dissolve")
	ErrFederationSuspended = errors.New("federation is suspended — no new members allowed")

	// Phase 5: Governance errors
	ErrProposalNotFound             = errors.New("governance proposal not found")
	ErrVotingClosed                 = errors.New("voting period has ended")
	ErrInsufficientCreditsToPropose = errors.New("insufficient credits to submit a proposal")
	ErrQuorumNotReached             = errors.New("quorum not reached — proposal cannot pass")
	ErrAlreadyVoted                 = errors.New("already cast a vote on this proposal")
	ErrTooManyActiveProposals       = errors.New("maximum active proposals reached")

	// Phase 5: Reputation errors
	ErrNodeNotRegistered = errors.New("node not registered in reputation system")
	ErrReputationTooLow  = errors.New("reputation score below required threshold")

	// Phase 5: Anomaly detection errors
	ErrNodeAnomalous  = errors.New("node exhibits anomalous behavior")
	ErrThreatDetected = errors.New("node flagged in threat intelligence feed")

	// Phase 6: ML scheduler errors
	ErrMLSchedulerNoCandidate = errors.New("ML scheduler: no candidate nodes available")
	ErrMLSchedulerColdStart   = errors.New("ML scheduler: insufficient observations for reliable selection")

	// Phase 6: Predictive scaling errors
	ErrScalingCooldown      = errors.New("scaling decision blocked by cooldown period")
	ErrCapacityAtMax        = errors.New("node capacity already at maximum")
	ErrCapacityAtMin        = errors.New("node capacity already at minimum")
	ErrForecastInsufficient = errors.New("insufficient observations for forecast")

	// Phase 6: Self-healing errors
	ErrIncidentNotFound      = errors.New("healing incident not found")
	ErrIncidentAlreadyActive = errors.New("node already has an active incident")
	ErrNoRunbook             = errors.New("no runbook available for this failure type")
	ErrMaxIncidents          = errors.New("maximum active incidents reached")
	ErrRemediationExhausted  = errors.New("all remediation attempts exhausted — escalated")

	// Phase 6: Network intelligence errors
	ErrModelNotTracked     = errors.New("model not tracked by intelligence optimizer")
	ErrNoPlacementData     = errors.New("insufficient data for placement optimization")
	ErrRetirementProtected = errors.New("model is pinned and cannot be retired")

	// Phase 7: Planetary-scale errors
	ErrContinentUnavailable = errors.New("no reachable regions on target continent")
	ErrGlobalQuorumLost     = errors.New("global quorum lost — majority of continents unreachable")
	ErrExabyteStorageFull   = errors.New("planetary model distribution storage at capacity")
	ErrRoutingLoopDetected  = errors.New("routing loop detected in continental mesh")

	// Phase 7: Universal access tier errors
	ErrFreeTierExhausted   = errors.New("free tier daily quota exhausted — resets at midnight UTC")
	ErrEduTierUnverified   = errors.New("education tier requires verified student/researcher status")
	ErrTierDowngrade       = errors.New("cannot downgrade tier while active tasks are pending")
	ErrQuotaExceeded       = errors.New("access tier quota exceeded")

	// Phase 7: Economic flywheel errors
	ErrEconomyUnsustainable   = errors.New("economic flywheel health below sustainability threshold")
	ErrNetworkEffectStalled   = errors.New("network effect growth has stalled below minimum rate")
	ErrContributionDeficit    = errors.New("global contribution deficit — more consumption than supply")

	// Phase 7: AI democracy errors
	ErrDemocracyQuorumFailed    = errors.New("democratic quorum not reached for global parameter change")
	ErrCouncilElectionInvalid   = errors.New("council election invalid — insufficient voter turnout")
	ErrParameterProtected       = errors.New("parameter is protected — requires supermajority (67%+)")
	ErrOpenSourceViolation      = errors.New("proposed change violates open-source compliance policy")
)
