package finetune

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"
)

// ─── Real-World Fine-Tuning Scenario Tests ──────────────────────────────────
// These tests simulate real-world distributed fine-tuning workflows including
// multi-epoch training, partial node failures, loss convergence validation,
// and concurrent job management.

// TestScenario_FullTrainingRun simulates a complete distributed fine-tuning
// run with 3 nodes across 5 epochs, verifying loss convergence via FedAvg.
func TestScenario_FullTrainingRun(t *testing.T) {
	c := NewCoordinator(CoordinatorConfig{
		MaxConcurrentJobs: 5,
		EpochTimeout:      30 * time.Minute,
		CreditPerMinute:   10,
	})

	job := FineTuneJob{
		ID:         "medical-qa-v1",
		BaseModel:  "llama-3.2-7b",
		DatasetURI: "s3://datasets/medical-qa-50k.jsonl",
		Method:     MethodLoRA,
		Config:     DefaultLoRAConfig(),
		Epochs:     5,
		MinNodes:   3,
		MaxNodes:   5,
	}

	if err := c.SubmitJob(job); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Assign 3 data shards
	shards := []DataShard{
		{ShardIndex: 0, NodeID: "gpu-node-us-1", SampleCount: 17000, SizeBytes: 50 * 1024 * 1024},
		{ShardIndex: 1, NodeID: "gpu-node-eu-1", SampleCount: 17000, SizeBytes: 50 * 1024 * 1024},
		{ShardIndex: 2, NodeID: "gpu-node-ap-1", SampleCount: 16000, SizeBytes: 48 * 1024 * 1024},
	}
	if err := c.AssignShards(job.ID, shards); err != nil {
		t.Fatalf("assign shards: %v", err)
	}

	if err := c.StartTraining(job.ID); err != nil {
		t.Fatalf("start training: %v", err)
	}

	// Simulate 5 epochs with decreasing loss (convergence)
	epochLosses := []float64{2.5, 1.8, 1.2, 0.9, 0.7}
	previousLoss := math.Inf(1)

	for epoch := 1; epoch <= 5; epoch++ {
		baseLoss := epochLosses[epoch-1]

		// Each node reports slightly different loss (realistic jitter)
		for i, shard := range shards {
			jitter := float64(i) * 0.05
			c.RecordGradient(GradientUpdate{
				JobID:      job.ID,
				NodeID:     shard.NodeID,
				ShardIndex: shard.ShardIndex,
				Epoch:      epoch,
				Loss:       baseLoss + jitter,
				Samples:    shard.SampleCount,
				Timestamp:  time.Now(),
			})
		}

		avgLoss, err := c.AggregateEpoch(job.ID, epoch)
		if err != nil {
			t.Fatalf("aggregate epoch %d: %v", epoch, err)
		}

		// Loss should be decreasing (convergence)
		if avgLoss >= previousLoss {
			t.Errorf("epoch %d: loss %.4f >= previous %.4f (not converging)", epoch, avgLoss, previousLoss)
		}
		previousLoss = avgLoss

		t.Logf("Epoch %d: avg_loss=%.4f", epoch, avgLoss)
	}

	// Complete the job
	if err := c.CompleteJob(job.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}

	// Verify checkpoints
	checks := c.Checkpoints(job.ID)
	if len(checks) != 5 {
		t.Errorf("expected 5 checkpoints, got %d", len(checks))
	}

	// Verify last checkpoint has lowest loss
	lastCheck := checks[len(checks)-1]
	if lastCheck.Loss >= 1.0 {
		t.Errorf("final checkpoint loss %.4f should be < 1.0", lastCheck.Loss)
	}
	if lastCheck.NodeCount != 3 {
		t.Errorf("final checkpoint nodes = %d, want 3", lastCheck.NodeCount)
	}

	// Verify job stats
	finalJob, _ := c.GetJob(job.ID)
	if finalJob.Status != JobCompleted {
		t.Errorf("final status = %s, want COMPLETED", finalJob.Status)
	}
	if finalJob.CreditCost <= 0 {
		t.Error("credit cost should be > 0")
	}
}

