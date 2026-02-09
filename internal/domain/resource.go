// Package domain — Phase 1 resource budget types.
// The resource governor controls how much CPU/GPU TuTu may use,
// based on idle state, thermals, and battery. Architecture Part VII.
package domain

// IdleLevel classifies the user's current activity state.
type IdleLevel int

const (
	IdleActive IdleLevel = iota // User actively using computer
	IdleLight                   // Stepped away briefly (<3 min)
	IdleDeep                    // Away extended period (>15 min, low CPU)
	IdleLocked                  // Screen locked
	IdleServer                  // Headless mode (no display)
)

// String returns human-readable idle level.
func (l IdleLevel) String() string {
	switch l {
	case IdleActive:
		return "active"
	case IdleLight:
		return "light"
	case IdleDeep:
		return "deep"
	case IdleLocked:
		return "locked"
	case IdleServer:
		return "server"
	default:
		return "unknown"
	}
}

// ComputeBudget defines what resources TuTu is allowed to consume.
// The governor recalculates this every few seconds.
type ComputeBudget struct {
	MaxCPUPercent    int  `json:"max_cpu_percent"`
	MaxGPUPercent    int  `json:"max_gpu_percent"`
	AllowDistributed bool `json:"allow_distributed"`
	AllowLargeBatch  bool `json:"allow_large_batch"`
}

// CanAcceptWork returns true if distributed tasks are permitted.
func (b ComputeBudget) CanAcceptWork() bool {
	return b.AllowDistributed && b.MaxCPUPercent > 0
}
