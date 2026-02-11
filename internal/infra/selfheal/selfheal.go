// Package selfheal implements Phase 6 autonomous incident response.
//
// Traditional systems need human operators to diagnose and fix issues.
// The self-healing mesh automates the entire incident lifecycle:
//
//	DETECT → ISOLATE → REMEDIATE → VERIFY → REPORT
//
// Key concepts for beginners:
//
//   - Runbook: a predefined set of steps to fix a known problem. Instead of
//     a human reading a wiki page, the self-healer executes the runbook
//     automatically. Example: "if node has high failure rate → drain tasks
//     from node → quarantine → test with known-good task → un-quarantine."
//
//   - Incident Lifecycle: each detected problem becomes an "incident" with
//     a unique ID and state machine: Detected → Isolating → Remediating →
//     Verifying → Resolved (or Escalated if auto-fix failed).
//
//   - Task Migration: when a node is failing, we "drain" its queued tasks
//     and reschedule them on healthy nodes. This prevents task loss.
//
//   - MTTR (Mean Time To Recovery): how quickly the system recovers from
//     failures. Phase 6 target: < 5 minutes without human intervention.
//
//   - Escalation: if the runbook can't fix the problem after MaxAttempts,
//     the incident is escalated for human review. The system is honest
//     about its limits.
//
// Architecture ref: Phase 6 spec — "Autonomous Anomaly Response" deliverable.
// Gate check: < 5 min MTTR without human intervention.
package selfheal

import (
	"fmt"
	"sync"
	"time"
)

// ─── Configuration ──────────────────────────────────────────────────────────

// Config configures the autonomous self-healer.
type Config struct {
	// MaxRemediationAttempts is how many times we retry a runbook before
	// escalating the incident to humans.
	MaxRemediationAttempts int

	// IsolationTimeout is the max time a node can spend in isolation
	// before we either verify recovery or escalate.
	IsolationTimeout time.Duration

	// VerificationTimeout is the max time to wait for verification tasks
	// to confirm recovery before escalating.
	VerificationTimeout time.Duration

	// IncidentTTL is how long resolved/escalated incidents are retained.
	IncidentTTL time.Duration

	// MaxActiveIncidents caps concurrent incidents to prevent cascading.
	MaxActiveIncidents int

	// Now is an injectable clock for testing.
	Now func() time.Time
}

// DefaultConfig returns production defaults.
func DefaultConfig() Config {
	return Config{
		MaxRemediationAttempts: 3,
		IsolationTimeout:      2 * time.Minute,
		VerificationTimeout:   1 * time.Minute,
		IncidentTTL:           24 * time.Hour,
		MaxActiveIncidents:    100,
		Now:                   time.Now,
	}
}

// ─── Incident State Machine ────────────────────────────────────────────────

// IncidentState tracks the lifecycle of an incident.
type IncidentState int

const (
	StateDetected    IncidentState = iota // Problem just identified
	StateIsolating                        // Node being isolated/drained
	StateRemediating                      // Runbook executing fixes
	StateVerifying                        // Checking if fix worked
	StateResolved                         // Successfully recovered
	StateEscalated                        // Auto-fix failed, needs human
)

// String returns a human-readable state label.
func (s IncidentState) String() string {
	switch s {
	case StateDetected:
		return "DETECTED"
	case StateIsolating:
		return "ISOLATING"
	case StateRemediating:
		return "REMEDIATING"
	case StateVerifying:
		return "VERIFYING"
	case StateResolved:
		return "RESOLVED"
	case StateEscalated:
		return "ESCALATED"
	default:
		return "UNKNOWN"
	}
}

// IsTerminal returns true if the incident reached a final state.
func (s IncidentState) IsTerminal() bool {
	return s == StateResolved || s == StateEscalated
}

// ─── Failure Type ───────────────────────────────────────────────────────────

// FailureType classifies the type of detected problem.
type FailureType string

