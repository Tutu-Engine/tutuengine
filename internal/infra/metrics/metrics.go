// Package metrics provides Prometheus metrics for TuTu.
// Architecture Part XVIII: Observability foundation — counters, gauges, histograms
// for inference, tasks, credits, resources, peers, and health.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ─── Inference ──────────────────────────────────────────────────────────────

// InferenceLatency tracks inference request duration in seconds.
var InferenceLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: "tutu",
	Name:      "inference_latency_seconds",
	Help:      "Inference request duration in seconds.",
	Buckets:   prometheus.DefBuckets,
}, []string{"model"})

// InferenceTokens tracks tokens generated per request.
var InferenceTokens = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "tutu",
	Name:      "inference_tokens_total",
	Help:      "Total tokens generated.",
}, []string{"model"})

// ─── Tasks ──────────────────────────────────────────────────────────────────

// TasksCompleted tracks completed tasks by type.
var TasksCompleted = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "tutu",
	Name:      "tasks_completed_total",
	Help:      "Total completed tasks.",
}, []string{"type"})

// TasksFailed tracks failed tasks by type and reason.
var TasksFailed = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "tutu",
	Name:      "tasks_failed_total",
	Help:      "Total failed tasks.",
}, []string{"type", "reason"})

// TasksActive tracks currently executing tasks.
var TasksActive = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: "tutu",
	Name:      "tasks_active",
	Help:      "Number of currently executing tasks.",
})

// TaskAssignLatency tracks time from task queued to assigned.
var TaskAssignLatency = promauto.NewHistogram(prometheus.HistogramOpts{
	Namespace: "tutu",
	Name:      "task_assign_latency_seconds",
	Help:      "Time from task queued to execution start.",
	Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
})

// ─── Credits ────────────────────────────────────────────────────────────────

// CreditsEarned tracks total credits earned.
var CreditsEarned = promauto.NewCounter(prometheus.CounterOpts{
	Namespace: "tutu",
	Name:      "credits_earned_total",
	Help:      "Total credits earned by this node.",
})

// CreditsBalance tracks current credit balance.
var CreditsBalance = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: "tutu",
	Name:      "credits_balance_current",
	Help:      "Current credit balance.",
})

// ─── Resources ──────────────────────────────────────────────────────────────

// CPUUsage tracks CPU usage percentage.
var CPUUsage = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: "tutu",
	Name:      "cpu_usage_percent",
	Help:      "Current CPU usage percentage.",
})

// GPUUsage tracks GPU usage percentage.
var GPUUsage = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: "tutu",
	Name:      "gpu_usage_percent",
	Help:      "Current GPU usage percentage.",
})

// GPUTemperature tracks GPU temperature in celsius.
var GPUTemperature = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: "tutu",
	Name:      "gpu_temperature_celsius",
	Help:      "Current GPU temperature in Celsius.",
})

// MemoryUsage tracks memory usage in bytes.
var MemoryUsage = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: "tutu",
	Name:      "memory_usage_bytes",
	Help:      "Current memory usage in bytes.",
})

// IdleLevel tracks the current idle level (0=Active, 1=Light, 2=Deep, 3=Locked, 4=Server).
var IdleLevel = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: "tutu",
	Name:      "idle_level",
	Help:      "Current idle level (0=Active, 1=Light, 2=Deep, 3=Locked, 4=Server).",
})

// ─── Peers ──────────────────────────────────────────────────────────────────

// PeersKnown tracks total known peers.
var PeersKnown = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: "tutu",
	Name:      "peers_known_total",
	Help:      "Number of known peers in the gossip mesh.",
})

// PeersAlive tracks alive peers.
var PeersAlive = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: "tutu",
	Name:      "peers_alive_total",
	Help:      "Number of alive peers.",
})

// ─── Heartbeat ──────────────────────────────────────────────────────────────

// HeartbeatLatency tracks heartbeat round-trip latency to Cloud Core.
var HeartbeatLatency = promauto.NewHistogram(prometheus.HistogramOpts{
	Namespace: "tutu",
	Name:      "heartbeat_latency_seconds",
	Help:      "Heartbeat round-trip latency to Cloud Core.",
	Buckets:   []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
})

// ─── Health ─────────────────────────────────────────────────────────────────

// HealthCheckStatus tracks health check results (1=healthy, 0=unhealthy).
var HealthCheckStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "tutu",
	Name:      "health_check_status",
	Help:      "Health check result per component (1=healthy, 0=unhealthy).",
}, []string{"check"})

// HealthRecoveries tracks auto-recovery attempts.
var HealthRecoveries = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "tutu",
	Name:      "health_recoveries_total",
	Help:      "Total auto-recovery attempts per check.",
}, []string{"check"})

// ─── Gossip ─────────────────────────────────────────────────────────────────

// GossipMessages tracks SWIM protocol messages.
var GossipMessages = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "tutu",
	Name:      "gossip_messages_total",
	Help:      "Total SWIM gossip messages by type.",
}, []string{"type"})

// GossipConvergenceTime tracks time for new member to be discovered.
var GossipConvergenceTime = promauto.NewHistogram(prometheus.HistogramOpts{
	Namespace: "tutu",
	Name:      "gossip_convergence_seconds",
	Help:      "Time for gossip membership convergence.",
	Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30},
})
