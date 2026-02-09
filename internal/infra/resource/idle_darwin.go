//go:build darwin

package resource

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// osIdleDuration returns how long the user has been idle on macOS.
// Uses ioreg to query HIDIdleTime (in nanoseconds).
func osIdleDuration() time.Duration {
	out, err := exec.Command("ioreg", "-c", "IOHIDSystem", "-d", "4").Output()
	if err != nil {
		return 0
	}
	// Parse "HIDIdleTime" = <nanoseconds>
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "HIDIdleTime") {
			parts := strings.Split(line, "=")
			if len(parts) == 2 {
				ns, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
				if err == nil {
					return time.Duration(ns)
				}
			}
		}
	}
	return 0
}

// hasDisplay returns true if a graphical display is available.
// macOS always has a display unless running headless in CI.
func hasDisplay() bool {
	return true
}

// isScreenLocked checks if the macOS screen is locked.
// Uses CGSessionCopyCurrentDictionary via Python bridge (no CGO needed).
func isScreenLocked() bool {
	out, err := exec.Command("python3", "-c",
		`import Quartz; d=Quartz.CGSessionCopyCurrentDictionary(); print(d.get("CGSSessionScreenIsLocked",0))`).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "1"
}