// TestScenario_NodeFailureDuringTraining simulates a node dropping out
// mid-training and the coordinator handling it gracefully.
func TestScenario_NodeFailureDuringTraining(t *testing.T) {
	c := NewCoordinator(DefaultCoordinatorConfig())

	job := FineTuneJob{
		ID:        "fail-recovery",
		BaseModel: "llama-3.2-1b",
		MinNodes:  2,
		Epochs:    3,
	}
	c.SubmitJob(job)
	c.AssignShards(job.ID, []DataShard{
		{ShardIndex: 0, NodeID: "healthy-node", SampleCount: 500},
		{ShardIndex: 1, NodeID: "failing-node", SampleCount: 500},
	})
	c.StartTraining(job.ID)

	// Epoch 1: both nodes report
	c.RecordGradient(GradientUpdate{
		JobID: job.ID, NodeID: "healthy-node", Epoch: 1, Loss: 2.0, Samples: 500,
	})
	c.RecordGradient(GradientUpdate{
		JobID: job.ID, NodeID: "failing-node", Epoch: 1, Loss: 2.2, Samples: 500,
	})
	loss1, _ := c.AggregateEpoch(job.ID, 1)

	// Epoch 2: failing-node drops out — only healthy-node reports
	c.RecordGradient(GradientUpdate{
		JobID: job.ID, NodeID: "healthy-node", Epoch: 2, Loss: 1.5, Samples: 500,
	})
	loss2, _ := c.AggregateEpoch(job.ID, 2)

	// Training still progresses (degraded but functional)
	if loss2 >= loss1 {
		t.Logf("Note: loss increased after node failure (epoch1=%.2f, epoch2=%.2f) — expected in degraded mode", loss1, loss2)
	}

	// Epoch 2 checkpoint should show only 1 node
	checks := c.Checkpoints(job.ID)
	epoch2Check := checks[len(checks)-1]
	if epoch2Check.NodeCount != 1 {
		t.Errorf("epoch 2 checkpoint nodes = %d, want 1 (after failure)", epoch2Check.NodeCount)
	}
}

// TestScenario_QLoRAFourBit tests QLoRA (4-bit quantized) fine-tuning flow.
func TestScenario_QLoRAFourBit(t *testing.T) {
	c := NewCoordinator(DefaultCoordinatorConfig())

	job := FineTuneJob{
		ID:         "qlora-sentiment",
		BaseModel:  "llama-3.2-7b",
		DatasetURI: "gs://training/sentiment-1m.jsonl",
		Method:     MethodQLoRA,
		Config: LoRAConfig{
			Rank:          8,
			Alpha:         16,
			Dropout:       0.1,
			TargetModules: []string{"q_proj", "k_proj", "v_proj", "o_proj"},
			LearningRate:  1e-4,
			BatchSize:     2, // Smaller batch for 4-bit
			GradAccumSteps: 8,
		},
		Epochs:   3,
		MinNodes: 2,
	}

	if err := c.SubmitJob(job); err != nil {
		t.Fatalf("submit QLoRA job: %v", err)
	}

	got, _ := c.GetJob(job.ID)
	if got.Method != MethodQLoRA {
		t.Errorf("method = %s, want qlora", got.Method)
	}
	if got.Config.Rank != 8 {
		t.Errorf("rank = %d, want 8", got.Config.Rank)
	}
}

// TestScenario_ConcurrentJobs tests multiple fine-tuning jobs running
// simultaneously without interference.
func TestScenario_ConcurrentJobs(t *testing.T) {
	c := NewCoordinator(CoordinatorConfig{
		MaxConcurrentJobs: 10,
		EpochTimeout:      30 * time.Minute,
		CreditPerMinute:   10,
	})

	const numJobs = 5
	var wg sync.WaitGroup
	errs := make(chan error, numJobs*10)

	for i := 0; i < numJobs; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			jobID := fmt.Sprintf("parallel-job-%d", idx)

			// Submit
			if err := c.SubmitJob(FineTuneJob{
				ID:        jobID,
				BaseModel: fmt.Sprintf("model-%d", idx),
				MinNodes:  1,
				Epochs:    2,
			}); err != nil {
				errs <- fmt.Errorf("submit %s: %w", jobID, err)
				return
			}

			// Assign shard
			if err := c.AssignShards(jobID, []DataShard{
				{ShardIndex: 0, NodeID: fmt.Sprintf("node-%d", idx), SampleCount: 100},
			}); err != nil {
				errs <- fmt.Errorf("assign %s: %w", jobID, err)
				return
			}

			// Start training
			if err := c.StartTraining(jobID); err != nil {
				errs <- fmt.Errorf("start %s: %w", jobID, err)
				return
			}

			// Record gradients for 2 epochs
			for epoch := 1; epoch <= 2; epoch++ {
				if err := c.RecordGradient(GradientUpdate{
					JobID: jobID, NodeID: fmt.Sprintf("node-%d", idx),
					Epoch: epoch, Loss: float64(3-epoch) * 0.5, Samples: 100,
				}); err != nil {
					errs <- fmt.Errorf("gradient %s epoch %d: %w", jobID, epoch, err)
					return
				}
				if _, err := c.AggregateEpoch(jobID, epoch); err != nil {
					errs <- fmt.Errorf("aggregate %s epoch %d: %w", jobID, epoch, err)
					return
				}
			}

			// Complete
			if err := c.CompleteJob(jobID); err != nil {
				errs <- fmt.Errorf("complete %s: %w", jobID, err)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}

	// All jobs should be completed
	stats := c.Stats()
	if stats.CompletedJobs != numJobs {
		t.Errorf("completed = %d, want %d", stats.CompletedJobs, numJobs)
	}
	if stats.ActiveJobs != 0 {
		t.Errorf("active = %d, want 0", stats.ActiveJobs)
	}
}

