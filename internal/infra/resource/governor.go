package resource

import (
	"context"
	"sync"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// GovernorConfig controls governor behavior.
type GovernorConfig struct {
	ThermalThrottle int // CPU temp (C) to start throttling (default: 80)
	ThermalShutdown int // CPU temp (C) to kill all tasks (default: 95)
	BatteryMinPct   int // Battery % below which distributed is disabled (default: 20)
	TickInterval    time.Duration
}

// DefaultGovernorConfig returns safe defaults.
func DefaultGovernorConfig() GovernorConfig {
	return GovernorConfig{
		ThermalThrottle: 80,
		ThermalShutdown: 95,
		BatteryMinPct:   20,
		TickInterval:    5 * time.Second,
	}
}

// Governor dynamically computes a ComputeBudget based on idle state,
// thermal conditions, and battery level. It ensures TuTu NEVER
// degrades the user's experience. Architecture Part VII.
type Governor struct {
	mu      sync.RWMutex
	idle    *IdleDetector
	thermal *ThermalMonitor
	battery *BatteryMonitor
	config  GovernorConfig
	budget  domain.ComputeBudget
}

// NewGovernor creates a resource governor.
func NewGovernor(cfg GovernorConfig) *Governor {
	return &Governor{
		idle:    NewIdleDetector(),
		thermal: NewThermalMonitor(),
		battery: NewBatteryMonitor(),
		config:  cfg,
		budget: domain.ComputeBudget{
			MaxCPUPercent: 10, // Start conservative
		},
	}
}

// Budget returns the current compute budget (thread-safe).
func (g *Governor) Budget() domain.ComputeBudget {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.budget
}

// IdleLevel returns the current idle classification.
func (g *Governor) IdleLevel() domain.IdleLevel {
	return g.idle.Level()
}

// Run starts the governor tick loop. Call in a goroutine.
func (g *Governor) Run(ctx context.Context) {
	ticker := time.NewTicker(g.config.TickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.tick()
		}
	}
}

// tick recalculates the budget from all sensors.
func (g *Governor) tick() {
	g.idle.Update()
	level := g.idle.Level()

	budget := baseBudget(level)
	budget = g.applyThermalOverrides(budget)
	budget = g.applyBatteryOverrides(budget)

	g.mu.Lock()
	g.budget = budget
	g.mu.Unlock()
}

// baseBudget maps idle level to base resource allocation.
func baseBudget(level domain.IdleLevel) domain.ComputeBudget {
	switch level {
	case domain.IdleActive:
		return domain.ComputeBudget{MaxCPUPercent: 10, MaxGPUPercent: 0, AllowDistributed: false}
	case domain.IdleLight:
		return domain.ComputeBudget{MaxCPUPercent: 30, MaxGPUPercent: 20, AllowDistributed: true}
	case domain.IdleDeep:
		return domain.ComputeBudget{MaxCPUPercent: 80, MaxGPUPercent: 80, AllowDistributed: true, AllowLargeBatch: true}
	case domain.IdleLocked:
		return domain.ComputeBudget{MaxCPUPercent: 90, MaxGPUPercent: 90, AllowDistributed: true, AllowLargeBatch: true}
	case domain.IdleServer:
		return domain.ComputeBudget{MaxCPUPercent: 95, MaxGPUPercent: 95, AllowDistributed: true, AllowLargeBatch: true}
	default:
		return domain.ComputeBudget{MaxCPUPercent: 10}
	}
}

// applyThermalOverrides reduces budget when hardware is hot.
func (g *Governor) applyThermalOverrides(b domain.ComputeBudget) domain.ComputeBudget {
	cpuTemp := g.thermal.CPUTemp()
	gpuTemp := g.thermal.GPUTemp()

	if cpuTemp > g.config.ThermalShutdown || gpuTemp > g.config.ThermalShutdown {
		// Emergency: kill ALL tasks
		return domain.ComputeBudget{}
	}

	if cpuTemp > g.config.ThermalThrottle {
		b.MaxCPUPercent = min(b.MaxCPUPercent, 5)
		b.MaxGPUPercent = 0
	}
	if gpuTemp > g.config.ThermalThrottle {
		b.MaxGPUPercent = 0
	}

	return b
}

// applyBatteryOverrides reduces budget when on battery.
func (g *Governor) applyBatteryOverrides(b domain.ComputeBudget) domain.ComputeBudget {
	if !g.battery.IsPresent() {
		return b // Desktop — no battery constraints
	}

	pct := g.battery.Percentage()

	if pct < 10 {
		b.AllowDistributed = false
		b.AllowLargeBatch = false
	} else if pct < g.config.BatteryMinPct {
		b.AllowDistributed = false
	}

	if !g.battery.IsCharging() && pct < 50 {
		b.MaxCPUPercent = min(b.MaxCPUPercent, 30)
	}

	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
