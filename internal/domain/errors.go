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
)
