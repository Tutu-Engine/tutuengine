// Package healing implements Phase 3 self-healing primitives.
// Architecture Part XVI: Circuit breakers, node quarantine, and automatic rollback.
//
// Circuit Breaker states:
//   - CLOSED  (normal) → errors exceed threshold → OPEN
//   - OPEN    (blocking) → after timeout → HALF_OPEN
//   - HALF_OPEN (probing) → probe succeeds → CLOSED, probe fails → OPEN
//
// Quarantine escalation:
//   - 3 failures → 1 hour quarantine
//   - Verification fail → 24 hour quarantine
//   - 3 quarantines in 7 days → 30 day ban
package healing

import (
	"fmt"
	"sync"
	"time"
)

// ═══════════════════════════════════════════════════════════════════════════
// Circuit Breaker
// ═══════════════════════════════════════════════════════════════════════════

// CBState represents the circuit breaker state.
type CBState int

const (
	CBClosed   CBState = iota // Normal operation — requests pass through
	CBOpen                    // Tripped — all requests rejected immediately
	CBHalfOpen                // Recovery probe — limited traffic allowed
)

// String returns a human-readable circuit breaker state.
func (s CBState) String() string {
	switch s {
	case CBClosed:
		return "CLOSED"
	case CBOpen:
		return "OPEN"
	case CBHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// CircuitBreakerConfig configures a circuit breaker.
type CircuitBreakerConfig struct {
	FailureThreshold int           // number of failures to trip (default 5)
	ResetTimeout     time.Duration // time in OPEN before trying HALF_OPEN (default 30s)
	HalfOpenMax      int           // max requests allowed in HALF_OPEN (default 3)
}

// DefaultCircuitBreakerConfig returns production defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		ResetTimeout:     30 * time.Second,
		HalfOpenMax:      3,
	}
}

// CircuitBreaker implements the circuit breaker pattern.
// Thread-safe for concurrent use.
type CircuitBreaker struct {
	mu          sync.Mutex
	name        string
	config      CircuitBreakerConfig
	state       CBState
	failures    int
	successes   int // successes in HALF_OPEN state
	lastFailure time.Time
	trippedAt   time.Time
	totalTrips  int
	now         func() time.Time // injectable clock for testing
}

// NewCircuitBreaker creates a circuit breaker with the given name and config.
func NewCircuitBreaker(name string, cfg CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		name:   name,
		config: cfg,
		state:  CBClosed,
		now:    time.Now,
	}
}

// Allow checks whether a request should be permitted.
// Returns an error if the circuit is open.
func (cb *CircuitBreaker) Allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CBClosed:
		return nil
	case CBOpen:
		// Check if it's time to transition to half-open
		if cb.now().Sub(cb.trippedAt) >= cb.config.ResetTimeout {
			cb.state = CBHalfOpen
			cb.successes = 0
			return nil
		}
		return fmt.Errorf("%s: %w", cb.name, ErrCircuitOpen)
	case CBHalfOpen:
		return nil
	}
	return nil
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CBHalfOpen:
		cb.successes++
		if cb.successes >= cb.config.HalfOpenMax {
			// Enough successful probes → close the circuit
			cb.state = CBClosed
			cb.failures = 0
			cb.successes = 0
		}
	case CBClosed:
		// Decay failures on success (simple reset)
		if cb.failures > 0 {
			cb.failures--
		}
	}
}

// RecordFailure records a failed request. May trip the breaker.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailure = cb.now()

	switch cb.state {
	case CBClosed:
		cb.failures++
		if cb.failures >= cb.config.FailureThreshold {
			cb.state = CBOpen
			cb.trippedAt = cb.now()
			cb.totalTrips++
		}
	case CBHalfOpen:
		// Any failure in half-open → back to open
		cb.state = CBOpen
		cb.trippedAt = cb.now()
		cb.totalTrips++
	}
}

// State returns the current circuit breaker state.
func (cb *CircuitBreaker) State() CBState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	// Auto-transition OPEN → HALF_OPEN if timeout has elapsed
	if cb.state == CBOpen && cb.now().Sub(cb.trippedAt) >= cb.config.ResetTimeout {
		cb.state = CBHalfOpen
		cb.successes = 0
	}
	return cb.state
}

// Snapshot returns a point-in-time view of the circuit breaker.
type Snapshot struct {
	Name       string    `json:"name"`
	State      CBState   `json:"state"`
	Failures   int       `json:"failures"`
	TotalTrips int       `json:"total_trips"`
	TrippedAt  time.Time `json:"tripped_at,omitempty"`
}

// Snapshot returns the current state snapshot.
func (cb *CircuitBreaker) Snapshot() Snapshot {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	// Read state directly (not via cb.State()) to avoid mutex re-entrance.
	st := cb.state
	if st == CBOpen && cb.now().Sub(cb.trippedAt) >= cb.config.ResetTimeout {
		st = CBHalfOpen
		cb.state = CBHalfOpen
		cb.successes = 0
	}
	return Snapshot{
		Name:       cb.name,
		State:      st,
		Failures:   cb.failures,
		TotalTrips: cb.totalTrips,
		TrippedAt:  cb.trippedAt,
	}
}

