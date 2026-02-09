//go:build linux

package resource

import (
	"os"
	"time"
)

// osIdleDuration returns how long the user has been idle on Linux.
// Tries /sys/class/graphics/fb0 modification time as a heuristic,
// falls back to 0 (assume active). Full X11/Wayland idle detection
// requires linking against libXss or using D-Bus logind, which will
// be added when we have a CI matrix for Linux.
func osIdleDuration() time.Duration {
	// Check if framebuffer exists (basic heuristic)
	info, err := os.Stat("/sys/class/graphics/fb0")
	if err != nil {
		return 0
	}
	return time.Since(info.ModTime())
}

// hasDisplay returns true if a graphical display is available.
func hasDisplay() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// isScreenLocked checks if the Linux screen is locked.
// Stub — full implementation will use D-Bus org.freedesktop.login1.
func isScreenLocked() bool {
	return false
}
