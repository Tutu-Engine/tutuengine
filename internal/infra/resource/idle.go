// Package resource provides idle detection and resource governance.
// Architecture Parts VII & VIII: TuTu NEVER degrades user experience.
package resource

import (
	"sync"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// IdleDetector monitors user activity and classifies idle state.
// Uses platform-specific APIs (Windows GetLastInputInfo, macOS
// CGEventSource, Linux X11/logind) wrapped behind osIdleDuration().
type IdleDetector struct {
	mu         sync.RWMutex
	level      domain.IdleLevel
	lastUpdate time.Time
}

// NewIdleDetector creates an idle detector.
func NewIdleDetector() *IdleDetector {
	return &IdleDetector{
		level:      domain.IdleActive,
		lastUpdate: time.Now(),
	}
}

// Level returns the current idle classification.
func (d *IdleDetector) Level() domain.IdleLevel {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.level
}

// Update recalculates the idle level from platform sensors.
// Called periodically by the governor tick loop.
func (d *IdleDetector) Update() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !hasDisplay() {
		d.level = domain.IdleServer
		d.lastUpdate = time.Now()
		return
	}

	idle := osIdleDuration()

	if isScreenLocked() {
		d.level = domain.IdleLocked
	} else if idle < 3*time.Minute {
		d.level = domain.IdleActive
	} else if idle > 15*time.Minute {
		d.level = domain.IdleDeep
	} else {
		d.level = domain.IdleLight
	}

	d.lastUpdate = time.Now()
}

// IdleDuration returns the raw idle duration from the OS.
func (d *IdleDetector) IdleDuration() time.Duration {
	return osIdleDuration()
}
