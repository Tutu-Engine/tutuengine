// Package scheduler implements the Phase 3 advanced task scheduler.
// Architecture Part IX: Work stealing, back-pressure, preemption, weighted scored matching.
//
// Core concepts:
//   - WorkQueue: a per-node double-ended queue (deque) of tasks
//   - Work Stealing: idle nodes steal from the TOP of busy peers' queues
//   - Back-Pressure: tiered rejection at queue depths 1K/5K/10K
//   - Preemption: realtime tasks can preempt spot tasks
//   - Scored Matching: O(K) weighted scoring across candidates after filter
package scheduler

import (
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ─── Configuration ──────────────────────────────────────────────────────────

// Config configures the advanced scheduler.
type Config struct {
	MaxQueueDepth      int           // default 10_000
	BackPressureSoft   int           // warn + reject low-priority at this depth (default 1_000)
	BackPressureMedium int           // reject all except realtime (default 5_000)
	BackPressureHard   int           // reject everything (default 10_000)
	StealBatchSize     int           // how many tasks to steal at once (default: half of peer's queue)
	StarvationInterval time.Duration // boost priority every N (default 60s)
	PreemptionEnabled  bool          // allow realtime to preempt spot (default true)
}

// DefaultConfig returns production scheduler defaults.
func DefaultConfig() Config {
	return Config{
		MaxQueueDepth:      10_000,
		BackPressureSoft:   1_000,
		BackPressureMedium: 5_000,
		BackPressureHard:   10_000,
		StealBatchSize:     0, // 0 means "half of peer's queue"
		StarvationInterval: 60 * time.Second,
		PreemptionEnabled:  true,
	}
}

// ─── Priority Classes (Architecture Part IX) ────────────────────────────────

const (
	P0Realtime = 0 // MCP realtime tier — dual-node race
	P1High     = 1 // Enterprise standard tier
	P2Normal   = 2 // Regular user tasks
	P3Low      = 3 // Batch processing
	P4Spot     = 4 // Best-effort / spot pricing
)

// PriorityLabel returns a human-readable label for a priority class.
func PriorityLabel(p int) string {
	switch p {
	case P0Realtime:
		return "REALTIME"
	case P1High:
		return "HIGH"
	case P2Normal:
		return "NORMAL"
	case P3Low:
		return "LOW"
	case P4Spot:
		return "SPOT"
	default:
		return "UNKNOWN"
	}
}

// ─── Queued Task ────────────────────────────────────────────────────────────

// QueuedTask wraps a domain.Task with scheduling metadata.
type QueuedTask struct {
	Task     domain.Task
	QueuedAt time.Time
	Routing  domain.TaskRouting
}

// EffectivePriority applies starvation-prevention age boost.
// Every starvationInterval in queue, priority improves by 1 class.
func (qt QueuedTask) EffectivePriority(starvationInterval time.Duration) int {
	age := time.Since(qt.QueuedAt)
	boost := int(age / starvationInterval)
	effective := qt.Task.Priority - boost
	if effective < 0 {
		effective = 0
	}
	return effective
}

// ─── Back-Pressure Levels ───────────────────────────────────────────────────

// BackPressureLevel indicates load severity.
type BackPressureLevel int

const (
	BPNone   BackPressureLevel = iota // accepting all tasks
	BPSoft                            // rejecting P4 spot tasks
	BPMedium                          // rejecting all except P0 realtime
	BPHard                            // rejecting everything
)

// String returns a human-readable back-pressure level.
func (bp BackPressureLevel) String() string {
	switch bp {
	case BPNone:
		return "NONE"
	case BPSoft:
		return "SOFT"
	case BPMedium:
		return "MEDIUM"
	case BPHard:
		return "HARD"
	default:
		return "UNKNOWN"
	}
}

// ─── Scheduler ──────────────────────────────────────────────────────────────

// Scheduler is the Phase 3 advanced task scheduler with work stealing,
// back-pressure, preemption, and starvation prevention.
type Scheduler struct {
	mu     sync.Mutex
	config Config

	// Priority queues — one per priority class (P0–P4)
	queues [5][]QueuedTask

	// Stats
	totalEnqueued  atomic.Int64
	totalCompleted atomic.Int64
	totalRejected  atomic.Int64
	totalStolen    atomic.Int64
	totalPreempted atomic.Int64
}

// NewScheduler creates a new advanced scheduler.
func NewScheduler(cfg Config) *Scheduler {
	return &Scheduler{config: cfg}
}

// ─── Enqueue ────────────────────────────────────────────────────────────────

// Enqueue adds a task to the appropriate priority queue.
// Returns an error if back-pressure rejects the task.
func (s *Scheduler) Enqueue(task domain.Task, routing domain.TaskRouting) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	depth := s.queueDepthLocked()
	bp := s.backPressureLevelLocked(depth)

	// Back-pressure rejection
	switch bp {
	case BPHard:
		s.totalRejected.Add(1)
		return domain.ErrBackPressureHard
	case BPMedium:
		if task.Priority > P0Realtime {
			s.totalRejected.Add(1)
			return domain.ErrBackPressureMedium
		}
	case BPSoft:
		if task.Priority >= P4Spot {
			s.totalRejected.Add(1)
			return domain.ErrBackPressureSoft
		}
	}

	qt := QueuedTask{
		Task:     task,
		QueuedAt: time.Now(),
		Routing:  routing,
	}

	// Clamp priority to valid range [0, 4]
	pClass := task.Priority
	if pClass < 0 {
		pClass = 0
	}
	if pClass > 4 {
		pClass = 4
	}

	s.queues[pClass] = append(s.queues[pClass], qt)
	s.totalEnqueued.Add(1)
	return nil
}

