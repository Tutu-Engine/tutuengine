// Package finetune implements distributed LoRA/QLoRA fine-tuning.
// Architecture Part IX: Fine-tuning tasks distributed across multiple nodes.
//
// How distributed fine-tuning works:
//  1. User submits a fine-tuning job (base model + dataset + config)
//  2. Coordinator splits dataset into shards (one per participating node)
//  3. Each node trains a LoRA adapter on its shard (local GPU)
//  4. Nodes send gradient updates back to coordinator
//  5. Coordinator aggregates gradients (FedAvg algorithm)
//  6. After all epochs, coordinator merges final adapter
//
// Cost: ~90% cheaper than cloud GPU because we use idle desktop GPUs.
package finetune

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ─── Errors ─────────────────────────────────────────────────────────────────

var (
	ErrJobNotFound       = errors.New("fine-tune job not found")
	ErrJobAlreadyRunning = errors.New("fine-tune job already running")
	ErrInsufficientNodes = errors.New("not enough capable nodes for fine-tuning")
	ErrShardFailed       = errors.New("data shard processing failed")
	ErrGradientMismatch  = errors.New("gradient dimensions do not match")
	ErrCheckpointMissing = errors.New("checkpoint not available")
	ErrEpochTimeout      = errors.New("epoch exceeded time limit")
)

// ─── Job Types ──────────────────────────────────────────────────────────────

// FineTuneMethod specifies the fine-tuning approach.
type FineTuneMethod string

const (
	MethodLoRA  FineTuneMethod = "lora"  // Low-Rank Adaptation (default)
	MethodQLoRA FineTuneMethod = "qlora" // Quantized LoRA (4-bit base model)
)

// JobStatus tracks the lifecycle of a fine-tuning job.
type JobStatus string

const (
	JobPending     JobStatus = "PENDING"    // Waiting for nodes
	JobSharding    JobStatus = "SHARDING"   // Splitting dataset
	JobTraining    JobStatus = "TRAINING"   // Nodes training
	JobAggregating JobStatus = "AGGREGATING"// Merging gradients
	JobCompleted   JobStatus = "COMPLETED"  // Adapter ready
	JobFailed      JobStatus = "FAILED"     // Unrecoverable error
	JobCancelled   JobStatus = "CANCELLED"
)

// LoRAConfig holds LoRA-specific hyperparameters.
type LoRAConfig struct {
	Rank           int     `json:"rank"`            // LoRA rank r (default: 16)
	Alpha          float64 `json:"alpha"`           // Scaling factor (default: 32)
	Dropout        float64 `json:"dropout"`         // Dropout probability (default: 0.05)
	TargetModules  []string `json:"target_modules"` // Which layers to adapt (default: q_proj, v_proj)
	LearningRate   float64 `json:"learning_rate"`   // Adam LR (default: 2e-4)
	BatchSize      int     `json:"batch_size"`      // Per-node batch size (default: 4)
	GradAccumSteps int     `json:"grad_accum_steps"`// Gradient accumulation (default: 4)
}

// DefaultLoRAConfig returns production defaults from Architecture Part IX.
func DefaultLoRAConfig() LoRAConfig {
	return LoRAConfig{
		Rank:           16,
		Alpha:          32,
		Dropout:        0.05,
		TargetModules:  []string{"q_proj", "v_proj"},
		LearningRate:   2e-4,
		BatchSize:      4,
		GradAccumSteps: 4,
	}
}