// Reset forces the circuit breaker back to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = CBClosed
	cb.failures = 0
	cb.successes = 0
}

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = fmt.Errorf("circuit breaker open")

// ═══════════════════════════════════════════════════════════════════════════
// Quarantine Manager
// ═══════════════════════════════════════════════════════════════════════════

// QuarantineReason explains why a node was quarantined.
type QuarantineReason string

const (
	QuarantineTaskFailures    QuarantineReason = "task_failures"     // 3+ task failures
	QuarantineVerificationFail QuarantineReason = "verification_fail" // result verification failed
	QuarantineAnomaly         QuarantineReason = "anomaly"           // behavioral anomaly detected
)

// QuarantineRecord tracks a quarantine period.
type QuarantineRecord struct {
	NodeID    string           `json:"node_id"`
	Reason    QuarantineReason `json:"reason"`
	StartedAt time.Time        `json:"started_at"`
	ExpiresAt time.Time        `json:"expires_at"`
	Released  bool             `json:"released"`
}

// IsActive reports whether the quarantine is currently in effect.
func (qr QuarantineRecord) IsActive(now time.Time) bool {
	return !qr.Released && now.Before(qr.ExpiresAt)
}

// QuarantineConfig sets quarantine durations.
type QuarantineConfig struct {
	FailureDuration      time.Duration // quarantine after 3 task failures (default 1h)
	VerificationDuration time.Duration // quarantine after verification fail (default 24h)
	BanDuration          time.Duration // ban after 3 quarantines in 7 days (default 30d)
	BanWindowDays        int           // rolling window for quarantine count (default 7)
	BanThreshold         int           // quarantines to trigger ban (default 3)
	FailureThreshold     int           // task failures to trigger quarantine (default 3)
}

// DefaultQuarantineConfig returns production defaults per Architecture Part XVI.
func DefaultQuarantineConfig() QuarantineConfig {
	return QuarantineConfig{
		FailureDuration:      1 * time.Hour,
		VerificationDuration: 24 * time.Hour,
		BanDuration:          30 * 24 * time.Hour,
		BanWindowDays:        7,
		BanThreshold:         3,
		FailureThreshold:     3,
	}
}

// QuarantineManager tracks node quarantines with escalation.
type QuarantineManager struct {
	mu       sync.Mutex
	config   QuarantineConfig
	records  map[string][]QuarantineRecord // nodeID → history
	failures map[string]int                // nodeID → consecutive failure count
	now      func() time.Time
}

// NewQuarantineManager creates a quarantine manager.
func NewQuarantineManager(cfg QuarantineConfig) *QuarantineManager {
	return &QuarantineManager{
		config:   cfg,
		records:  make(map[string][]QuarantineRecord),
		failures: make(map[string]int),
		now:      time.Now,
	}
}

// RecordFailure increments the failure count for a node.
// If failures reach the threshold, the node is automatically quarantined.
// Returns non-nil QuarantineRecord if quarantine was triggered.
func (qm *QuarantineManager) RecordFailure(nodeID string) *QuarantineRecord {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	qm.failures[nodeID]++
	if qm.failures[nodeID] >= qm.config.FailureThreshold {
		qm.failures[nodeID] = 0
		return qm.quarantineLocked(nodeID, QuarantineTaskFailures)
	}
	return nil
}

// RecordVerificationFailure immediately quarantines a node for verification failure.
func (qm *QuarantineManager) RecordVerificationFailure(nodeID string) *QuarantineRecord {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	return qm.quarantineLocked(nodeID, QuarantineVerificationFail)
}

// IsQuarantined checks if a node is currently quarantined.
func (qm *QuarantineManager) IsQuarantined(nodeID string) bool {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	now := qm.now()
	for _, r := range qm.records[nodeID] {
		if r.IsActive(now) {
			return true
		}
	}
	return false
}

// ActiveQuarantine returns the active quarantine record for a node, if any.
func (qm *QuarantineManager) ActiveQuarantine(nodeID string) *QuarantineRecord {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	now := qm.now()
	for _, r := range qm.records[nodeID] {
		if r.IsActive(now) {
			rec := r
			return &rec
		}
	}
	return nil
}

// Release manually releases a node from quarantine.
func (qm *QuarantineManager) Release(nodeID string) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	for i := range qm.records[nodeID] {
		qm.records[nodeID][i].Released = true
	}
	qm.failures[nodeID] = 0
}

// RecentQuarantineCount returns how many quarantines a node has had in the ban window.
func (qm *QuarantineManager) RecentQuarantineCount(nodeID string) int {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	return qm.recentCountLocked(nodeID)
}

// FailureCount returns the current consecutive failure count for a node.
func (qm *QuarantineManager) FailureCount(nodeID string) int {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	return qm.failures[nodeID]
}

