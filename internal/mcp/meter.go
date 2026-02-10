package mcp

import (
	"sync"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ─── Usage Meter ────────────────────────────────────────────────────────────
// Architecture Part XII: Per-client usage metering for billing.
// Records every API call with token counts, latency, and cost.
// Thread-safe — concurrent tool calls from multiple clients.

// Meter tracks per-client usage for billing and analytics.
type Meter struct {
	mu      sync.Mutex
	sla     *SLAEngine
	records []domain.UsageRecord
	// byClient indexes total tokens per client for fast summary.
	byClient map[string]*clientAccum
}

// clientAccum accumulates per-client token and cost totals.
type clientAccum struct {
	TotalCalls  int64
	TotalInput  int64
	TotalOutput int64
	TotalCost   int64 // microdollars
}

// NewMeter creates a usage meter with the given SLA engine for pricing.
func NewMeter(sla *SLAEngine) *Meter {
	return &Meter{
		sla:      sla,
		records:  make([]domain.UsageRecord, 0, 256),
		byClient: make(map[string]*clientAccum),
	}
}

// Record logs a usage event. Cost is calculated from the SLA tier pricing.
func (m *Meter) Record(clientID, tool, model string, inputToks, outputToks int, latencyMs int64, tier domain.SLATier) domain.UsageRecord {
	cost := m.sla.CostMicro(tier, inputToks, outputToks)

	rec := domain.UsageRecord{
		ClientID:   clientID,
		Tool:       tool,
		Model:      model,
		InputToks:  inputToks,
		OutputToks: outputToks,
		LatencyMs:  latencyMs,
		Tier:       tier,
		CostMicro:  cost,
		Timestamp:  time.Now(),
	}

	m.mu.Lock()
	m.records = append(m.records, rec)

	acc, ok := m.byClient[clientID]
	if !ok {
		acc = &clientAccum{}
		m.byClient[clientID] = acc
	}
	acc.TotalCalls++
	acc.TotalInput += int64(inputToks)
	acc.TotalOutput += int64(outputToks)
	acc.TotalCost += cost
	m.mu.Unlock()

	return rec
}

// ClientSummary returns aggregated usage for a single client.
func (m *Meter) ClientSummary(clientID string) domain.ClientUsageSummary {
	m.mu.Lock()
	defer m.mu.Unlock()

	acc, ok := m.byClient[clientID]
	if !ok {
		return domain.ClientUsageSummary{ClientID: clientID}
	}

	return domain.ClientUsageSummary{
		ClientID:    clientID,
		TotalCalls:  acc.TotalCalls,
		TotalInput:  acc.TotalInput,
		TotalOutput: acc.TotalOutput,
		TotalCost:   float64(acc.TotalCost) / 1_000_000, // microdollars → dollars
	}
}

// TotalRecords returns the total number of usage records.
func (m *Meter) TotalRecords() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.records)
}

// RecentRecords returns the last n usage records (most recent first).
func (m *Meter) RecentRecords(n int) []domain.UsageRecord {
	m.mu.Lock()
	defer m.mu.Unlock()

	if n > len(m.records) {
		n = len(m.records)
	}
	result := make([]domain.UsageRecord, n)
	for i := 0; i < n; i++ {
		result[i] = m.records[len(m.records)-1-i]
	}
	return result
}

// Reset clears all usage records and client accumulators.
// Used in testing and billing period rollovers.
func (m *Meter) Reset() {
	m.mu.Lock()
	m.records = m.records[:0]
	m.byClient = make(map[string]*clientAccum)
	m.mu.Unlock()
}
