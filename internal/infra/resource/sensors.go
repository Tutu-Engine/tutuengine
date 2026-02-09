package resource

// ThermalMonitor reads CPU and GPU temperatures.
// Phase 1 foundation uses a stub implementation. Full platform-specific
// implementations (WMI on Windows, CoreFoundation on macOS, sysfs on Linux)
// will be wired in Step 1.1 polish.
type ThermalMonitor struct{}

// NewThermalMonitor creates a thermal monitor.
func NewThermalMonitor() *ThermalMonitor {
	return &ThermalMonitor{}
}

// CPUTemp returns the CPU temperature in Celsius.
// Returns 0 when sensor data is unavailable (safe default — no throttle).
func (t *ThermalMonitor) CPUTemp() int {
	return readCPUTemp()
}

// GPUTemp returns the GPU temperature in Celsius.
func (t *ThermalMonitor) GPUTemp() int {
	return readGPUTemp()
}

// BatteryMonitor reads battery state.
type BatteryMonitor struct{}

// NewBatteryMonitor creates a battery monitor.
func NewBatteryMonitor() *BatteryMonitor {
	return &BatteryMonitor{}
}

// IsPresent returns true if the machine has a battery (laptop).
func (b *BatteryMonitor) IsPresent() bool {
	return hasBattery()
}

// Percentage returns battery charge level (0-100).
func (b *BatteryMonitor) Percentage() int {
	return batteryPercentage()
}

// IsCharging returns true if plugged in and charging.
func (b *BatteryMonitor) IsCharging() bool {
	return isBatteryCharging()
}
