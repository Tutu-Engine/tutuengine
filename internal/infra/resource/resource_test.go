package resource

import (
	"testing"

	"github.com/tutu-network/tutu/internal/domain"
)

// ─── Governor Budget Tests ──────────────────────────────────────────────────

func TestBaseBudget_Active(t *testing.T) {
	b := baseBudget(domain.IdleActive)
	if b.MaxCPUPercent != 10 {
		t.Errorf("Active CPU = %d, want 10", b.MaxCPUPercent)
	}
	if b.MaxGPUPercent != 0 {
		t.Errorf("Active GPU = %d, want 0", b.MaxGPUPercent)
	}
	if b.AllowDistributed {
		t.Error("Active should not allow distributed")
	}
}

func TestBaseBudget_Light(t *testing.T) {
	b := baseBudget(domain.IdleLight)
	if b.MaxCPUPercent != 30 {
		t.Errorf("Light CPU = %d, want 30", b.MaxCPUPercent)
	}
	if !b.AllowDistributed {
		t.Error("Light should allow distributed")
	}
}

func TestBaseBudget_Deep(t *testing.T) {
	b := baseBudget(domain.IdleDeep)
	if b.MaxCPUPercent != 80 {
		t.Errorf("Deep CPU = %d, want 80", b.MaxCPUPercent)
	}
	if !b.AllowLargeBatch {
		t.Error("Deep should allow large batch")
	}
}

func TestBaseBudget_Locked(t *testing.T) {
	b := baseBudget(domain.IdleLocked)
	if b.MaxCPUPercent != 90 {
		t.Errorf("Locked CPU = %d, want 90", b.MaxCPUPercent)
	}
}

func TestBaseBudget_Server(t *testing.T) {
	b := baseBudget(domain.IdleServer)
	if b.MaxCPUPercent != 95 {
		t.Errorf("Server CPU = %d, want 95", b.MaxCPUPercent)
	}
}

func TestBaseBudget_AllLevels(t *testing.T) {
	levels := []domain.IdleLevel{
		domain.IdleActive, domain.IdleLight, domain.IdleDeep,
		domain.IdleLocked, domain.IdleServer,
	}
	prevCPU := 0
	for _, level := range levels {
		b := baseBudget(level)
		if b.MaxCPUPercent < prevCPU {
			t.Errorf("Budget should be monotonically increasing: level %s got %d, prev %d",
				level, b.MaxCPUPercent, prevCPU)
		}
		prevCPU = b.MaxCPUPercent
	}
}

// ─── Governor Tests ─────────────────────────────────────────────────────────

func TestGovernor_New(t *testing.T) {
	cfg := DefaultGovernorConfig()
	g := NewGovernor(cfg)

	b := g.Budget()
	if b.MaxCPUPercent != 10 {
		t.Errorf("initial Budget CPU = %d, want 10 (conservative)", b.MaxCPUPercent)
	}
}

func TestGovernor_ThermalShutdown(t *testing.T) {
	cfg := DefaultGovernorConfig()
	g := NewGovernor(cfg)

	// ThermalMonitor reads 0°C on platforms without sensors.
	// Set shutdown threshold to -1 so that 0 > -1 triggers shutdown.
	g.config.ThermalShutdown = -1

	b := domain.ComputeBudget{MaxCPUPercent: 80, MaxGPUPercent: 80, AllowDistributed: true, AllowLargeBatch: true}
	b = g.applyThermalOverrides(b)

	if b.MaxCPUPercent != 0 || b.MaxGPUPercent != 0 {
		t.Errorf("After thermal shutdown: CPU=%d GPU=%d, both should be 0", b.MaxCPUPercent, b.MaxGPUPercent)
	}
	if b.AllowDistributed {
		t.Error("Thermal shutdown should disable distributed")
	}
}

func TestGovernor_BatteryOverrides(t *testing.T) {
	cfg := DefaultGovernorConfig()
	g := NewGovernor(cfg)

	// No battery = no constraints
	b := domain.ComputeBudget{MaxCPUPercent: 80, AllowDistributed: true}
	b = g.applyBatteryOverrides(b)
	if b.MaxCPUPercent != 80 {
		t.Errorf("Desktop (no battery) CPU = %d, want 80", b.MaxCPUPercent)
	}
}

// ─── IdleDetector Tests ─────────────────────────────────────────────────────

func TestIdleDetector_Initial(t *testing.T) {
	d := NewIdleDetector()
	level := d.Level()
	// Initial level should be Active (conservative default)
	if level != domain.IdleActive {
		t.Errorf("initial Level = %v, want Active", level)
	}
}

func TestIdleDetector_Update(t *testing.T) {
	d := NewIdleDetector()
	// Just verify Update doesn't panic
	d.Update()
}
