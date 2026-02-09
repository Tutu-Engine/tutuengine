//go:build linux

package resource

import (
	"os"
	"strconv"
	"strings"
)

// readCPUTemp reads CPU temperature on Linux via sysfs thermal zone.
func readCPUTemp() int {
	data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return 0
	}
	milliC, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return milliC / 1000
}

// readGPUTemp reads GPU temperature. Stub for Phase 1.
func readGPUTemp() int {
	return 0
}

// hasBattery checks for battery on Linux via sysfs.
func hasBattery() bool {
	_, err := os.Stat("/sys/class/power_supply/BAT0")
	return err == nil
}

// batteryPercentage returns charge on Linux.
func batteryPercentage() int {
	data, err := os.ReadFile("/sys/class/power_supply/BAT0/capacity")
	if err != nil {
		return 100
	}
	pct, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if pct == 0 {
		return 100
	}
	return pct
}

// isBatteryCharging returns charging state on Linux.
func isBatteryCharging() bool {
	data, err := os.ReadFile("/sys/class/power_supply/BAT0/status")
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(data)) == "Charging"
}