// TestScenario_JobCapacityThrottling tests that the coordinator enforces
// max concurrent job limits under load.
func TestScenario_JobCapacityThrottling(t *testing.T) {
	c := NewCoordinator(CoordinatorConfig{
		MaxConcurrentJobs: 2,
		EpochTimeout:      5 * time.Minute,
		CreditPerMinute:   10,
	})

	// Fill capacity
	c.SubmitJob(FineTuneJob{ID: "cap-1"})
	c.SubmitJob(FineTuneJob{ID: "cap-2"})

	// Third should be rejected
	err := c.SubmitJob(FineTuneJob{ID: "cap-3"})
	if err == nil {
		t.Error("expected capacity error")
	}

	// Complete one — should free a slot
	c.CompleteJob("cap-1")

	// Now cap-3 should succeed
	err = c.SubmitJob(FineTuneJob{ID: "cap-3"})
	if err != nil {
		t.Errorf("after freeing slot, submit should work: %v", err)
	}
}

// TestScenario_FedAvgUnbalancedShards tests FedAvg with very unbalanced
// data distribution (one node has 10x more data than another).
func TestScenario_FedAvgUnbalancedShards(t *testing.T) {
	c := NewCoordinator(DefaultCoordinatorConfig())

	c.SubmitJob(FineTuneJob{ID: "unbalanced", MinNodes: 1})

	// Node A: 100 samples with loss 1.0
	// Node B: 1000 samples with loss 3.0
	// FedAvg should heavily weight Node B: (1.0*100 + 3.0*1000) / 1100 ≈ 2.818
	c.RecordGradient(GradientUpdate{
		JobID: "unbalanced", NodeID: "A", Epoch: 1, Loss: 1.0, Samples: 100,
	})
	c.RecordGradient(GradientUpdate{
		JobID: "unbalanced", NodeID: "B", Epoch: 1, Loss: 3.0, Samples: 1000,
	})

	avgLoss, err := c.AggregateEpoch("unbalanced", 1)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}

	expected := (1.0*100 + 3.0*1000) / 1100.0 // ≈ 2.818
	if math.Abs(avgLoss-expected) > 0.01 {
		t.Errorf("FedAvg unbalanced: got %.4f, want %.4f", avgLoss, expected)
	}
}

// TestScenario_CreditTracking verifies that credit costs accumulate
// correctly across multiple epochs and jobs.
func TestScenario_CreditTracking(t *testing.T) {
	c := NewCoordinator(CoordinatorConfig{
		MaxConcurrentJobs: 5,
		EpochTimeout:      30 * time.Minute,
		CreditPerMinute:   15,
	})

	c.SubmitJob(FineTuneJob{ID: "credit-test", MinNodes: 1, Epochs: 3})

	// 3 epochs = 3 aggregations = 3 * 15 = 45 credits
	for epoch := 1; epoch <= 3; epoch++ {
		c.RecordGradient(GradientUpdate{
			JobID: "credit-test", NodeID: "node-1",
			Epoch: epoch, Loss: float64(3-epoch), Samples: 100,
		})
		c.AggregateEpoch("credit-test", epoch)
	}
	c.CompleteJob("credit-test")

	job, _ := c.GetJob("credit-test")
	if job.CreditCost != 45 {
		t.Errorf("credit cost = %d, want 45 (3 epochs * 15 cr/min)", job.CreditCost)
	}

	stats := c.Stats()
	if stats.TotalCreditsSpent != 45 {
		t.Errorf("total credits = %d, want 45", stats.TotalCreditsSpent)
	}
}

// TestScenario_JobLifecycleStates verifies that a job passes through
// all expected states in the correct order.
func TestScenario_JobLifecycleStates(t *testing.T) {
	c := NewCoordinator(DefaultCoordinatorConfig())

	c.SubmitJob(FineTuneJob{ID: "lifecycle", MinNodes: 1})

	// State 1: PENDING
	j, _ := c.GetJob("lifecycle")
	if j.Status != JobPending {
		t.Errorf("state 1: %s, want PENDING", j.Status)
	}

	// State 2: SHARDING
	c.AssignShards("lifecycle", []DataShard{{NodeID: "n1", SampleCount: 100}})
	j, _ = c.GetJob("lifecycle")
	if j.Status != JobSharding {
		t.Errorf("state 2: %s, want SHARDING", j.Status)
	}

	// State 3: TRAINING
	c.StartTraining("lifecycle")
	j, _ = c.GetJob("lifecycle")
	if j.Status != JobTraining {
		t.Errorf("state 3: %s, want TRAINING", j.Status)
	}

	// State 4: COMPLETED
	c.CompleteJob("lifecycle")
	j, _ = c.GetJob("lifecycle")
	if j.Status != JobCompleted {
		t.Errorf("state 4: %s, want COMPLETED", j.Status)
	}
	if j.IsTerminal() != true {
		t.Error("completed job should be terminal")
	}
}