const (
	FailHighErrorRate   FailureType = "HIGH_ERROR_RATE"   // Sudden spike in task failures
	FailCPUOverload     FailureType = "CPU_OVERLOAD"      // Node CPU consistently >95%
	FailMemoryExhausted FailureType = "MEMORY_EXHAUSTED"  // Node memory >95%
	FailDiskFull        FailureType = "DISK_FULL"         // Node disk >95%
	FailNetworkPartial  FailureType = "NETWORK_PARTITION" // Node intermittently unreachable
	FailGPUError        FailureType = "GPU_ERROR"         // GPU not responding
	FailModelCorrupt    FailureType = "MODEL_CORRUPT"     // Model integrity check failed
	FailHeartbeatLost   FailureType = "HEARTBEAT_LOST"    // Node stopped sending heartbeats
)

// ─── Runbook ────────────────────────────────────────────────────────────────

// RunbookAction is a single step in a remediation runbook.
type RunbookAction struct {
	Name        string // human-readable step name
	Description string // what this step does
}

// Runbook is a sequence of remediation actions for a failure type.
type Runbook struct {
	FailureType FailureType
	Actions     []RunbookAction
	DrainFirst  bool // should we drain tasks before remediating?
}

// DefaultRunbooks returns the built-in runbook library.
// Each common failure type has a predefined remediation sequence.
func DefaultRunbooks() map[FailureType]Runbook {
	return map[FailureType]Runbook{
		FailHighErrorRate: {
			FailureType: FailHighErrorRate,
			DrainFirst:  true,
			Actions: []RunbookAction{
				{Name: "drain_tasks", Description: "Move all queued tasks to other nodes"},
				{Name: "quarantine_node", Description: "Place node in quarantine"},
				{Name: "run_test_task", Description: "Send a known-good test task to verify"},
			},
		},
		FailCPUOverload: {
			FailureType: FailCPUOverload,
			DrainFirst:  true,
			Actions: []RunbookAction{
				{Name: "reduce_concurrency", Description: "Lower max concurrent tasks on node"},
				{Name: "shed_low_priority", Description: "Cancel spot/batch tasks"},
				{Name: "wait_cooldown", Description: "Wait 60s for load to normalize"},
			},
		},
		FailMemoryExhausted: {
			FailureType: FailMemoryExhausted,
			DrainFirst:  true,
			Actions: []RunbookAction{
				{Name: "evict_models", Description: "Unload least-recently-used models"},
				{Name: "clear_caches", Description: "Flush inference caches"},
				{Name: "restart_engine", Description: "Restart inference engine process"},
			},
		},
		FailDiskFull: {
			FailureType: FailDiskFull,
			DrainFirst:  false,
			Actions: []RunbookAction{
				{Name: "prune_old_models", Description: "Remove oldest unused models"},
				{Name: "compact_database", Description: "Run SQLite VACUUM"},
				{Name: "purge_logs", Description: "Remove old log files"},
			},
		},
		FailNetworkPartial: {
			FailureType: FailNetworkPartial,
			DrainFirst:  true,
			Actions: []RunbookAction{
				{Name: "mark_suspect", Description: "Mark node as SUSPECT in gossip"},
				{Name: "reroute_traffic", Description: "Route tasks away from node"},
				{Name: "wait_reconnect", Description: "Wait for network recovery"},
			},
		},
		FailGPUError: {
			FailureType: FailGPUError,
			DrainFirst:  true,
			Actions: []RunbookAction{
				{Name: "reset_gpu_context", Description: "Reset GPU compute context"},
				{Name: "fallback_cpu", Description: "Switch to CPU-only inference"},
				{Name: "run_gpu_diag", Description: "Run GPU diagnostics"},
			},
		},
		FailModelCorrupt: {
			FailureType: FailModelCorrupt,
			DrainFirst:  false,
			Actions: []RunbookAction{
				{Name: "unload_model", Description: "Unload corrupted model from memory"},
				{Name: "delete_model", Description: "Remove corrupted model files"},
				{Name: "repull_model", Description: "Re-download model from registry"},
			},
		},
		FailHeartbeatLost: {
			FailureType: FailHeartbeatLost,
			DrainFirst:  true,
			Actions: []RunbookAction{
				{Name: "mark_dead", Description: "Mark node as DEAD in gossip"},
				{Name: "reassign_tasks", Description: "Re-queue tasks to healthy nodes"},
				{Name: "notify_cluster", Description: "Broadcast node death to cluster"},
			},
		},
	}
}

