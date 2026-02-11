package domain

import "errors"

// ─── Sentinel Errors ────────────────────────────────────────────────────────
// Domain errors are pure — no infrastructure dependency.

var (
	// Model errors
	ErrModelNotFound    = errors.New("model not found")
	ErrModelExists      = errors.New("model already exists")
	ErrModelCorrupted   = errors.New("model integrity check failed")
	ErrModelTooLarge    = errors.New("insufficient storage for model")

	// Inference errors
	ErrInferenceTimeout = errors.New("inference request timed out")
	ErrModelNotLoaded   = errors.New("model not loaded in memory")
	ErrContextExceeded  = errors.New("context length exceeded")

	// TuTufile errors
	ErrNoFromDirective  = errors.New("TuTufile must include FROM directive")
	ErrInvalidDirective = errors.New("invalid TuTufile directive")
	ErrBaseModelMissing = errors.New("base model specified in FROM not found")

	// Network errors (prepared for Phase 1)
	ErrOffline          = errors.New("no internet connection available")
	ErrRegistryDown     = errors.New("model registry is unreachable")

	// Pool errors
	ErrPoolExhausted    = errors.New("model pool memory exhausted — all models in use")

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
	ErrFineTuneJobNotFound   = errors.New("fine-tune job not found")
	ErrFineTuneInProgress    = errors.New("fine-tune job already running")
	ErrInsufficientNodes     = errors.New("not enough capable nodes for fine-tuning")
	ErrGradientMismatch      = errors.New("gradient dimensions do not match")
	ErrCheckpointMissing     = errors.New("checkpoint not available")
	ErrEpochTimeout          = errors.New("epoch exceeded time limit")

	// Phase 4: Marketplace errors
	ErrListingNotFound    = errors.New("marketplace listing not found")
	ErrAlreadyPublished   = errors.New("model already published")
	ErrSelfReview         = errors.New("cannot review your own model")
	ErrDuplicateReview    = errors.New("already reviewed this model")
	ErrModelUnverified    = errors.New("model has not passed quality checks")
	ErrInsufficientFunds  = errors.New("insufficient credits for download")

	// Phase 4: P2P distribution errors
	ErrChunkCorrupted     = errors.New("chunk integrity check failed")
	ErrManifestInvalid    = errors.New("manifest signature invalid")
	ErrNoPeersAvailable   = errors.New("no peers have required chunk")
	ErrTransferCancelled  = errors.New("transfer was cancelled")

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
)