// ─── Dequeue ────────────────────────────────────────────────────────────────

// Dequeue removes and returns the highest-priority task.
// Returns nil if all queues are empty.
// Uses starvation prevention: tasks waiting longer get priority boosts.
func (s *Scheduler) Dequeue() *QueuedTask {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Scan from highest priority (P0) to lowest (P4).
	// Within each queue, find the task with the best effective priority.
	var bestIdx int = -1
	var bestQueue int = -1
	var bestEffective int = math.MaxInt

	for q := 0; q < 5; q++ {
		for i, qt := range s.queues[q] {
			eff := qt.EffectivePriority(s.config.StarvationInterval)
			if eff < bestEffective {
				bestEffective = eff
				bestIdx = i
				bestQueue = q
			}
		}
	}

	if bestIdx < 0 {
		return nil // all empty
	}

	qt := s.queues[bestQueue][bestIdx]
	// Remove from queue (swap with last for O(1))
	last := len(s.queues[bestQueue]) - 1
	s.queues[bestQueue][bestIdx] = s.queues[bestQueue][last]
	s.queues[bestQueue] = s.queues[bestQueue][:last]

	return &qt
}

// ─── Preemption ─────────────────────────────────────────────────────────────

// Preempt checks if a realtime task should preempt a running spot task.
// Returns the spot task to be preempted (checkpointed and re-queued), or nil.
func (s *Scheduler) Preempt(realtimeTask domain.Task, runningTasks []domain.Task) *domain.Task {
	if !s.config.PreemptionEnabled {
		return nil
	}
	if realtimeTask.Priority > P0Realtime {
		return nil // only realtime can preempt
	}

	// Find the lowest-priority running task (prefer P4 spot tasks).
	var victim *domain.Task
	for i := range runningTasks {
		t := &runningTasks[i]
		if t.Priority >= P4Spot && !t.IsTerminal() {
			if victim == nil || t.Priority > victim.Priority {
				victim = t
			}
		}
	}

	if victim != nil {
		s.totalPreempted.Add(1)
	}
	return victim
}

// ─── Work Stealing ──────────────────────────────────────────────────────────

// StealableTasks returns tasks that can be stolen by an idle peer.
// Takes from the TOP (oldest) of queues — FIFO for thieves.
// Returns up to half the queue depth (or StealBatchSize if configured).
func (s *Scheduler) StealableTasks(maxCount int) []QueuedTask {
	s.mu.Lock()
	defer s.mu.Unlock()

	if maxCount <= 0 {
		total := s.queueDepthLocked()
		maxCount = total / 2
	}
	if maxCount <= 0 {
		return nil
	}

	stolen := make([]QueuedTask, 0, maxCount)
	// Steal from lowest priority first (P4 → P0)
	for q := 4; q >= 0 && len(stolen) < maxCount; q-- {
		canTake := maxCount - len(stolen)
		if canTake > len(s.queues[q]) {
			canTake = len(s.queues[q])
		}
		// Take from the front (oldest = FIFO for thieves)
		stolen = append(stolen, s.queues[q][:canTake]...)
		s.queues[q] = s.queues[q][canTake:]
	}

	s.totalStolen.Add(int64(len(stolen)))
	return stolen
}

// ImportStolenTasks adds stolen tasks to local queues.
func (s *Scheduler) ImportStolenTasks(tasks []QueuedTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, qt := range tasks {
		pClass := qt.Task.Priority
		if pClass < 0 {
			pClass = 0
		}
		if pClass > 4 {
			pClass = 4
		}
		s.queues[pClass] = append(s.queues[pClass], qt)
		s.totalEnqueued.Add(1)
	}
}

// ─── Stats & Inspection ─────────────────────────────────────────────────────