// ─── Incident ───────────────────────────────────────────────────────────────

// Incident represents a single detected problem and its resolution lifecycle.
type Incident struct {
	ID              string        // unique incident ID
	NodeID          string        // affected node
	FailureType     FailureType   // what went wrong
	State           IncidentState // current lifecycle state
	Attempts        int           // remediation attempts so far
	DrainedTasks    int           // how many tasks were migrated
	DetectedAt      time.Time     // when detected
	IsolatedAt      time.Time     // when isolated
	RemediatedAt    time.Time     // when remediation was attempted
	VerifiedAt      time.Time     // when verification completed
	ResolvedAt      time.Time     // when resolved or escalated
	CurrentAction   string        // which runbook step is executing
	ActionsComplete []string      // completed action names
	Error           string        // last error message (if escalated)
	MTTR            time.Duration // mean time to recovery (detection → resolution)
}

// ─── Self-Healing Mesh ──────────────────────────────────────────────────────

// Mesh is the Phase 6 autonomous self-healing system.
type Mesh struct {
	mu       sync.RWMutex
	cfg      Config
	runbooks map[FailureType]Runbook
	idSeq    int64 // monotonic incident ID sequence

	// Active and historical incidents.
	active   map[string]*Incident // incidentID → incident (non-terminal)
	resolved []*Incident          // resolved/escalated incidents (ring buffer)
	rIdx     int
	rCap     int
	rFull    bool

	// Per-node incident tracking (prevent duplicate incidents).
	nodeIncidents map[string]string // nodeID → active incident ID

	// MTTR tracking.
	totalMTTR    time.Duration
	resolvedCnt  int64
	escalatedCnt int64
}

