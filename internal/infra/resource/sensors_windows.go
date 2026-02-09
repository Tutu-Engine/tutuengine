//go:build windows

package resource

import (
	"os/exec"
	"strconv"
	"strings"
)

// readCPUTemp reads CPU temperature on Windows via WMI.
// Returns 0 if unavailable (safe: no throttle triggered).
func readCPUTemp() int {
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		`Get-CimInstance MSAcpi_ThermalZoneTemperature -Namespace root/wmi -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty CurrentTemperature`).Output()
	if err != nil {
		return 0
	}
	// WMI returns temperature in tenths of Kelvin
	val, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0
	}
	celsius := (val / 10) - 273
	if celsius < 0 || celsius > 150 {
		return 0
	}
	return celsius
}

// readGPUTemp reads GPU temperature. Stub for Phase 1.
func readGPUTemp() int {
	return 0
}

// hasBattery checks for battery presence on Windows.
func hasBattery() bool {
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		`(Get-CimInstance Win32_Battery -ErrorAction SilentlyContinue).Count`).Output()
	if err != nil {
		return false
	}
	count, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return count > 0
}

// batteryPercentage returns charge level on Windows.
func batteryPercentage() int {
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		`(Get-CimInstance Win32_Battery -ErrorAction SilentlyContinue).EstimatedChargeRemaining`).Output()
	if err != nil {
		return 100
	}
	pct, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	if pct == 0 {
		return 100 // Assume full if query fails
	}
	return pct
}

// isBatteryCharging returns charging status on Windows.
func isBatteryCharging() bool {
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		`(Get-CimInstance Win32_Battery -ErrorAction SilentlyContinue).BatteryStatus`).Output()
	if err != nil {
		return true
	}
	status, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return status == 2 // 2 = AC connected / charging
}