// Stats returns scheduler statistics.
type Stats struct {
	QueueDepth     int               `json:"queue_depth"`
	BackPressure   BackPressureLevel `json:"back_pressure"`
	QueueByClass   [5]int            `json:"queue_by_class"`
	TotalEnqueued  int64             `json:"total_enqueued"`
	TotalCompleted int64             `json:"total_completed"`
	TotalRejected  int64             `json:"total_rejected"`
	TotalStolen    int64             `json:"total_stolen"`
	TotalPreempted int64             `json:"total_preempted"`
}

// Stats returns current scheduler statistics.
func (s *Scheduler) Stats() Stats {
	s.mu.Lock()
	depth := s.queueDepthLocked()
	bp := s.backPressureLevelLocked(depth)
	var byClass [5]int
	for i := 0; i < 5; i++ {
		byClass[i] = len(s.queues[i])
	}
	s.mu.Unlock()

	return Stats{
		QueueDepth:     depth,
		BackPressure:   bp,
		QueueByClass:   byClass,
		TotalEnqueued:  s.totalEnqueued.Load(),
		TotalCompleted: s.totalCompleted.Load(),
		TotalRejected:  s.totalRejected.Load(),
		TotalStolen:    s.totalStolen.Load(),
		TotalPreempted: s.totalPreempted.Load(),
	}
}

// QueueDepth returns total tasks across all priority queues.
func (s *Scheduler) QueueDepth() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queueDepthLocked()
}

// BackPressureLevel returns the current back-pressure level.
func (s *Scheduler) BackPressureLevel() BackPressureLevel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.backPressureLevelLocked(s.queueDepthLocked())
}

// MarkCompleted records that a task has been completed.
func (s *Scheduler) MarkCompleted() {
	s.totalCompleted.Add(1)
}

// ─── Internal ───────────────────────────────────────────────────────────────

func (s *Scheduler) queueDepthLocked() int {
	total := 0
	for i := 0; i < 5; i++ {
		total += len(s.queues[i])
	}
	return total
}

func (s *Scheduler) backPressureLevelLocked(depth int) BackPressureLevel {
	switch {
	case depth >= s.config.BackPressureHard:
		return BPHard
	case depth >= s.config.BackPressureMedium:
		return BPMedium
	case depth >= s.config.BackPressureSoft:
		return BPSoft
	default:
		return BPNone
	}
}

// ─── Node Scoring (Weighted Scored Matching) ────────────────────────────────

// NodeCandidate represents a potential task executor.
type NodeCandidate struct {
	NodeID       string
	Region       domain.RegionID
	Reputation   float64 // [0.0, 1.0]
	CurrentLoad  float64 // [0.0, 1.0]
	LatencyMs    float64
	HasModelHot  bool    // model already loaded in memory?
	CreditRate   float64 // cost per task
	GPUAvailable bool
	VRAMGB       float64
}

// ScoreNode computes the weighted match score for a node to execute a task.
// Higher score = better match. Score of 0 means node is disqualified.
//
// Weights (Architecture Part IX):
//
//	hardware: 20%  reputation: 20%  locality: 15%  availability: 15%
//	latency: 10%   cache: 15%       cost: 5%
func ScoreNode(node NodeCandidate, task domain.Task, taskRegion domain.RegionID) float64 {
	// Hardware check
	hw := 1.0
	if task.Type == domain.TaskFineTune && !node.GPUAvailable {
		return 0 // hard disqualification
	}

	// Reputation [0, 1]
	rep := node.Reputation

	// Locality
	loc := 0.0
	if node.Region == taskRegion {
		loc = 1.0
	} else {
		latMs := domain.RegionLatencyMs(node.Region, taskRegion)
		loc = 1.0 / (1.0 + float64(latMs)/100.0)
	}

	// Availability (inverse of load)
	avail := 1.0 - node.CurrentLoad
	if avail < 0 {
		avail = 0
	}

	// Latency score
	lat := 1.0 / (1.0 + node.LatencyMs/100.0)

	// Cache hit bonus
	cache := 0.0
	if node.HasModelHot {
		cache = 1.0
	}

	// Cost (lower is better)
	cost := 1.0 / (1.0 + node.CreditRate/10.0)

	return 0.20*hw + 0.20*rep + 0.15*loc + 0.15*avail +
		0.10*lat + 0.15*cache + 0.05*cost
}

// RankNodes scores and sorts candidates. Returns sorted best-first.
func RankNodes(candidates []NodeCandidate, task domain.Task, taskRegion domain.RegionID) []NodeCandidate {
	type scored struct {
		node  NodeCandidate
		score float64
	}

	all := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		s := ScoreNode(c, task, taskRegion)
		if s > 0 {
			all = append(all, scored{node: c, score: s})
		}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].score > all[j].score
	})

	ranked := make([]NodeCandidate, len(all))
	for i, s := range all {
		ranked[i] = s.node
	}
	return ranked
}
