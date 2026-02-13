package scheduler

import (
	"testing"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ─── Retry Queue Tests ──────────────────────────────────────────────────────

func TestRetryQueue_ScheduleAndDrain(t *testing.T) {
	rq := NewRetryQueue(RetryConfig{
		MaxRetries:    3,
		BaseDelay:     1 * time.Millisecond, // Tiny for testing
		MaxDelay:      100 * time.Millisecond,
		BoostInterval: 5 * time.Minute,
	})

	entry := RetryEntry{
		Task: domain.Task{
			ID:       "task-1",
			Priority: P2Normal,
		},
		Routing: domain.TaskRouting{},
		Error:   "timeout",
	}

	// Schedule first retry
	ok := rq.ScheduleRetry(entry)
	if !ok {
		t.Fatal("expected ScheduleRetry to succeed for first retry")
	}
	if rq.Len() != 1 {
		t.Fatalf("expected 1 pending retry, got %d", rq.Len())
	}

	// Wait for backoff to expire
	time.Sleep(5 * time.Millisecond)

	// Drain ready tasks
	ready := rq.DrainReady()
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready retry, got %d", len(ready))
	}
	if ready[0].Task.ID != "task-1" {
		t.Errorf("got task ID %q, want task-1", ready[0].Task.ID)
	}
	if ready[0].Attempt != 1 {
		t.Errorf("attempt = %d, want 1", ready[0].Attempt)
	}
}

func TestRetryQueue_MaxRetriesExhausted(t *testing.T) {
	rq := NewRetryQueue(RetryConfig{
		MaxRetries:    2,
		BaseDelay:     1 * time.Millisecond,
		MaxDelay:      10 * time.Millisecond,
		BoostInterval: 5 * time.Minute,
	})

	entry := RetryEntry{
		Task: domain.Task{ID: "task-exhaust", Priority: P3Low},
	}

	// Attempt 1
	ok := rq.ScheduleRetry(entry)
	if !ok {
		t.Fatal("retry 1 should succeed")
	}

	// Attempt 2
	entry.Attempt = 1
	ok = rq.ScheduleRetry(entry)
	if !ok {
		t.Fatal("retry 2 should succeed")
	}

	// Attempt 3 = exceeds max of 2
	entry.Attempt = 2
	ok = rq.ScheduleRetry(entry)
	if ok {
		t.Fatal("retry 3 should fail (exceeds MaxRetries=2)")
	}

	stats := rq.RetryStats()
	if stats.TotalExhausted != 1 {
		t.Errorf("exhausted = %d, want 1", stats.TotalExhausted)
	}
}

func TestRetryQueue_ExponentialBackoff(t *testing.T) {
	rq := NewRetryQueue(RetryConfig{
		MaxRetries:    5,
		BaseDelay:     10 * time.Millisecond,
		MaxDelay:      1 * time.Second,
		BoostInterval: 5 * time.Minute,
	})

	entry := RetryEntry{
		Task: domain.Task{ID: "backoff-test", Priority: P2Normal},
	}

	// First retry — should not be ready immediately
	rq.ScheduleRetry(entry)
	_, ready := rq.NextReady()
	if ready {
		t.Error("task should not be ready immediately (10ms backoff)")
	}

	// Wait and check
	time.Sleep(15 * time.Millisecond)
	_, ready = rq.NextReady()
	if !ready {
		t.Error("task should be ready after 15ms (10ms backoff)")
	}
}

func TestRetryQueue_PriorityOrdering(t *testing.T) {
	rq := NewRetryQueue(RetryConfig{
		MaxRetries:    5,
		BaseDelay:     1 * time.Millisecond,
		MaxDelay:      10 * time.Millisecond,
		BoostInterval: 5 * time.Minute,
	})

	// Schedule high-priority and low-priority retries
	rq.ScheduleRetry(RetryEntry{
		Task: domain.Task{ID: "low", Priority: P4Spot},
	})
	rq.ScheduleRetry(RetryEntry{
		Task: domain.Task{ID: "high", Priority: P0Realtime},
	})

	time.Sleep(5 * time.Millisecond)

	ready := rq.DrainReady()
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready, got %d", len(ready))
	}
	// Min-heap: lower priority number = higher priority = comes first
	if ready[0].Task.ID != "high" {
		t.Errorf("first task should be 'high' (P0), got %q", ready[0].Task.ID)
	}
}

func TestRetryQueue_SuggestNode(t *testing.T) {
	rq := NewRetryQueue(DefaultRetryConfig())

	rq.AddRetryNode("node-a")
	rq.AddRetryNode("node-b")
	rq.AddRetryNode("node-c")

	// SuggestNode should return a different node than the failed one
	suggested := rq.SuggestNode("task-x", "node-a")
	if suggested == "node-a" {
		t.Error("suggested node should differ from failed node when alternatives exist")
	}
	if suggested == "" {
		t.Error("suggested node should not be empty with 3 nodes")
	}
}

func TestRetryQueue_EmptyQueue(t *testing.T) {
	rq := NewRetryQueue(DefaultRetryConfig())

	_, ok := rq.NextReady()
	if ok {
		t.Error("empty queue should return not ready")
	}

	ready := rq.DrainReady()
	if len(ready) != 0 {
		t.Errorf("empty drain should return 0 items, got %d", len(ready))
	}
}

func TestRetryQueue_Stats(t *testing.T) {
	rq := NewRetryQueue(RetryConfig{
		MaxRetries:    1,
		BaseDelay:     1 * time.Millisecond,
		MaxDelay:      10 * time.Millisecond,
		BoostInterval: 5 * time.Minute,
	})

	rq.AddRetryNode("n1")
	rq.AddRetryNode("n2")

	rq.ScheduleRetry(RetryEntry{
		Task: domain.Task{ID: "s1", Priority: P2Normal},
	})
	rq.ScheduleRetry(RetryEntry{
		Task:    domain.Task{ID: "s2", Priority: P2Normal},
		Attempt: 1, // Already at max
	})

	stats := rq.RetryStats()
	if stats.PendingRetries != 1 {
		t.Errorf("pending = %d, want 1", stats.PendingRetries)
	}
	if stats.TotalRetries != 1 {
		t.Errorf("total retries = %d, want 1", stats.TotalRetries)
	}
	if stats.TotalExhausted != 1 {
		t.Errorf("exhausted = %d, want 1", stats.TotalExhausted)
	}
	if stats.RetryNodes != 2 {
		t.Errorf("retry nodes = %d, want 2", stats.RetryNodes)
	}
}
