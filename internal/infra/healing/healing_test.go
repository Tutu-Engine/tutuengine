package healing

import (
	"testing"
	"time"
)

// ═══════════════════════════════════════════════════════════════════════════
// Healing Tests — Phase 3
// ═══════════════════════════════════════════════════════════════════════════

// ─── Helpers ────────────────────────────────────────────────────────────────

func newTestCB(t *testing.T) *CircuitBreaker {
	t.Helper()
	return NewCircuitBreaker("test-cb", DefaultCircuitBreakerConfig())
}

func newTestCBWithClock(t *testing.T, now func() time.Time) *CircuitBreaker {
	t.Helper()
	cb := NewCircuitBreaker("test-cb", CircuitBreakerConfig{
		FailureThreshold: 3,
		ResetTimeout:     1 * time.Second,
		HalfOpenMax:      2,
	})
	cb.now = now
	return cb
}

// ─── CBState.String ─────────────────────────────────────────────────────────

func TestCBState_String(t *testing.T) {
	tests := []struct {
		state CBState
		want  string
	}{
		{CBClosed, "CLOSED"},
		{CBOpen, "OPEN"},
		{CBHalfOpen, "HALF_OPEN"},
		{CBState(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("CBState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// ─── Circuit Breaker State Transitions ──────────────────────────────────────

func TestCircuitBreaker_StartsInClosed(t *testing.T) {
	cb := newTestCB(t)
	if cb.State() != CBClosed {
		t.Errorf("initial state = %s, want CLOSED", cb.State())
	}
}

func TestCircuitBreaker_Closed_AllowsRequests(t *testing.T) {
	cb := newTestCB(t)
	if err := cb.Allow(); err != nil {
		t.Errorf("Allow() in CLOSED state should succeed, got %v", err)
	}
}

func TestCircuitBreaker_TripsToOpen(t *testing.T) {
	clock := time.Now()
	cb := newTestCBWithClock(t, func() time.Time { return clock })

	// 3 failures should trip the breaker (threshold=3)
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	if cb.State() != CBOpen {
		t.Errorf("state after %d failures = %s, want OPEN", 3, cb.State())
	}
}

func TestCircuitBreaker_Open_BlocksRequests(t *testing.T) {
	clock := time.Now()
	cb := newTestCBWithClock(t, func() time.Time { return clock })

	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	err := cb.Allow()
	if err == nil {
		t.Error("Allow() in OPEN state should return error")
	}
}

func TestCircuitBreaker_Open_TransitionsToHalfOpen(t *testing.T) {
	clock := time.Now()
	cb := newTestCBWithClock(t, func() time.Time { return clock })

	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	// Advance past reset timeout
	clock = clock.Add(2 * time.Second)
	cb.now = func() time.Time { return clock }

	if cb.State() != CBHalfOpen {
		t.Errorf("state after timeout = %s, want HALF_OPEN", cb.State())
	}
}

func TestCircuitBreaker_HalfOpen_AllowsProbes(t *testing.T) {
	clock := time.Now()
	cb := newTestCBWithClock(t, func() time.Time { return clock })

	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	clock = clock.Add(2 * time.Second)
	cb.now = func() time.Time { return clock }

	// Should allow in HALF_OPEN
	if err := cb.Allow(); err != nil {
		t.Errorf("Allow() in HALF_OPEN should succeed, got %v", err)
	}
}

func TestCircuitBreaker_HalfOpen_SuccessCloses(t *testing.T) {
	clock := time.Now()
	cb := newTestCBWithClock(t, func() time.Time { return clock })

	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	clock = clock.Add(2 * time.Second)
	cb.now = func() time.Time { return clock }

	cb.Allow() // transition to HALF_OPEN

	// 2 successes should close (HalfOpenMax=2)
	cb.RecordSuccess()
	cb.RecordSuccess()

	if cb.State() != CBClosed {
		t.Errorf("state after %d successes in HALF_OPEN = %s, want CLOSED", 2, cb.State())
	}
}

func TestCircuitBreaker_HalfOpen_FailureReopens(t *testing.T) {
	clock := time.Now()
	cb := newTestCBWithClock(t, func() time.Time { return clock })

	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	clock = clock.Add(2 * time.Second)
	cb.now = func() time.Time { return clock }

	cb.Allow() // transition to HALF_OPEN
	cb.RecordFailure()

	if cb.State() != CBOpen {
		t.Errorf("state after failure in HALF_OPEN = %s, want OPEN", cb.State())
	}
}

func TestCircuitBreaker_Closed_SuccessDecaysFailures(t *testing.T) {
	cb := newTestCB(t)
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess() // should decay 1 failure
	snap := cb.Snapshot()
	if snap.Failures != 1 {
		t.Errorf("Failures after 2 failures + 1 success = %d, want 1", snap.Failures)
	}
}

// ─── Snapshot ───────────────────────────────────────────────────────────────

func TestCircuitBreaker_Snapshot(t *testing.T) {
	cb := newTestCB(t)
	snap := cb.Snapshot()
	if snap.Name != "test-cb" {
		t.Errorf("Name = %q, want %q", snap.Name, "test-cb")
	}
	if snap.State != CBClosed {
		t.Errorf("State = %s, want CLOSED", snap.State)
	}
	if snap.TotalTrips != 0 {
		t.Errorf("TotalTrips = %d, want 0", snap.TotalTrips)
	}
}

func TestCircuitBreaker_Snapshot_CountsTrips(t *testing.T) {
	clock := time.Now()
	cb := newTestCBWithClock(t, func() time.Time { return clock })

	// Trip once
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	snap := cb.Snapshot()
	if snap.TotalTrips != 1 {
		t.Errorf("TotalTrips = %d, want 1", snap.TotalTrips)
	}
}

// ─── Reset ──────────────────────────────────────────────────────────────────

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := newTestCB(t)
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}
	cb.Reset()
	if cb.State() != CBClosed {
		t.Errorf("State after Reset() = %s, want CLOSED", cb.State())
	}
	if err := cb.Allow(); err != nil {
		t.Errorf("Allow() after Reset() = %v, want nil", err)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Quarantine Manager Tests
// ═══════════════════════════════════════════════════════════════════════════

func newTestQM(t *testing.T, now func() time.Time) *QuarantineManager {
	t.Helper()
	qm := NewQuarantineManager(QuarantineConfig{
		FailureDuration:      1 * time.Hour,
		VerificationDuration: 24 * time.Hour,
		BanDuration:          30 * 24 * time.Hour,
		BanWindowDays:        7,
		BanThreshold:         3,
		FailureThreshold:     3,
	})
	qm.now = now
	return qm
}

func TestQuarantine_NotQuarantinedByDefault(t *testing.T) {
	clock := time.Now()
	qm := newTestQM(t, func() time.Time { return clock })
	if qm.IsQuarantined("node-1") {
		t.Error("node should not be quarantined by default")
	}
}

func TestQuarantine_FailureThresholdTriggers(t *testing.T) {
	clock := time.Now()
	qm := newTestQM(t, func() time.Time { return clock })

	// 2 failures: not yet quarantined
	qm.RecordFailure("node-1")
	qm.RecordFailure("node-1")
	if qm.IsQuarantined("node-1") {
		t.Error("2 failures should not trigger quarantine (threshold=3)")
	}

	// 3rd failure: quarantined
	rec := qm.RecordFailure("node-1")
	if rec == nil {
		t.Fatal("3rd failure should return quarantine record")
	}
	if !qm.IsQuarantined("node-1") {
		t.Error("node should be quarantined after 3 failures")
	}
	if rec.Reason != QuarantineTaskFailures {
		t.Errorf("Reason = %q, want %q", rec.Reason, QuarantineTaskFailures)
	}
}

func TestQuarantine_FailureCountReset(t *testing.T) {
	clock := time.Now()
	qm := newTestQM(t, func() time.Time { return clock })
	qm.RecordFailure("node-1")
	qm.RecordFailure("node-1")
	qm.RecordFailure("node-1") // triggers quarantine, resets counter

	if qm.FailureCount("node-1") != 0 {
		t.Errorf("FailureCount after quarantine trigger = %d, want 0", qm.FailureCount("node-1"))
	}
}

func TestQuarantine_VerificationFailure_Immediate(t *testing.T) {
	clock := time.Now()
	qm := newTestQM(t, func() time.Time { return clock })
	rec := qm.RecordVerificationFailure("node-1")
	if rec == nil {
		t.Fatal("RecordVerificationFailure should return record")
	}
	if rec.Reason != QuarantineVerificationFail {
		t.Errorf("Reason = %q, want %q", rec.Reason, QuarantineVerificationFail)
	}
	if !qm.IsQuarantined("node-1") {
		t.Error("node should be quarantined after verification failure")
	}
	// Verification failures have 24h duration
	expectedExpiry := clock.Add(24 * time.Hour)
	if !rec.ExpiresAt.Equal(expectedExpiry) {
		t.Errorf("ExpiresAt = %v, want %v", rec.ExpiresAt, expectedExpiry)
	}
}

func TestQuarantine_Expires(t *testing.T) {
	clock := time.Now()
	qm := newTestQM(t, func() time.Time { return clock })
	qm.RecordFailure("node-1")
	qm.RecordFailure("node-1")
	qm.RecordFailure("node-1")

	// Advance past quarantine duration
	clock = clock.Add(2 * time.Hour) // > 1h failure duration
	qm.now = func() time.Time { return clock }

	if qm.IsQuarantined("node-1") {
		t.Error("quarantine should have expired after 2 hours")
	}
}

func TestQuarantine_Release(t *testing.T) {
	clock := time.Now()
	qm := newTestQM(t, func() time.Time { return clock })
	qm.RecordFailure("node-1")
	qm.RecordFailure("node-1")
	qm.RecordFailure("node-1")

	qm.Release("node-1")
	if qm.IsQuarantined("node-1") {
		t.Error("node should not be quarantined after Release()")
	}
}

func TestQuarantine_BanEscalation(t *testing.T) {
	clock := time.Now()
	qm := newTestQM(t, func() time.Time { return clock })

	// Create 3 quarantines in the 7-day window → ban
	for i := 0; i < 3; i++ {
		qm.RecordFailure("node-1")
		qm.RecordFailure("node-1")
		qm.RecordFailure("node-1") // each triggers quarantine
		qm.Release("node-1")       // release so we can trigger again
	}

	// The 3rd quarantine should have been escalated to a ban (30 days)
	count := qm.RecentQuarantineCount("node-1")
	if count < 3 {
		t.Errorf("RecentQuarantineCount = %d, want >= 3", count)
	}
}

func TestQuarantine_ActiveQuarantine(t *testing.T) {
	clock := time.Now()
	qm := newTestQM(t, func() time.Time { return clock })
	qm.RecordFailure("node-1")
	qm.RecordFailure("node-1")
	qm.RecordFailure("node-1")

	active := qm.ActiveQuarantine("node-1")
	if active == nil {
		t.Fatal("ActiveQuarantine should return a record")
	}
	if active.NodeID != "node-1" {
		t.Errorf("NodeID = %q, want %q", active.NodeID, "node-1")
	}
}

func TestQuarantine_ActiveQuarantine_None(t *testing.T) {
	clock := time.Now()
	qm := newTestQM(t, func() time.Time { return clock })
	if qm.ActiveQuarantine("nonexistent") != nil {
		t.Error("ActiveQuarantine should be nil for unknown node")
	}
}

func TestQuarantineRecord_IsActive(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name   string
		rec    QuarantineRecord
		active bool
	}{
		{"active", QuarantineRecord{ExpiresAt: now.Add(1 * time.Hour)}, true},
		{"expired", QuarantineRecord{ExpiresAt: now.Add(-1 * time.Hour)}, false},
		{"released", QuarantineRecord{ExpiresAt: now.Add(1 * time.Hour), Released: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rec.IsActive(now); got != tt.active {
				t.Errorf("IsActive() = %v, want %v", got, tt.active)
			}
		})
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Deployment State / Rollback Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestDeploymentState_HealthyCanary(t *testing.T) {
	ds := NewDeploymentState(DefaultRollbackConfig(), "v2.0", "v1.0")
	ds.now = func() time.Time { return time.Now().Add(11 * time.Minute) } // past canary duration (10m)

	// All healthy checks
	for i := 0; i < 100; i++ {
		shouldRollback := ds.RecordHealthCheck(true)
		if shouldRollback {
			t.Fatalf("should not rollback on healthy checks, check #%d", i)
		}
	}

	if !ds.ShouldPromoteCanary() {
		t.Error("ShouldPromoteCanary() = false, want true after canary passes")
	}

	ds.PromoteCanary()
	status := ds.Status()
	if status.IsCanary {
		t.Error("IsCanary should be false after promotion")
	}
}

func TestDeploymentState_RollbackOnHighCrashRate(t *testing.T) {
	ds := NewDeploymentState(RollbackConfig{
		CanaryDuration: 10 * time.Minute,
		CrashThreshold: 5.0,
	}, "v2.0", "v1.0")

	// 6% crash rate → should trigger rollback
	for i := 0; i < 94; i++ {
		ds.RecordHealthCheck(true)
	}
	for i := 0; i < 6; i++ {
		shouldRollback := ds.RecordHealthCheck(false)
		if i == 5 && !shouldRollback {
			t.Error("should trigger rollback at 6% crash rate (threshold=5%)")
		}
	}
}

func TestDeploymentState_NoPromote_HighCrashRate(t *testing.T) {
	ds := NewDeploymentState(RollbackConfig{
		CanaryDuration: 10 * time.Minute,
		CrashThreshold: 5.0,
	}, "v2.0", "v1.0")
	ds.now = func() time.Time { return time.Now().Add(11 * time.Minute) }

	for i := 0; i < 90; i++ {
		ds.RecordHealthCheck(true)
	}
	for i := 0; i < 10; i++ {
		ds.RecordHealthCheck(false)
	}

	if ds.ShouldPromoteCanary() {
		t.Error("should NOT promote canary with 10% crash rate")
	}
}

func TestDeploymentState_NoPromote_TooEarly(t *testing.T) {
	ds := NewDeploymentState(DefaultRollbackConfig(), "v2.0", "v1.0")
	// Don't advance time → still in canary period
	ds.RecordHealthCheck(true)
	if ds.ShouldPromoteCanary() {
		t.Error("should NOT promote before canary duration elapses")
	}
}

func TestDeploymentState_MarkRolledBack(t *testing.T) {
	ds := NewDeploymentState(DefaultRollbackConfig(), "v2.0", "v1.0")
	ds.MarkRolledBack()
	status := ds.Status()
	if !status.RolledBack {
		t.Error("RolledBack should be true")
	}
	if status.IsCanary {
		t.Error("IsCanary should be false after rollback")
	}
}

func TestDeploymentState_NoPromote_AfterRollback(t *testing.T) {
	ds := NewDeploymentState(DefaultRollbackConfig(), "v2.0", "v1.0")
	ds.now = func() time.Time { return time.Now().Add(11 * time.Minute) }
	ds.RecordHealthCheck(true)
	ds.MarkRolledBack()
	if ds.ShouldPromoteCanary() {
		t.Error("should NOT promote after rollback")
	}
}

func TestDeploymentState_Status(t *testing.T) {
	ds := NewDeploymentState(DefaultRollbackConfig(), "v2.0", "v1.0")
	status := ds.Status()
	if status.CurrentVersion != "v2.0" {
		t.Errorf("CurrentVersion = %q, want %q", status.CurrentVersion, "v2.0")
	}
	if status.PreviousVersion != "v1.0" {
		t.Errorf("PreviousVersion = %q, want %q", status.PreviousVersion, "v1.0")
	}
	if !status.IsCanary {
		t.Error("should start as canary")
	}
}

func TestDeploymentState_NoPromote_NoChecks(t *testing.T) {
	ds := NewDeploymentState(DefaultRollbackConfig(), "v2.0", "v1.0")
	ds.now = func() time.Time { return time.Now().Add(11 * time.Minute) }
	// No health checks recorded
	if ds.ShouldPromoteCanary() {
		t.Error("should NOT promote with zero health checks")
	}
}
