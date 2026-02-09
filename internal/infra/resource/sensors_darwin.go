//go:build darwin

package resource

import (
	"os/exec"
	"strconv"
	"strings"
)

// readCPUTemp reads CPU temperature on macOS.
// Uses osx-cpu-temp if installed, otherwise returns 0.
func readCPUTemp() int {
	out, err := exec.Command("osx-cpu-temp").Output()
	if err != nil {
		return 0
	}
	// Output format: "65.0°C"
	s := strings.TrimSpace(string(out))
	s = strings.TrimSuffix(s, "°C")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int(f)
}

// readGPUTemp reads GPU temperature. Stub for Phase 1.
func readGPUTemp() int {
	return 0
}

// hasBattery checks for battery on macOS.
func hasBattery() bool {
	out, err := exec.Command("pmset", "-g", "batt").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Battery")
}

// batteryPercentage returns charge on macOS.
func batteryPercentage() int {
	out, err := exec.Command("pmset", "-g", "batt").Output()
	if err != nil {
		return 100
	}
	// Parse "XX%" from output
	for _, line := range strings.Split(string(out), "\n") {
		if idx := strings.Index(line, "%"); idx > 0 {
			start := idx - 1
			for start > 0 && line[start-1] >= '0' && line[start-1] <= '9' {
				start--
			}
			pct, _ := strconv.Atoi(line[start:idx])
			if pct > 0 {
				return pct
			}
		}
	}
	return 100
}

// isBatteryCharging returns charging state on macOS.
func isBatteryCharging() bool {
	out, err := exec.Command("pmset", "-g", "batt").Output()
	if err != nil {
		return true
	}
	return strings.Contains(string(out), "AC Power") || strings.Contains(string(out), "charging")
}