// NewMesh creates a new autonomous self-healing mesh.
func NewMesh(cfg Config) *Mesh {
	if cfg.MaxRemediationAttempts <= 0 {
		cfg.MaxRemediationAttempts = 3
	}
	if cfg.IsolationTimeout <= 0 {
		cfg.IsolationTimeout = 2 * time.Minute
	}
	if cfg.VerificationTimeout <= 0 {
		cfg.VerificationTimeout = 1 * time.Minute
	}
	if cfg.IncidentTTL <= 0 {
		cfg.IncidentTTL = 24 * time.Hour
	}
	if cfg.MaxActiveIncidents <= 0 {
		cfg.MaxActiveIncidents = 100
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Mesh{
		cfg:           cfg,
		runbooks:      DefaultRunbooks(),
		active:        make(map[string]*Incident),
		resolved:      make([]*Incident, 10_000),
		rCap:          10_000,
		nodeIncidents: make(map[string]string),
	}
}

// ─── Core: Detect ───────────────────────────────────────────────────────────

// Detect creates a new incident for a detected failure on a node.
// If the node already has an active incident, returns it instead of creating a duplicate.
// Returns the incident and true if it's newly created.
func (m *Mesh) Detect(nodeID string, failureType FailureType) (*Incident, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for existing active incident on this node.
	if existingID, ok := m.nodeIncidents[nodeID]; ok {
		if inc, found := m.active[existingID]; found {
			return inc, false
		}
	}

	// Check active incident cap.
	if len(m.active) >= m.cfg.MaxActiveIncidents {
		return nil, false
	}

	now := m.cfg.Now()
	m.idSeq++
	id := fmt.Sprintf("INC-%06d", m.idSeq)

	inc := &Incident{
		ID:          id,
		NodeID:      nodeID,
		FailureType: failureType,
		State:       StateDetected,
		DetectedAt:  now,
	}

	m.active[id] = inc
	m.nodeIncidents[nodeID] = id
	return inc, true
}

// ─── Core: Isolate ──────────────────────────────────────────────────────────

// Isolate transitions an incident from Detected → Isolating, optionally
// recording how many tasks were drained (moved away from the failing node).
func (m *Mesh) Isolate(incidentID string, drainedTasks int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	inc, ok := m.active[incidentID]
	if !ok {
		return fmt.Errorf("incident %s not found", incidentID)
	}
	if inc.State != StateDetected {
		return fmt.Errorf("incident %s in state %s, expected DETECTED", incidentID, inc.State)
	}

	inc.State = StateIsolating
	inc.IsolatedAt = m.cfg.Now()
	inc.DrainedTasks = drainedTasks
	return nil
}

// ─── Core: Remediate ────────────────────────────────────────────────────────

// Remediate transitions an incident from Isolating → Remediating.
// It looks up the runbook for the failure type and begins executing it.
// Returns the runbook actions to execute. The caller should execute them
// and then call Verify().
func (m *Mesh) Remediate(incidentID string) ([]RunbookAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	inc, ok := m.active[incidentID]
	if !ok {
		return nil, fmt.Errorf("incident %s not found", incidentID)
	}
	if inc.State != StateIsolating {
		return nil, fmt.Errorf("incident %s in state %s, expected ISOLATING", incidentID, inc.State)
	}

	rb, exists := m.runbooks[inc.FailureType]
	if !exists {
		// No runbook — escalate immediately.
		inc.State = StateEscalated
		inc.Error = "no runbook for failure type: " + string(inc.FailureType)
		inc.ResolvedAt = m.cfg.Now()
		inc.MTTR = inc.ResolvedAt.Sub(inc.DetectedAt)
		m.finalizeLocked(inc)
		return nil, fmt.Errorf("no runbook for %s — escalated", inc.FailureType)
	}

	inc.State = StateRemediating
	inc.RemediatedAt = m.cfg.Now()
	inc.Attempts++

	return rb.Actions, nil
}

// RecordActionComplete records that a runbook action was completed.
func (m *Mesh) RecordActionComplete(incidentID, actionName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	inc, ok := m.active[incidentID]
	if !ok {
		return fmt.Errorf("incident %s not found", incidentID)
	}
	inc.ActionsComplete = append(inc.ActionsComplete, actionName)
	inc.CurrentAction = actionName
	return nil
}

// ─── Core: Verify ───────────────────────────────────────────────────────────

// Verify transitions from Remediating → Verifying, then checks if the
// problem is actually fixed. Pass `healthy=true` if verification succeeded.
func (m *Mesh) Verify(incidentID string, healthy bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	inc, ok := m.active[incidentID]
	if !ok {
		return fmt.Errorf("incident %s not found", incidentID)
	}
	if inc.State != StateRemediating {
		return fmt.Errorf("incident %s in state %s, expected REMEDIATING", incidentID, inc.State)
	}

	now := m.cfg.Now()
	inc.State = StateVerifying
	inc.VerifiedAt = now

	if healthy {
		// Fix worked — resolve!
		inc.State = StateResolved
		inc.ResolvedAt = now
		inc.MTTR = now.Sub(inc.DetectedAt)
		m.totalMTTR += inc.MTTR
		m.resolvedCnt++
		m.finalizeLocked(inc)
		return nil
	}

	// Fix didn't work — retry or escalate.
	if inc.Attempts >= m.cfg.MaxRemediationAttempts {
		inc.State = StateEscalated
		inc.ResolvedAt = now
		inc.Error = fmt.Sprintf("exhausted %d remediation attempts", inc.Attempts)
		inc.MTTR = now.Sub(inc.DetectedAt)
		m.escalatedCnt++
		m.finalizeLocked(inc)
		return nil
	}

	// Return to isolating for another attempt.
	inc.State = StateIsolating
	inc.IsolatedAt = now
	return nil
}

// finalizeLocked moves an incident from active to resolved history.
// Must be called with m.mu held.
func (m *Mesh) finalizeLocked(inc *Incident) {
	delete(m.active, inc.ID)
	delete(m.nodeIncidents, inc.NodeID)

	m.resolved[m.rIdx] = inc
	m.rIdx++
	if m.rIdx >= m.rCap {
		m.rIdx = 0
		m.rFull = true
	}
}

// ─── Escalate (manual) ─────────────────────────────────────────────────────

// Escalate manually escalates an active incident regardless of state.
func (m *Mesh) Escalate(incidentID, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	inc, ok := m.active[incidentID]
	if !ok {
		return fmt.Errorf("incident %s not found", incidentID)
	}

	now := m.cfg.Now()
	inc.State = StateEscalated
	inc.Error = reason
	inc.ResolvedAt = now
	inc.MTTR = now.Sub(inc.DetectedAt)
	m.escalatedCnt++
	m.finalizeLocked(inc)
	return nil
}

// ─── Runbook Management ────────────────────────────────────────────────────

// RegisterRunbook adds or replaces a runbook for a failure type.
func (m *Mesh) RegisterRunbook(rb Runbook) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runbooks[rb.FailureType] = rb
}

