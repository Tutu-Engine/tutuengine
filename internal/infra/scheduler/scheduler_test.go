package scheduler

import (
	"testing"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ═══════════════════════════════════════════════════════════════════════════
// Scheduler Tests — Phase 3
// ═══════════════════════════════════════════════════════════════════════════

func newTestScheduler(t *testing.T) *Scheduler {
	t.Helper()
	return NewScheduler(DefaultConfig())
}

func newSmallScheduler(t *testing.T) *Scheduler {
	t.Helper()
	return NewScheduler(Config{
		MaxQueueDepth:      20,
		BackPressureSoft:   5,
		BackPressureMedium: 10,
		BackPressureHard:   15,
		StarvationInterval: 100 * time.Millisecond,
		PreemptionEnabled:  true,
	})
}

func taskAt(priority int, taskType domain.TaskType) domain.Task {
	return domain.Task{
		ID:       "task-" + PriorityLabel(priority),
		Type:     taskType,
		Status:   domain.TaskQueued,
		Priority: priority,
	}
}

// ─── Priority Labels ────────────────────────────────────────────────────────

func TestPriorityLabel(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{P0Realtime, "REALTIME"},
		{P1High, "HIGH"},
		{P2Normal, "NORMAL"},
		{P3Low, "LOW"},
		{P4Spot, "SPOT"},
		{99, "UNKNOWN"},
	}
	for _, tt := range tests {
		if got := PriorityLabel(tt.in); got != tt.want {
			t.Errorf("PriorityLabel(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ─── Enqueue / Dequeue ──────────────────────────────────────────────────────

func TestScheduler_Enqueue_Dequeue(t *testing.T) {
	s := newTestScheduler(t)
	task := taskAt(P2Normal, domain.TaskInference)
	if err := s.Enqueue(task, domain.TaskRouting{}); err != nil {
		t.Fatalf("Enqueue() error: %v", err)
	}
	if s.QueueDepth() != 1 {
		t.Errorf("QueueDepth() = %d, want 1", s.QueueDepth())
	}

	got := s.Dequeue()
	if got == nil {
		t.Fatal("Dequeue() returned nil")
	}
	if got.Task.ID != task.ID {
		t.Errorf("Task.ID = %q, want %q", got.Task.ID, task.ID)
	}
	if s.QueueDepth() != 0 {
		t.Errorf("after dequeue, QueueDepth() = %d, want 0", s.QueueDepth())
	}
}

func TestScheduler_Dequeue_Empty(t *testing.T) {
	s := newTestScheduler(t)
	if got := s.Dequeue(); got != nil {
		t.Errorf("Dequeue() on empty = %v, want nil", got)
	}
}

func TestScheduler_PriorityOrdering(t *testing.T) {
	s := newTestScheduler(t)
	// Enqueue in reverse order
	for _, p := range []int{P4Spot, P3Low, P2Normal, P1High, P0Realtime} {
		task := domain.Task{ID: PriorityLabel(p), Priority: p, Status: domain.TaskQueued, Type: domain.TaskInference}
		if err := s.Enqueue(task, domain.TaskRouting{}); err != nil {
			t.Fatalf("Enqueue(P%d) error: %v", p, err)
		}
	}

	// Should dequeue highest priority first
	expected := []string{"REALTIME", "HIGH", "NORMAL", "LOW", "SPOT"}
	for _, want := range expected {
		got := s.Dequeue()
		if got == nil {
			t.Fatalf("Dequeue() returned nil, want %q", want)
		}
		if got.Task.ID != want {
			t.Errorf("Dequeue() = %q, want %q", got.Task.ID, want)
		}
	}
}

// ─── Back-Pressure ──────────────────────────────────────────────────────────

func TestScheduler_BackPressure_Soft(t *testing.T) {
	s := newSmallScheduler(t) // soft=5
	// Fill up to soft threshold
	for i := 0; i < 5; i++ {
		task := domain.Task{ID: "fill", Priority: P2Normal, Status: domain.TaskQueued, Type: domain.TaskInference}
		if err := s.Enqueue(task, domain.TaskRouting{}); err != nil {
			t.Fatalf("Enqueue fill #%d error: %v", i, err)
		}
	}

	if s.BackPressureLevel() != BPSoft {
		t.Fatalf("BackPressureLevel = %v, want BPSoft", s.BackPressureLevel())
	}

	// P4 Spot should be rejected
	spotTask := domain.Task{ID: "spot", Priority: P4Spot, Status: domain.TaskQueued, Type: domain.TaskInference}
	err := s.Enqueue(spotTask, domain.TaskRouting{})
	if err != domain.ErrBackPressureSoft {
		t.Errorf("Enqueue(P4) error = %v, want ErrBackPressureSoft", err)
	}

	// P2 Normal should still be accepted
	normalTask := domain.Task{ID: "ok", Priority: P2Normal, Status: domain.TaskQueued, Type: domain.TaskInference}
	if err := s.Enqueue(normalTask, domain.TaskRouting{}); err != nil {
		t.Errorf("Enqueue(P2) should succeed at BPSoft, got %v", err)
	}
}

func TestScheduler_BackPressure_Medium(t *testing.T) {
	s := newSmallScheduler(t) // medium=10
	for i := 0; i < 10; i++ {
		task := domain.Task{ID: "fill", Priority: P0Realtime, Status: domain.TaskQueued, Type: domain.TaskInference}
		if err := s.Enqueue(task, domain.TaskRouting{}); err != nil {
			t.Fatalf("Enqueue fill #%d error: %v", i, err)
		}
	}

	if s.BackPressureLevel() != BPMedium {
		t.Fatalf("BackPressureLevel = %v, want BPMedium", s.BackPressureLevel())
	}

	// Only P0 realtime should be accepted
	highTask := domain.Task{ID: "high", Priority: P1High, Status: domain.TaskQueued, Type: domain.TaskInference}
	err := s.Enqueue(highTask, domain.TaskRouting{})
	if err != domain.ErrBackPressureMedium {
		t.Errorf("Enqueue(P1) at BPMedium = %v, want ErrBackPressureMedium", err)
	}

	rtTask := domain.Task{ID: "rt", Priority: P0Realtime, Status: domain.TaskQueued, Type: domain.TaskInference}
	if err := s.Enqueue(rtTask, domain.TaskRouting{}); err != nil {
		t.Errorf("Enqueue(P0) should succeed at BPMedium, got %v", err)
	}
}

func TestScheduler_BackPressure_Hard(t *testing.T) {
	s := newSmallScheduler(t) // hard=15
	for i := 0; i < 15; i++ {
		task := domain.Task{ID: "fill", Priority: P0Realtime, Status: domain.TaskQueued, Type: domain.TaskInference}
		if err := s.Enqueue(task, domain.TaskRouting{}); err != nil {
			t.Fatalf("Enqueue fill #%d error: %v", i, err)
		}
	}

	if s.BackPressureLevel() != BPHard {
		t.Fatalf("BackPressureLevel = %v, want BPHard", s.BackPressureLevel())
	}

	// Even P0 should be rejected
	rtTask := domain.Task{ID: "rt", Priority: P0Realtime, Status: domain.TaskQueued, Type: domain.TaskInference}
	err := s.Enqueue(rtTask, domain.TaskRouting{})
	if err != domain.ErrBackPressureHard {
		t.Errorf("Enqueue(P0) at BPHard = %v, want ErrBackPressureHard", err)
	}
}

// ─── BackPressureLevel String ───────────────────────────────────────────────

func TestBackPressureLevel_String(t *testing.T) {
	tests := []struct {
		bp   BackPressureLevel
		want string
	}{
		{BPNone, "NONE"},
		{BPSoft, "SOFT"},
		{BPMedium, "MEDIUM"},
		{BPHard, "HARD"},
		{BackPressureLevel(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		if got := tt.bp.String(); got != tt.want {
			t.Errorf("BackPressureLevel(%d).String() = %q, want %q", tt.bp, got, tt.want)
		}
	}
}

// ─── Work Stealing ──────────────────────────────────────────────────────────

func TestScheduler_WorkStealing(t *testing.T) {
	s := newTestScheduler(t)
	// Enqueue 10 spot tasks
	for i := 0; i < 10; i++ {
		task := domain.Task{ID: "spot", Priority: P4Spot, Status: domain.TaskQueued, Type: domain.TaskInference}
		if err := s.Enqueue(task, domain.TaskRouting{}); err != nil {
			t.Fatal(err)
		}
	}

	stolen := s.StealableTasks(3)
	if len(stolen) != 3 {
		t.Fatalf("StealableTasks(3) = %d, want 3", len(stolen))
	}
	if s.QueueDepth() != 7 {
		t.Errorf("after stealing 3, QueueDepth() = %d, want 7", s.QueueDepth())
	}
}

func TestScheduler_WorkStealing_StealsLowestPriorityFirst(t *testing.T) {
	s := newTestScheduler(t)
	// Add 2 normal + 3 spot
	for i := 0; i < 2; i++ {
		task := domain.Task{ID: "normal", Priority: P2Normal, Status: domain.TaskQueued, Type: domain.TaskInference}
		s.Enqueue(task, domain.TaskRouting{})
	}
	for i := 0; i < 3; i++ {
		task := domain.Task{ID: "spot", Priority: P4Spot, Status: domain.TaskQueued, Type: domain.TaskInference}
		s.Enqueue(task, domain.TaskRouting{})
	}

	stolen := s.StealableTasks(4)
	if len(stolen) != 4 {
		t.Fatalf("StealableTasks(4) = %d, want 4", len(stolen))
	}
	// First 3 should be spot (P4 stolen first), 4th should be normal
	spotCount := 0
	for _, st := range stolen {
		if st.Task.Priority == P4Spot {
			spotCount++
		}
	}
	if spotCount != 3 {
		t.Errorf("stole %d spot tasks, want 3 (lowest priority stolen first)", spotCount)
	}
}

func TestScheduler_WorkStealing_AutoHalf(t *testing.T) {
	s := newTestScheduler(t)
	for i := 0; i < 10; i++ {
		task := domain.Task{ID: "t", Priority: P3Low, Status: domain.TaskQueued, Type: domain.TaskInference}
		s.Enqueue(task, domain.TaskRouting{})
	}
	// maxCount=0 → steal half
	stolen := s.StealableTasks(0)
	if len(stolen) != 5 {
		t.Errorf("StealableTasks(0) = %d, want 5 (half of 10)", len(stolen))
	}
}

func TestScheduler_ImportStolenTasks(t *testing.T) {
	s := newTestScheduler(t)
	tasks := []QueuedTask{
		{Task: domain.Task{ID: "a", Priority: P2Normal, Status: domain.TaskQueued}, QueuedAt: time.Now()},
		{Task: domain.Task{ID: "b", Priority: P4Spot, Status: domain.TaskQueued}, QueuedAt: time.Now()},
	}
	s.ImportStolenTasks(tasks)
	if s.QueueDepth() != 2 {
		t.Errorf("after import, QueueDepth() = %d, want 2", s.QueueDepth())
	}
}

// ─── Preemption ─────────────────────────────────────────────────────────────

func TestScheduler_Preempt_RealtimePreemptsSpot(t *testing.T) {
	s := newTestScheduler(t)
	rt := domain.Task{ID: "rt", Priority: P0Realtime, Status: domain.TaskQueued, Type: domain.TaskInference}
	running := []domain.Task{
		{ID: "spot1", Priority: P4Spot, Status: domain.TaskExecuting, Type: domain.TaskInference},
		{ID: "normal1", Priority: P2Normal, Status: domain.TaskExecuting, Type: domain.TaskInference},
	}
	victim := s.Preempt(rt, running)
	if victim == nil {
		t.Fatal("Preempt() returned nil, want spot task")
	}
	if victim.ID != "spot1" {
		t.Errorf("Preempt() = %q, want %q", victim.ID, "spot1")
	}
}

func TestScheduler_Preempt_OnlyRealtimeCanPreempt(t *testing.T) {
	s := newTestScheduler(t)
	high := domain.Task{ID: "high", Priority: P1High, Type: domain.TaskInference}
	running := []domain.Task{
		{ID: "spot1", Priority: P4Spot, Status: domain.TaskExecuting, Type: domain.TaskInference},
	}
	if victim := s.Preempt(high, running); victim != nil {
		t.Errorf("P1 should not preempt, but got %q", victim.ID)
	}
}

func TestScheduler_Preempt_Disabled(t *testing.T) {
	s := NewScheduler(Config{PreemptionEnabled: false})
	rt := domain.Task{ID: "rt", Priority: P0Realtime, Type: domain.TaskInference}
	running := []domain.Task{
		{ID: "spot1", Priority: P4Spot, Status: domain.TaskExecuting, Type: domain.TaskInference},
	}
	if victim := s.Preempt(rt, running); victim != nil {
		t.Error("Preempt() should be nil when disabled")
	}
}

func TestScheduler_Preempt_NoSpotRunning(t *testing.T) {
	s := newTestScheduler(t)
	rt := domain.Task{ID: "rt", Priority: P0Realtime, Type: domain.TaskInference}
	running := []domain.Task{
		{ID: "norm", Priority: P2Normal, Status: domain.TaskExecuting, Type: domain.TaskInference},
	}
	if victim := s.Preempt(rt, running); victim != nil {
		t.Error("Preempt() should be nil when no spot tasks running")
	}
}

// ─── Starvation Prevention ──────────────────────────────────────────────────

func TestQueuedTask_EffectivePriority(t *testing.T) {
	qt := QueuedTask{
		Task:     domain.Task{Priority: P4Spot},
		QueuedAt: time.Now().Add(-130 * time.Millisecond), // 1.3× interval
	}
	eff := qt.EffectivePriority(100 * time.Millisecond)
	if eff != P3Low {
		t.Errorf("EffectivePriority = %d, want %d (boosted by 1)", eff, P3Low)
	}
}

func TestQueuedTask_EffectivePriority_FloorAtZero(t *testing.T) {
	qt := QueuedTask{
		Task:     domain.Task{Priority: P0Realtime},
		QueuedAt: time.Now().Add(-1 * time.Hour),
	}
	eff := qt.EffectivePriority(100 * time.Millisecond)
	if eff != 0 {
		t.Errorf("EffectivePriority = %d, want 0 (floor)", eff)
	}
}

// ─── Node Scoring ───────────────────────────────────────────────────────────

func TestScoreNode_DisqualifiesNoGPU_ForFineTune(t *testing.T) {
	node := NodeCandidate{
		NodeID:       "n1",
		Region:       domain.RegionUSEast,
		GPUAvailable: false,
		Reputation:   0.9,
	}
	task := domain.Task{Type: domain.TaskFineTune}
	score := ScoreNode(node, task, domain.RegionUSEast)
	if score != 0 {
		t.Errorf("ScoreNode(no GPU for fine-tune) = %f, want 0", score)
	}
}

func TestScoreNode_HigherForSameRegion(t *testing.T) {
	base := NodeCandidate{
		NodeID:       "n1",
		Reputation:   0.8,
		CurrentLoad:  0.3,
		LatencyMs:    10,
		GPUAvailable: true,
		VRAMGB:       16,
		CreditRate:   5,
	}
	task := domain.Task{Type: domain.TaskInference}

	// Same region
	local := base
	local.Region = domain.RegionUSEast
	localScore := ScoreNode(local, task, domain.RegionUSEast)

	// Different region
	remote := base
	remote.Region = domain.RegionAPSouth
	remoteScore := ScoreNode(remote, task, domain.RegionUSEast)

	if localScore <= remoteScore {
		t.Errorf("same-region (%f) should score higher than cross-region (%f)", localScore, remoteScore)
	}
}

func TestScoreNode_CacheBonus(t *testing.T) {
	base := NodeCandidate{
		NodeID:       "n1",
		Region:       domain.RegionUSEast,
		Reputation:   0.8,
		CurrentLoad:  0.3,
		GPUAvailable: true,
	}
	task := domain.Task{Type: domain.TaskInference}

	cold := base
	cold.HasModelHot = false
	coldScore := ScoreNode(cold, task, domain.RegionUSEast)

	hot := base
	hot.HasModelHot = true
	hotScore := ScoreNode(hot, task, domain.RegionUSEast)

	if hotScore <= coldScore {
		t.Errorf("cache-hot (%f) should score higher than cache-cold (%f)", hotScore, coldScore)
	}
}

func TestRankNodes(t *testing.T) {
	candidates := []NodeCandidate{
		{NodeID: "bad", Region: domain.RegionAPSouth, Reputation: 0.2, CurrentLoad: 0.9, GPUAvailable: true},
		{NodeID: "good", Region: domain.RegionUSEast, Reputation: 0.95, CurrentLoad: 0.1, HasModelHot: true, GPUAvailable: true},
		{NodeID: "mid", Region: domain.RegionUSEast, Reputation: 0.5, CurrentLoad: 0.5, GPUAvailable: true},
	}
	task := domain.Task{Type: domain.TaskInference}
	ranked := RankNodes(candidates, task, domain.RegionUSEast)
	if len(ranked) != 3 {
		t.Fatalf("RankNodes() returned %d, want 3", len(ranked))
	}
	if ranked[0].NodeID != "good" {
		t.Errorf("best node = %q, want %q", ranked[0].NodeID, "good")
	}
}

// ─── Stats ──────────────────────────────────────────────────────────────────

func TestScheduler_Stats(t *testing.T) {
	s := newTestScheduler(t)
	task := domain.Task{ID: "t", Priority: P2Normal, Status: domain.TaskQueued, Type: domain.TaskInference}
	s.Enqueue(task, domain.TaskRouting{})
	s.Dequeue()
	s.MarkCompleted()

	stats := s.Stats()
	if stats.TotalEnqueued != 1 {
		t.Errorf("TotalEnqueued = %d, want 1", stats.TotalEnqueued)
	}
	if stats.TotalCompleted != 1 {
		t.Errorf("TotalCompleted = %d, want 1", stats.TotalCompleted)
	}
}