func (qm *QuarantineManager) quarantineLocked(nodeID string, reason QuarantineReason) *QuarantineRecord {
	now := qm.now()

	// Determine duration based on reason and escalation
	var duration time.Duration
	switch reason {
	case QuarantineVerificationFail:
		duration = qm.config.VerificationDuration
	default:
		duration = qm.config.FailureDuration
	}

	// Escalation: if too many quarantines in window → ban
	recentCount := qm.recentCountLocked(nodeID)
	if recentCount+1 >= qm.config.BanThreshold {
		duration = qm.config.BanDuration
	}

	record := QuarantineRecord{
		NodeID:    nodeID,
		Reason:    reason,
		StartedAt: now,
		ExpiresAt: now.Add(duration),
	}

	qm.records[nodeID] = append(qm.records[nodeID], record)
	return &record
}

func (qm *QuarantineManager) recentCountLocked(nodeID string) int {
	now := qm.now()
	windowStart := now.AddDate(0, 0, -qm.config.BanWindowDays)
	count := 0
	for _, r := range qm.records[nodeID] {
		if r.StartedAt.After(windowStart) {
			count++
		}
	}
	return count
}

// ═══════════════════════════════════════════════════════════════════════════
// Version Rollback Manager
// ═══════════════════════════════════════════════════════════════════════════

// RollbackConfig configures automatic rollback behavior.
type RollbackConfig struct {
	HealthCheckInterval time.Duration // how often to check health (default 5s)
	CanaryDuration      time.Duration // how long canary runs before full rollout (default 10m)
	CrashThreshold      float64       // crash rate % to trigger rollback (default 5.0)
	RollbackTimeout     time.Duration // max time for rollback operation (default 5m)
}

// DefaultRollbackConfig returns production defaults.
func DefaultRollbackConfig() RollbackConfig {
	return RollbackConfig{
		HealthCheckInterval: 5 * time.Second,
		CanaryDuration:      10 * time.Minute,
		CrashThreshold:      5.0,
		RollbackTimeout:     5 * time.Minute,
	}
}

// DeploymentState tracks a rolling deployment with auto-rollback.
type DeploymentState struct {
	mu              sync.Mutex
	config          RollbackConfig
	currentVersion  string
	previousVersion string
	isCanary        bool
	crashCount      int
	totalChecks     int
	deployedAt      time.Time
	rolledBack      bool
	now             func() time.Time
}

// NewDeploymentState creates a deployment tracker.
func NewDeploymentState(cfg RollbackConfig, currentVersion, previousVersion string) *DeploymentState {
	return &DeploymentState{
		config:          cfg,
		currentVersion:  currentVersion,
		previousVersion: previousVersion,
		isCanary:        true,
		deployedAt:      time.Now(),
		now:             time.Now,
	}
}

// RecordHealthCheck records a health check result.
// Returns true if rollback should be triggered.
func (ds *DeploymentState) RecordHealthCheck(healthy bool) bool {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.totalChecks++
	if !healthy {
		ds.crashCount++
	}

	if ds.totalChecks == 0 {
		return false
	}

	crashRate := float64(ds.crashCount) / float64(ds.totalChecks) * 100.0
	return crashRate > ds.config.CrashThreshold
}

// ShouldPromoteCanary returns true if canary period has passed without issues.
func (ds *DeploymentState) ShouldPromoteCanary() bool {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if !ds.isCanary || ds.rolledBack {
		return false
	}

	elapsed := ds.now().Sub(ds.deployedAt)
	if elapsed < ds.config.CanaryDuration {
		return false
	}

	if ds.totalChecks == 0 {
		return false
	}

	crashRate := float64(ds.crashCount) / float64(ds.totalChecks) * 100.0
	return crashRate <= ds.config.CrashThreshold
}

// PromoteCanary marks the canary as promoted to full deployment.
func (ds *DeploymentState) PromoteCanary() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.isCanary = false
}

// MarkRolledBack records that a rollback occurred.
func (ds *DeploymentState) MarkRolledBack() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.rolledBack = true
	ds.isCanary = false
}

// Status returns the current deployment status.
type DeploymentStatus struct {
	CurrentVersion  string  `json:"current_version"`
	PreviousVersion string  `json:"previous_version"`
	IsCanary        bool    `json:"is_canary"`
	CrashRate       float64 `json:"crash_rate_pct"`
	TotalChecks     int     `json:"total_checks"`
	RolledBack      bool    `json:"rolled_back"`
}

// Status returns a snapshot of the deployment state.
func (ds *DeploymentState) Status() DeploymentStatus {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	crashRate := 0.0
	if ds.totalChecks > 0 {
		crashRate = float64(ds.crashCount) / float64(ds.totalChecks) * 100.0
	}
	return DeploymentStatus{
		CurrentVersion:  ds.currentVersion,
		PreviousVersion: ds.previousVersion,
		IsCanary:        ds.isCanary,
		CrashRate:       crashRate,
		TotalChecks:     ds.totalChecks,
		RolledBack:      ds.rolledBack,
	}
}
