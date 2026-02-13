// Package scheduler — retry_queue.go integrates DSA concepts for retry scheduling.
// Uses the DSA PriorityQueue (min-heap) for O(log n) retry scheduling with
// exponential backoff and starvation prevention.
//
// Architecture: Failed tasks are inserted with a retry priority that increases
// with each attempt. The heap's starvation prevention ensures tasks with many
// retries don't starve. Combined with consistent hash ring for node affinity.
package scheduler

import (
	"sync"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/infra/dsa"
)

// ─── DSA Retry Queue ────────────────────────────────────────────────────────
// Architecture Part XX: Priority-aware retry scheduling using min-heap.
// Failed tasks are re-queued with increasing delay using exponential backoff.
// The heap guarantees O(log n) insertion and O(log n) extraction of the
// next-to-retry task.

// RetryConfig configures the retry queue behavior.
type RetryConfig struct {
	MaxRetries    int           // Maximum retry attempts before permanent failure
	BaseDelay     time.Duration // Initial backoff delay (doubles each retry)
	MaxDelay      time.Duration // Cap on backoff delay
	BoostInterval time.Duration // Starvation prevention: boost every N
}

// DefaultRetryConfig returns production retry defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:    5,
		BaseDelay:     1 * time.Second,
		MaxDelay:      60 * time.Second,
		BoostInterval: 5 * time.Minute,
	}
}

// RetryEntry tracks a failed task's retry state.
type RetryEntry struct {
	Task      domain.Task
	Routing   domain.TaskRouting
	Attempt   int       // Current retry attempt (0 = first try)
	NextRetry time.Time // Earliest time this can be retried
	FailedAt  time.Time // When the last failure occurred
	Error     string    // Last failure reason
}

// RetryQueue uses the DSA min-heap to schedule task retries with
// exponential backoff and starvation prevention.
type RetryQueue struct {
	mu     sync.Mutex
	config RetryConfig
	heap   *dsa.PriorityQueue
	ring   *dsa.HashRing // For node-affinity on retry placement

	// Stats
	totalRetries   int64
	totalExhausted int64 // Tasks that exceeded MaxRetries
}

// NewRetryQueue creates a retry queue backed by a DSA priority queue.
func NewRetryQueue(cfg RetryConfig) *RetryQueue {
	return &RetryQueue{
		config: cfg,
		heap: dsa.NewPriorityQueue(dsa.PriorityQueueConfig{
			BoostInterval: cfg.BoostInterval,
			MaxBoost:      2, // Allow up to 2 priority levels of starvation boost
		}),
		ring: dsa.NewHashRing(dsa.DefaultHashRingConfig()),
	}
}

// AddRetryNode registers a node in the consistent hash ring for retry affinity.
// When a task fails on one node, the ring helps select an alternative node.
func (rq *RetryQueue) AddRetryNode(nodeID string) {
	rq.ring.AddNode(nodeID)
}

// RemoveRetryNode removes a node from the retry affinity ring.
func (rq *RetryQueue) RemoveRetryNode(nodeID string) {
	rq.ring.RemoveNode(nodeID)
}

// ScheduleRetry adds a failed task to the retry queue with exponential backoff.
// Returns false if the task has exceeded MaxRetries.
func (rq *RetryQueue) ScheduleRetry(entry RetryEntry) bool {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	entry.Attempt++
	if entry.Attempt > rq.config.MaxRetries {
		rq.totalExhausted++
		return false // Permanent failure
	}

	// Exponential backoff: baseDelay * 2^(attempt-1)
	delay := rq.config.BaseDelay
	for i := 1; i < entry.Attempt; i++ {
		delay *= 2
		if delay > rq.config.MaxDelay {
			delay = rq.config.MaxDelay
			break
		}
	}

	entry.NextRetry = time.Now().Add(delay)
	entry.FailedAt = time.Now()

	// Priority: base task priority + attempt penalty (so fresh retries go first)
	retryPriority := entry.Task.Priority + entry.Attempt

	rq.heap.Push(dsa.HeapItem{
		Key:         entry.Task.ID,
		Priority:    retryPriority,
		SubmittedAt: entry.FailedAt,
		Value:       entry,
	})

	rq.totalRetries++
	return true
}

// NextReady returns the next task ready to be retried, if any.
// Only returns tasks whose NextRetry time has passed.
func (rq *RetryQueue) NextReady() (*RetryEntry, bool) {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	item, ok := rq.heap.Peek()
	if !ok {
		return nil, false
	}

	entry, ok := item.Value.(RetryEntry)
	if !ok {
		// Corrupted entry — discard
		rq.heap.Pop()
		return nil, false
	}

	if time.Now().Before(entry.NextRetry) {
		return nil, false // Not yet ready
	}

	// Pop it — ready to retry
	rq.heap.Pop()
	return &entry, true
}

// DrainReady drains all ready-to-retry tasks. Returns them in priority order.
func (rq *RetryQueue) DrainReady() []RetryEntry {
	var ready []RetryEntry
	for {
		entry, ok := rq.NextReady()
		if !ok {
			break
		}
		ready = append(ready, *entry)
	}
	return ready
}

// SuggestNode uses the consistent hash ring to pick a node for retrying a task,
// preferring a DIFFERENT node than the one that failed.
func (rq *RetryQueue) SuggestNode(taskID string, failedNode string) string {
	candidates := rq.ring.LookupN(taskID, 3)
	for _, c := range candidates {
		if c != failedNode {
			return c
		}
	}
	// All candidates are the same node (or ring is empty) — return first anyway
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

// Len returns the number of tasks pending retry.
func (rq *RetryQueue) Len() int {
	return rq.heap.Len()
}

// RetryStats holds retry queue statistics.
type RetryStats struct {
	PendingRetries int   `json:"pending_retries"`
	TotalRetries   int64 `json:"total_retries"`
	TotalExhausted int64 `json:"total_exhausted"` // Exceeded MaxRetries
	RetryNodes     int   `json:"retry_nodes"`      // Nodes in hash ring
}

// RetryStats returns current retry queue statistics.
func (rq *RetryQueue) RetryStats() RetryStats {
	rq.mu.Lock()
	pending := rq.heap.Len()
	retries := rq.totalRetries
	exhausted := rq.totalExhausted
	rq.mu.Unlock()

	return RetryStats{
		PendingRetries: pending,
		TotalRetries:   retries,
		TotalExhausted: exhausted,
		RetryNodes:     rq.ring.Size(),
	}
}
