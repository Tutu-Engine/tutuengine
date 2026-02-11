package finetune

import (
	"fmt"
	"testing"
	"time"
)

// ─── FineTuneJob Tests ──────────────────────────────────────────────────────

func TestFineTuneJob_Duration(t *testing.T) {
	now := time.Now()
	job := &FineTuneJob{
		StartedAt:   now.Add(-10 * time.Minute),
		CompletedAt: now,
	}
	d := job.Duration()
	if d < 9*time.Minute || d > 11*time.Minute {
		t.Errorf("Duration() = %v, want ~10m", d)
	}
}

func TestFineTuneJob_DurationNotStarted(t *testing.T) {
	job := &FineTuneJob{}
	if job.Duration() != 0 {
		t.Errorf("Duration() of unstarted job = %v, want 0", job.Duration())
	}
}

func TestFineTuneJob_IsTerminal(t *testing.T) {
	tests := []struct {
		status   JobStatus
		terminal bool
	}{
		{JobPending, false},
		{JobSharding, false},
		{JobTraining, false},
		{JobAggregating, false},
		{JobCompleted, true},
		{JobFailed, true},
		{JobCancelled, true},
	}
	for _, tt := range tests {
		job := &FineTuneJob{Status: tt.status}
		if got := job.IsTerminal(); got != tt.terminal {
			t.Errorf("IsTerminal(%s) = %v, want %v", tt.status, got, tt.terminal)
		}
	}
}

func TestDefaultLoRAConfig(t *testing.T) {
	cfg := DefaultLoRAConfig()
	if cfg.Rank != 16 {
		t.Errorf("Rank = %d, want 16", cfg.Rank)
	}
	if cfg.Alpha != 32 {
		t.Errorf("Alpha = %f, want 32", cfg.Alpha)
	}
	if cfg.LearningRate != 2e-4 {
		t.Errorf("LR = %f, want 2e-4", cfg.LearningRate)
	}
	if len(cfg.TargetModules) != 2 {
		t.Errorf("TargetModules len = %d, want 2", len(cfg.TargetModules))
	}
}

// ─── Coordinator Tests ──────────────────────────────────────────────────────

func newTestCoordinator() *Coordinator {
	return NewCoordinator(CoordinatorConfig{
		MaxConcurrentJobs: 2,
		EpochTimeout:      5 * time.Minute,
		CreditPerMinute:   10,
	})
}

func TestCoordinator_SubmitJob(t *testing.T) {
	c := newTestCoordinator()

	job := FineTuneJob{
		ID:        "job-1",
		BaseModel: "llama3.2",
	}
	if err := c.SubmitJob(job); err != nil {
		t.Fatalf("SubmitJob: %v", err)
	}

	got, err := c.GetJob("job-1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != JobPending {
		t.Errorf("status = %s, want PENDING", got.Status)
	}
	if got.Method != MethodLoRA {
		t.Errorf("method = %s, want lora (default)", got.Method)
	}
	if got.Epochs != 3 {
		t.Errorf("epochs = %d, want 3 (default)", got.Epochs)
	}
}

func TestCoordinator_DuplicateJob(t *testing.T) {
	c := newTestCoordinator()
	c.SubmitJob(FineTuneJob{ID: "dup"})

	err := c.SubmitJob(FineTuneJob{ID: "dup"})
	if err != ErrJobAlreadyRunning {
		t.Errorf("duplicate submit err = %v, want ErrJobAlreadyRunning", err)
	}
}

func TestCoordinator_MaxConcurrent(t *testing.T) {
	c := newTestCoordinator() // max 2

	c.SubmitJob(FineTuneJob{ID: "a"})
	c.SubmitJob(FineTuneJob{ID: "b"})

	err := c.SubmitJob(FineTuneJob{ID: "c"})
	if err == nil {
		t.Error("expected max concurrent error, got nil")
	}
}

func TestCoordinator_JobNotFound(t *testing.T) {
	c := newTestCoordinator()

	_, err := c.GetJob("nope")
	if err != ErrJobNotFound {
		t.Errorf("GetJob(nope) err = %v, want ErrJobNotFound", err)
	}
}

