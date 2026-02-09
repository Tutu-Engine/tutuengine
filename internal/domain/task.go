// Package domain — Phase 1 task types.
// A Task is a unit of work that flows through the distributed network:
// submit → schedule → assign → execute → verify → credit.
package domain

import "time"

// TaskStatus tracks task lifecycle.
type TaskStatus string

const (
	TaskQueued    TaskStatus = "QUEUED"
	TaskAssigned  TaskStatus = "ASSIGNED"
	TaskExecuting TaskStatus = "EXECUTING"
	TaskCompleted TaskStatus = "COMPLETED"
	TaskFailed    TaskStatus = "FAILED"
	TaskCancelled TaskStatus = "CANCELLED"
)

// TaskType categorizes the kind of computation.
type TaskType string

const (
	TaskInference TaskType = "INFERENCE"
	TaskEmbedding TaskType = "EMBEDDING"
	TaskFineTune  TaskType = "FINE_TUNE"
	TaskAgent     TaskType = "AGENT"
)

// Task is a unit of distributed work.
type Task struct {
	ID          string     `json:"id"`
	Type        TaskType   `json:"type"`
	Status      TaskStatus `json:"status"`
	Priority    int        `json:"priority"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   time.Time  `json:"started_at,omitempty"`
	CompletedAt time.Time  `json:"completed_at,omitempty"`
	Credits     int64      `json:"credits,omitempty"`
	ResultHash  string     `json:"result_hash,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// IsTerminal returns true if the task has reached a final state.
func (t *Task) IsTerminal() bool {
	return t.Status == TaskCompleted || t.Status == TaskFailed || t.Status == TaskCancelled
}

// Duration returns how long the task took to execute (0 if not started/completed).
func (t *Task) Duration() time.Duration {
	if t.StartedAt.IsZero() || t.CompletedAt.IsZero() {
		return 0
	}
	return t.CompletedAt.Sub(t.StartedAt)
}
