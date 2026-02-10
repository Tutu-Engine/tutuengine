package mcp

import (
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ─── SLA Engine ─────────────────────────────────────────────────────────────
// Architecture Part XII: 4 SLA tiers with pricing & performance guarantees.
// Each tier maps to a TaskPriority, latency budget, and price.

// SLAEngine resolves client SLA tiers into concrete performance parameters.
type SLAEngine struct {
	tiers map[domain.SLATier]domain.SLAConfig
}

// NewSLAEngine creates the engine with the 4 architecture-defined tiers.
func NewSLAEngine() *SLAEngine {
	return &SLAEngine{
		tiers: map[domain.SLATier]domain.SLAConfig{
			domain.SLARealtime: {
				Tier:            domain.SLARealtime,
				MaxLatencyP99:   200 * time.Millisecond,
				TargetTokensSec: 200,
				AvailabilityPct: 99.9,
				PricePerMTokens: 2.00,
				Priority:        255,
				MaxConcurrent:   100,
				RateLimitRPM:    600,
			},
			domain.SLAStandard: {
				Tier:            domain.SLAStandard,
				MaxLatencyP99:   2 * time.Second,
				TargetTokensSec: 100,
				AvailabilityPct: 99.5,
				PricePerMTokens: 0.50,
				Priority:        128,
				MaxConcurrent:   50,
				RateLimitRPM:    300,
			},
			domain.SLABatch: {
				Tier:            domain.SLABatch,
				MaxLatencyP99:   30 * time.Second,
				TargetTokensSec: 50,
				AvailabilityPct: 99.0,
				PricePerMTokens: 0.10,
				Priority:        64,
				MaxConcurrent:   20,
				RateLimitRPM:    60,
			},
			domain.SLASpot: {
				Tier:            domain.SLASpot,
				MaxLatencyP99:   0, // best-effort
				TargetTokensSec: 0, // best-effort
				AvailabilityPct: 0, // no SLA
				PricePerMTokens: 0.02,
				Priority:        1,
				MaxConcurrent:   10,
				RateLimitRPM:    30,
			},
		},
	}
}

// ConfigFor returns the SLA configuration for the given tier.
// Returns the spot tier config as fallback for unknown tiers.
func (e *SLAEngine) ConfigFor(tier domain.SLATier) domain.SLAConfig {
	if cfg, ok := e.tiers[tier]; ok {
		return cfg
	}
	return e.tiers[domain.SLASpot]
}

// PriorityFor returns the task queue priority for the given tier.
func (e *SLAEngine) PriorityFor(tier domain.SLATier) int {
	return e.ConfigFor(tier).Priority
}

// CostMicro calculates the cost in microdollars for a given token count and tier.
// 1 microdollar = $0.000001
func (e *SLAEngine) CostMicro(tier domain.SLATier, inputToks, outputToks int) int64 {
	cfg := e.ConfigFor(tier)
	totalToks := int64(inputToks + outputToks)
	// price_per_m_tokens * total_tokens / 1_000_000 → dollars
	// Convert to microdollars (* 1_000_000)
	// Simplifies to: price_per_m_tokens * total_tokens
	return int64(cfg.PricePerMTokens * float64(totalToks))
}

// AllTiers returns all SLA configurations in priority order (highest first).
func (e *SLAEngine) AllTiers() []domain.SLAConfig {
	return []domain.SLAConfig{
		e.tiers[domain.SLARealtime],
		e.tiers[domain.SLAStandard],
		e.tiers[domain.SLABatch],
		e.tiers[domain.SLASpot],
	}
}