func TestCoordinator_AssignShards(t *testing.T) {
	c := newTestCoordinator()
	c.SubmitJob(FineTuneJob{ID: "j1", MinNodes: 2})

	shards := []DataShard{
		{ShardIndex: 0, NodeID: "node-a", SampleCount: 500},
		{ShardIndex: 1, NodeID: "node-b", SampleCount: 500},
	}
	if err := c.AssignShards("j1", shards); err != nil {
		t.Fatalf("AssignShards: %v", err)
	}

	got := c.Shards("j1")
	if len(got) != 2 {
		t.Errorf("shards = %d, want 2", len(got))
	}
}

func TestCoordinator_AssignShards_Insufficient(t *testing.T) {
	c := newTestCoordinator()
	c.SubmitJob(FineTuneJob{ID: "j2", MinNodes: 3})

	err := c.AssignShards("j2", []DataShard{{ShardIndex: 0, NodeID: "n1"}})
	if err != ErrInsufficientNodes {
		t.Errorf("err = %v, want ErrInsufficientNodes", err)
	}
}

func TestCoordinator_TrainingLifecycle(t *testing.T) {
	c := newTestCoordinator()
	c.SubmitJob(FineTuneJob{ID: "life", MinNodes: 1})
	c.AssignShards("life", []DataShard{{ShardIndex: 0, NodeID: "n1", SampleCount: 100}})

	// Start training
	if err := c.StartTraining("life"); err != nil {
		t.Fatalf("StartTraining: %v", err)
	}
	job, _ := c.GetJob("life")
	if job.Status != JobTraining {
		t.Errorf("status = %s, want TRAINING", job.Status)
	}

	// Record gradient
	c.RecordGradient(GradientUpdate{
		JobID: "life", NodeID: "n1", ShardIndex: 0,
		Epoch: 1, Loss: 2.5, Samples: 100,
	})

	// Aggregate
	avgLoss, err := c.AggregateEpoch("life", 1)
	if err != nil {
		t.Fatalf("AggregateEpoch: %v", err)
	}
	if avgLoss != 2.5 {
		t.Errorf("avgLoss = %f, want 2.5", avgLoss)
	}

	// Checkpoint should exist
	checks := c.Checkpoints("life")
	if len(checks) != 1 {
		t.Fatalf("checkpoints = %d, want 1", len(checks))
	}
	if checks[0].Epoch != 1 {
		t.Errorf("checkpoint epoch = %d, want 1", checks[0].Epoch)
	}

	// Complete
	if err := c.CompleteJob("life"); err != nil {
		t.Fatalf("CompleteJob: %v", err)
	}
	job, _ = c.GetJob("life")
	if job.Status != JobCompleted {
		t.Errorf("final status = %s, want COMPLETED", job.Status)
	}
}

func TestCoordinator_FedAvgMultiNode(t *testing.T) {
	c := newTestCoordinator()
	c.SubmitJob(FineTuneJob{ID: "fedavg", MinNodes: 1})

	// Two nodes with different sample counts → weighted average
	c.RecordGradient(GradientUpdate{
		JobID: "fedavg", NodeID: "A", Epoch: 1,
		Loss: 2.0, Samples: 100,
	})
	c.RecordGradient(GradientUpdate{
		JobID: "fedavg", NodeID: "B", Epoch: 1,
		Loss: 4.0, Samples: 300,
	})

	avgLoss, _ := c.AggregateEpoch("fedavg", 1)
	// Weighted: (2.0*100 + 4.0*300) / 400 = (200 + 1200) / 400 = 3.5
	expected := 3.5
	if diff := avgLoss - expected; diff > 0.01 || diff < -0.01 {
		t.Errorf("FedAvg loss = %f, want %f", avgLoss, expected)
	}
}