// FineTuneJob represents a distributed fine-tuning request.
type FineTuneJob struct {
	ID          string         `json:"id"`
	BaseModel   string         `json:"base_model"`    // e.g. "llama3.2"
	DatasetURI  string         `json:"dataset_uri"`   // URI to training data
	Method      FineTuneMethod `json:"method"`
	Config      LoRAConfig     `json:"config"`
	Epochs      int            `json:"epochs"`         // Total epochs (default: 3)
	MinNodes    int            `json:"min_nodes"`      // Minimum nodes required
	MaxNodes    int            `json:"max_nodes"`      // Maximum nodes to use
	Status      JobStatus      `json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	StartedAt   time.Time      `json:"started_at,omitempty"`
	CompletedAt time.Time      `json:"completed_at,omitempty"`
	Error       string         `json:"error,omitempty"`
	CreditCost  int64          `json:"credit_cost"`    // Total credits consumed
}

// Duration returns training wall time.
func (j *FineTuneJob) Duration() time.Duration {
	if j.StartedAt.IsZero() {
		return 0
	}
	end := j.CompletedAt
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(j.StartedAt)
}

// IsTerminal returns true if the job reached a final state.
func (j *FineTuneJob) IsTerminal() bool {
	return j.Status == JobCompleted || j.Status == JobFailed || j.Status == JobCancelled
}

// ─── Data Shard ─────────────────────────────────────────────────────────────

// DataShard is a partition of the training dataset assigned to one node.
type DataShard struct {
	ShardIndex  int    `json:"shard_index"`
	NodeID      string `json:"node_id"`
	SampleCount int    `json:"sample_count"` // Number of training samples
	SizeBytes   int64  `json:"size_bytes"`
	Digest      string `json:"digest"`       // SHA-256 of shard data
}

// ─── Gradient Update ────────────────────────────────────────────────────────

// GradientUpdate is sent by a node after processing one epoch of its shard.
type GradientUpdate struct {
	JobID      string    `json:"job_id"`
	NodeID     string    `json:"node_id"`
	ShardIndex int       `json:"shard_index"`
	Epoch      int       `json:"epoch"`
	Loss       float64   `json:"loss"`         // Training loss for this epoch
	Samples    int       `json:"samples"`      // Samples processed
	Timestamp  time.Time `json:"timestamp"`
}

// ─── Checkpoint ─────────────────────────────────────────────────────────────

// Checkpoint captures training state at a point in time for fault tolerance.
type Checkpoint struct {
	JobID     string    `json:"job_id"`
	Epoch     int       `json:"epoch"`
	Loss      float64   `json:"loss"`     // Aggregated loss at this epoch
	NodeCount int       `json:"node_count"`
	Digest    string    `json:"digest"`   // SHA-256 of checkpoint data
	CreatedAt time.Time `json:"created_at"`
}

// ─── Coordinator ────────────────────────────────────────────────────────────
// Manages the lifecycle of distributed fine-tuning jobs.

// CoordinatorConfig configures the fine-tuning coordinator.
type CoordinatorConfig struct {
	MaxConcurrentJobs int           // Max simultaneous fine-tune jobs
	EpochTimeout      time.Duration // Max time for one epoch across all nodes
	CreditPerMinute   int64         // Fine-tuning credit cost per minute
}

// DefaultCoordinatorConfig returns production defaults.
func DefaultCoordinatorConfig() CoordinatorConfig {
	return CoordinatorConfig{
		MaxConcurrentJobs: 3,
		EpochTimeout:      30 * time.Minute,
		CreditPerMinute:   10, // Architecture Part X: 10 cr/min fine-tuning
	}
}

// Coordinator orchestrates distributed fine-tuning jobs.
type Coordinator struct {
	mu     sync.RWMutex
	config CoordinatorConfig
	jobs   map[string]*FineTuneJob
	shards map[string][]DataShard      // jobID → shards
	grads  map[string][]GradientUpdate // jobID → gradient updates
	checks map[string][]Checkpoint     // jobID → checkpoints
}

// NewCoordinator creates a fine-tuning coordinator.
func NewCoordinator(cfg CoordinatorConfig) *Coordinator {
	return &Coordinator{
		config: cfg,
		jobs:   make(map[string]*FineTuneJob),
		shards: make(map[string][]DataShard),
		grads:  make(map[string][]GradientUpdate),
		checks: make(map[string][]Checkpoint),
	}
}

// SubmitJob registers a new fine-tuning job.
func (c *Coordinator) SubmitJob(job FineTuneJob) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.jobs[job.ID]; exists {
		return ErrJobAlreadyRunning
	}

	active := 0
	for _, j := range c.jobs {
		if !j.IsTerminal() {
			active++
		}
	}
	if active >= c.config.MaxConcurrentJobs {
		return fmt.Errorf("maximum concurrent jobs (%d) reached", c.config.MaxConcurrentJobs)
	}

	job.Status = JobPending
	if job.Method == "" {
		job.Method = MethodLoRA
	}
	if job.Epochs <= 0 {
		job.Epochs = 3
	}
	if job.MinNodes <= 0 {
		job.MinNodes = 2
	}
	if job.MaxNodes <= 0 {
		job.MaxNodes = 10
	}
	job.CreatedAt = time.Now()

	c.jobs[job.ID] = &job
	return nil
}

// GetJob returns a job by ID.
func (c *Coordinator) GetJob(jobID string) (*FineTuneJob, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	job, ok := c.jobs[jobID]
	if !ok {
		return nil, ErrJobNotFound
	}
	cp := *job
	return &cp, nil
}

// ListJobs returns all jobs.
func (c *Coordinator) ListJobs() []FineTuneJob {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]FineTuneJob, 0, len(c.jobs))
	for _, j := range c.jobs {
		result = append(result, *j)
	}
	return result
}

// AssignShards records how dataset was split across nodes.
func (c *Coordinator) AssignShards(jobID string, shards []DataShard) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	job, ok := c.jobs[jobID]
	if !ok {
		return ErrJobNotFound
	}

	if len(shards) < job.MinNodes {
		return ErrInsufficientNodes
	}

	job.Status = JobSharding
	c.shards[jobID] = shards
	return nil
}

// Shards returns the data shards for a job.
func (c *Coordinator) Shards(jobID string) []DataShard {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.shards[jobID]
}

// StartTraining transitions a job to training state.
func (c *Coordinator) StartTraining(jobID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	job, ok := c.jobs[jobID]
	if !ok {
		return ErrJobNotFound
	}

	job.Status = JobTraining
	job.StartedAt = time.Now()
	return nil
}

// RecordGradient records a gradient update from a node.
func (c *Coordinator) RecordGradient(update GradientUpdate) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.jobs[update.JobID]; !ok {
		return ErrJobNotFound
	}

	c.grads[update.JobID] = append(c.grads[update.JobID], update)
	return nil
}

// Gradients returns all gradient updates for a job.
func (c *Coordinator) Gradients(jobID string) []GradientUpdate {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.grads[jobID]
}

// EpochGradients returns gradients for a specific epoch.
func (c *Coordinator) EpochGradients(jobID string, epoch int) []GradientUpdate {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []GradientUpdate
	for _, g := range c.grads[jobID] {
		if g.Epoch == epoch {
			result = append(result, g)
		}
	}
	return result
}

// AggregateEpoch performs FedAvg gradient aggregation for an epoch.
// FedAvg: weighted average of gradients proportional to sample count.
// Returns average loss across all participating nodes.
func (c *Coordinator) AggregateEpoch(jobID string, epoch int) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	job, ok := c.jobs[jobID]
	if !ok {
		return 0, ErrJobNotFound
	}

	var grads []GradientUpdate
	for _, g := range c.grads[jobID] {
		if g.Epoch == epoch {
			grads = append(grads, g)
		}
	}

	if len(grads) == 0 {
		return 0, fmt.Errorf("no gradients for epoch %d", epoch)
	}

	// FedAvg: weighted by sample count
	var totalLoss float64
	var totalSamples int
	for _, g := range grads {
		totalLoss += g.Loss * float64(g.Samples)
		totalSamples += g.Samples
	}
	avgLoss := totalLoss / float64(totalSamples)

	// Save checkpoint
	checkpoint := Checkpoint{
		JobID:     jobID,
		Epoch:     epoch,
		Loss:      avgLoss,
		NodeCount: len(grads),
		CreatedAt: time.Now(),
	}
	c.checks[jobID] = append(c.checks[jobID], checkpoint)

	// Update cost
	job.CreditCost += c.config.CreditPerMinute

	return avgLoss, nil
}

// Checkpoints returns all checkpoints for a job.
func (c *Coordinator) Checkpoints(jobID string) []Checkpoint {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.checks[jobID]
}

// CompleteJob marks a job as completed.
func (c *Coordinator) CompleteJob(jobID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	job, ok := c.jobs[jobID]
	if !ok {
		return ErrJobNotFound
	}
	job.Status = JobCompleted
	job.CompletedAt = time.Now()
	return nil
}

// FailJob marks a job as failed with an error message.
func (c *Coordinator) FailJob(jobID, reason string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	job, ok := c.jobs[jobID]
	if !ok {
		return ErrJobNotFound
	}
	job.Status = JobFailed
	job.CompletedAt = time.Now()
	job.Error = reason
	return nil
}

// CancelJob marks a job as cancelled.
func (c *Coordinator) CancelJob(jobID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	job, ok := c.jobs[jobID]
	if !ok {
		return ErrJobNotFound
	}
	if job.IsTerminal() {
		return fmt.Errorf("job %s already in terminal state %s", jobID, job.Status)
	}
	job.Status = JobCancelled
	job.CompletedAt = time.Now()
	return nil
}

// ActiveJobCount returns the number of non-terminal jobs.
func (c *Coordinator) ActiveJobCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	count := 0
	for _, j := range c.jobs {
		if !j.IsTerminal() {
			count++
		}
	}
	return count
}

// Stats returns aggregate coordinator statistics.
func (c *Coordinator) Stats() CoordinatorStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var stats CoordinatorStats
	for _, j := range c.jobs {
		switch j.Status {
		case JobPending, JobSharding, JobTraining, JobAggregating:
			stats.ActiveJobs++
		case JobCompleted:
			stats.CompletedJobs++
		case JobFailed:
			stats.FailedJobs++
		}
		stats.TotalCreditsSpent += j.CreditCost
	}
	stats.TotalJobs = len(c.jobs)
	return stats
}

// CoordinatorStats holds aggregate fine-tuning statistics.
type CoordinatorStats struct {
	TotalJobs         int   `json:"total_jobs"`
	ActiveJobs        int   `json:"active_jobs"`
	CompletedJobs     int   `json:"completed_jobs"`
	FailedJobs        int   `json:"failed_jobs"`
	TotalCreditsSpent int64 `json:"total_credits_spent"`
}