// Runbooks returns all registered runbooks.
func (m *Mesh) Runbooks() map[FailureType]Runbook {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[FailureType]Runbook, len(m.runbooks))
	for k, v := range m.runbooks {
		result[k] = v
	}
	return result
}

// ─── Incident Inspection ────────────────────────────────────────────────────

// ActiveIncidents returns all non-terminal incidents.
func (m *Mesh) ActiveIncidents() []*Incident {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Incident, 0, len(m.active))
	for _, inc := range m.active {
		result = append(result, inc)
	}
	return result
}

// ActiveIncidentCount returns the count of active incidents.
func (m *Mesh) ActiveIncidentCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.active)
}

// GetIncident returns an active incident by ID.
func (m *Mesh) GetIncident(id string) (*Incident, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inc, ok := m.active[id]
	return inc, ok
}

// NodeHasActiveIncident returns true if the given node has an active incident.
func (m *Mesh) NodeHasActiveIncident(nodeID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.nodeIncidents[nodeID]
	return ok
}

// ─── Statistics & Gate Check ────────────────────────────────────────────────

// MeshStats exposes self-healing performance metrics.
type MeshStats struct {
	ActiveIncidents    int           // current non-terminal incidents
	TotalResolved      int64         // total successfully resolved
	TotalEscalated     int64         // total escalated to humans
	AvgMTTR            time.Duration // average mean time to recovery
	ResolutionRate     float64       // resolved / (resolved + escalated) × 100
	RegisteredRunbooks int           // number of runbooks available
}

// Stats returns current self-healing statistics.
func (m *Mesh) Stats() MeshStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var avgMTTR time.Duration
	if m.resolvedCnt > 0 {
		avgMTTR = m.totalMTTR / time.Duration(m.resolvedCnt)
	}

	var resRate float64
	total := m.resolvedCnt + m.escalatedCnt
	if total > 0 {
		resRate = float64(m.resolvedCnt) / float64(total) * 100.0
	}

	return MeshStats{
		ActiveIncidents:    len(m.active),
		TotalResolved:      m.resolvedCnt,
		TotalEscalated:     m.escalatedCnt,
		AvgMTTR:            avgMTTR,
		ResolutionRate:     resRate,
		RegisteredRunbooks: len(m.runbooks),
	}
}

// GatePassed returns true if the average MTTR is below the target
// AND the autonomous resolution rate is at least the given percentage.
//
// Phase 6 gate check: "< 5 min MTTR without human intervention"
// and "95% autonomous anomaly resolution".
func (m *Mesh) GatePassed(maxMTTR time.Duration, minResolutionPct float64) bool {
	st := m.Stats()
	if st.TotalResolved == 0 {
		return false
	}
	return st.AvgMTTR <= maxMTTR && st.ResolutionRate >= minResolutionPct
}

// ─── Recent History ─────────────────────────────────────────────────────────

// ResolvedIncidents returns the most recent N resolved/escalated incidents.
func (m *Mesh) ResolvedIncidents(limit int) []*Incident {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var count int
	if m.rFull {
		count = m.rCap
	} else {
		count = m.rIdx
	}
	if limit > count {
		limit = count
	}
	if limit <= 0 {
		return nil
	}

	result := make([]*Incident, limit)
	idx := m.rIdx
	for i := 0; i < limit; i++ {
		idx--
		if idx < 0 {
			idx = m.rCap - 1
		}
		result[i] = m.resolved[idx]
	}
	return result
}

// Reset clears all incidents and statistics.
func (m *Mesh) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.idSeq = 0
	m.active = make(map[string]*Incident)
	m.resolved = make([]*Incident, m.rCap)
	m.rIdx = 0
	m.rFull = false
	m.nodeIncidents = make(map[string]string)
	m.totalMTTR = 0
	m.resolvedCnt = 0
	m.escalatedCnt = 0
}