func TestCoordinator_EpochGradients(t *testing.T) {
	c := newTestCoordinator()
	c.SubmitJob(FineTuneJob{ID: "eg"})

	c.RecordGradient(GradientUpdate{JobID: "eg", Epoch: 1, Samples: 10, Loss: 1.0})
	c.RecordGradient(GradientUpdate{JobID: "eg", Epoch: 2, Samples: 10, Loss: 0.5})
	c.RecordGradient(GradientUpdate{JobID: "eg", Epoch: 1, Samples: 10, Loss: 0.8})

	epoch1 := c.EpochGradients("eg", 1)
	if len(epoch1) != 2 {
		t.Errorf("epoch 1 gradients = %d, want 2", len(epoch1))
	}

	epoch2 := c.EpochGradients("eg", 2)
	if len(epoch2) != 1 {
		t.Errorf("epoch 2 gradients = %d, want 1", len(epoch2))
	}
}

func TestCoordinator_FailAndCancel(t *testing.T) {
	c := newTestCoordinator()
	c.SubmitJob(FineTuneJob{ID: "fail"})
	c.SubmitJob(FineTuneJob{ID: "cancel"})

	if err := c.FailJob("fail", "OOM on node-3"); err != nil {
		t.Fatalf("FailJob: %v", err)
	}
	j, _ := c.GetJob("fail")
	if j.Status != JobFailed || j.Error != "OOM on node-3" {
		t.Errorf("status=%s error=%q", j.Status, j.Error)
	}

	if err := c.CancelJob("cancel"); err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	j, _ = c.GetJob("cancel")
	if j.Status != JobCancelled {
		t.Errorf("status = %s, want CANCELLED", j.Status)
	}

	// Can't cancel already-terminal job
	if err := c.CancelJob("fail"); err == nil {
		t.Error("CancelJob on failed job should error")
	}
}

func TestCoordinator_ListJobs(t *testing.T) {
	c := newTestCoordinator()
	c.SubmitJob(FineTuneJob{ID: "x"})
	c.SubmitJob(FineTuneJob{ID: "y"})

	jobs := c.ListJobs()
	if len(jobs) != 2 {
		t.Errorf("ListJobs = %d, want 2", len(jobs))
	}
}

func TestCoordinator_ActiveJobCount(t *testing.T) {
	c := newTestCoordinator()
	c.SubmitJob(FineTuneJob{ID: "a1"})
	c.SubmitJob(FineTuneJob{ID: "a2"})

	if c.ActiveJobCount() != 2 {
		t.Errorf("active = %d, want 2", c.ActiveJobCount())
	}

	c.CompleteJob("a1")
	if c.ActiveJobCount() != 1 {
		t.Errorf("after complete: active = %d, want 1", c.ActiveJobCount())
	}
}

func TestCoordinator_Stats(t *testing.T) {
	c := newTestCoordinator()
	c.SubmitJob(FineTuneJob{ID: "s1"})
	c.SubmitJob(FineTuneJob{ID: "s2"})
	c.CompleteJob("s1")
	c.FailJob("s2", "error")

	stats := c.Stats()
	if stats.TotalJobs != 2 {
		t.Errorf("total = %d, want 2", stats.TotalJobs)
	}
	if stats.CompletedJobs != 1 {
		t.Errorf("completed = %d, want 1", stats.CompletedJobs)
	}
	if stats.FailedJobs != 1 {
		t.Errorf("failed = %d, want 1", stats.FailedJobs)
	}
}

func TestCoordinator_ConcurrentSubmit(t *testing.T) {
	c := NewCoordinator(CoordinatorConfig{
		MaxConcurrentJobs: 100,
		EpochTimeout:      5 * time.Minute,
		CreditPerMinute:   10,
	})

	done := make(chan error, 50)
	for i := 0; i < 50; i++ {
		go func(id int) {
			done <- c.SubmitJob(FineTuneJob{ID: fmt.Sprintf("c-%d", id)})
		}(i)
	}

	errs := 0
	for i := 0; i < 50; i++ {
		if err := <-done; err != nil {
			errs++
		}
	}
	if errs > 0 {
		t.Errorf("%d submit errors in concurrent test", errs)
	}

	if len(c.ListJobs()) != 50 {
		t.Errorf("jobs = %d, want 50", len(c.ListJobs()))
	}
}
