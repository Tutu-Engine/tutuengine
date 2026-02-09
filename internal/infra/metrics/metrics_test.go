package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestInferenceLatency_Registered(t *testing.T) {
	// Verify the metric is registered with the default registry
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	// promauto registers with the default registry automatically.
	// Just verify we can observe without panicking.
	InferenceLatency.WithLabelValues("llama3.2").Observe(1.5)

	// Verify the histogram records
	families, _ = prometheus.DefaultGatherer.Gather()
	found := false
	for _, f := range families {
		if f.GetName() == "tutu_inference_latency_seconds" {
			found = true
		}
	}
	if !found {
		t.Error("tutu_inference_latency_seconds not found in gathered metrics")
	}
}

func TestTaskCounters(t *testing.T) {
	TasksCompleted.WithLabelValues("INFERENCE").Inc()
	TasksFailed.WithLabelValues("INFERENCE", "timeout").Inc()
	TasksActive.Set(3)

	families, _ := prometheus.DefaultGatherer.Gather()
	names := make(map[string]bool)
	for _, f := range families {
		names[f.GetName()] = true
	}

	expected := []string{
		"tutu_tasks_completed_total",
		"tutu_tasks_failed_total",
		"tutu_tasks_active",
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("metric %q not found", name)
		}
	}
}

func TestCreditMetrics(t *testing.T) {
	CreditsEarned.Add(42)
	CreditsBalance.Set(100.5)

	families, _ := prometheus.DefaultGatherer.Gather()
	names := make(map[string]bool)
	for _, f := range families {
		names[f.GetName()] = true
	}

	if !names["tutu_credits_earned_total"] {
		t.Error("tutu_credits_earned_total not found")
	}
	if !names["tutu_credits_balance_current"] {
		t.Error("tutu_credits_balance_current not found")
	}
}

func TestResourceMetrics(t *testing.T) {
	CPUUsage.Set(45.2)
	GPUUsage.Set(80.0)
	GPUTemperature.Set(72.0)
	MemoryUsage.Set(4 * 1024 * 1024 * 1024) // 4GB
	IdleLevel.Set(2)                          // Deep

	families, _ := prometheus.DefaultGatherer.Gather()
	names := make(map[string]bool)
	for _, f := range families {
		names[f.GetName()] = true
	}

	expected := []string{
		"tutu_cpu_usage_percent",
		"tutu_gpu_usage_percent",
		"tutu_gpu_temperature_celsius",
		"tutu_memory_usage_bytes",
		"tutu_idle_level",
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("metric %q not found", name)
		}
	}
}

func TestPeerMetrics(t *testing.T) {
	PeersKnown.Set(12)
	PeersAlive.Set(10)

	families, _ := prometheus.DefaultGatherer.Gather()
	names := make(map[string]bool)
	for _, f := range families {
		names[f.GetName()] = true
	}

	if !names["tutu_peers_known_total"] {
		t.Error("tutu_peers_known_total not found")
	}
	if !names["tutu_peers_alive_total"] {
		t.Error("tutu_peers_alive_total not found")
	}
}

func TestHealthMetrics(t *testing.T) {
	HealthCheckStatus.WithLabelValues("sqlite").Set(1)
	HealthCheckStatus.WithLabelValues("disk_space").Set(1)
	HealthCheckStatus.WithLabelValues("model_integrity").Set(0)
	HealthRecoveries.WithLabelValues("sqlite").Inc()

	families, _ := prometheus.DefaultGatherer.Gather()
	names := make(map[string]bool)
	for _, f := range families {
		names[f.GetName()] = true
	}

	if !names["tutu_health_check_status"] {
		t.Error("tutu_health_check_status not found")
	}
	if !names["tutu_health_recoveries_total"] {
		t.Error("tutu_health_recoveries_total not found")
	}
}

func TestGossipMetrics(t *testing.T) {
	GossipMessages.WithLabelValues("PING").Add(100)
	GossipMessages.WithLabelValues("ACK").Add(95)
	GossipMessages.WithLabelValues("PING-REQ").Add(5)

	families, _ := prometheus.DefaultGatherer.Gather()
	names := make(map[string]bool)
	for _, f := range families {
		names[f.GetName()] = true
	}

	if !names["tutu_gossip_messages_total"] {
		t.Error("tutu_gossip_messages_total not found")
	}
}

func TestAllMetricsGatherable(t *testing.T) {
	// Ensure all metrics can be gathered without errors
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	tutuMetrics := 0
	for _, f := range families {
		if len(f.GetName()) > 4 && f.GetName()[:5] == "tutu_" {
			tutuMetrics++
		}
	}

	// We should have at least 12 tutu_ metric families
	if tutuMetrics < 12 {
		t.Errorf("expected at least 12 tutu_ metrics, got %d", tutuMetrics)
	}
}

func TestHeartbeatLatency(t *testing.T) {
	HeartbeatLatency.Observe(0.05)
	HeartbeatLatency.Observe(0.12)

	families, _ := prometheus.DefaultGatherer.Gather()
	found := false
	for _, f := range families {
		if f.GetName() == "tutu_heartbeat_latency_seconds" {
			found = true
		}
	}
	if !found {
		t.Error("tutu_heartbeat_latency_seconds not found")
	}
}
