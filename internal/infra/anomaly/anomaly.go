// Package anomaly implements behavioral anomaly detection for network nodes.
//
// Each node has a statistical profile (avg task duration, success rate, CPU usage).
// Events that fall outside 3σ (standard deviations) are flagged as anomalies.
// Flagged nodes enter quarantine for verification with test tasks.
//
// Architecture Part XI §6 — Behavioral Anomaly Detection.
// Phase 5 spec: "ML-based behavioral analysis, resource abuse patterns."
package anomaly

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// ─── Constants ──────────────────────────────────────────────────────────────

const (
	// SigmaThreshold is the number of standard deviations for statistical outlier.
	SigmaThreshold = 3.0

	// MinSamplesForProfile is how many events before statistical checks kick in.
	MinSamplesForProfile = 5

	// MinCPUForInference is the minimum CPU usage expected for an inference task.
	// Below this → suspiciously low (probably returning cached/fake results).
	MinCPUForInference = 0.01

	// MaxConsecutiveAnomalies before escalation.
	MaxConsecutiveAnomalies = 3

	// ProfileExpiryDays is how long profile data is retained without updates.
	ProfileExpiryDays = 90

	// ThreatFeedMaxEntries caps the threat intelligence feed.
	ThreatFeedMaxEntries = 10000
)

// ─── Types ──────────────────────────────────────────────────────────────────

// AnomalyType identifies what kind of anomaly was detected.
type AnomalyType int

const (
	AnomalyNone           AnomalyType = iota // No anomaly
	AnomalyDurationOutlier                    // Task took much longer/shorter than expected
	AnomalyLowCPU                             // Suspiciously low CPU for task type
	AnomalyHighFailRate                       // Sudden spike in failures
	AnomalyEarningSpike                       // Abnormal credit earning rate
	AnomalyPatternMismatch                    // Behavior doesn't match historical profile
)

// String returns a human-readable anomaly type.
func (a AnomalyType) String() string {
	switch a {
	case AnomalyNone:
		return "NONE"
	case AnomalyDurationOutlier:
		return "DURATION_OUTLIER"
	case AnomalyLowCPU:
		return "LOW_CPU"
	case AnomalyHighFailRate:
		return "HIGH_FAIL_RATE"
	case AnomalyEarningSpike:
		return "EARNING_SPIKE"
	case AnomalyPatternMismatch:
		return "PATTERN_MISMATCH"
	default:
		return "UNKNOWN"
	}
}

// Severity indicates how serious an anomaly is.
type Severity int

const (
	SevInfo     Severity = iota // Informational, no action needed
	SevWarning                  // Worth watching
	SevCritical                 // Requires immediate quarantine
)

// String returns the severity label.
func (s Severity) String() string {
	switch s {
	case SevInfo:
		return "INFO"
	case SevWarning:
		return "WARNING"
	case SevCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

// TaskEvent describes a single task execution for anomaly analysis.
type TaskEvent struct {
	NodeID     string        `json:"node_id"`
	TaskID     string        `json:"task_id"`
	TaskType   string        `json:"task_type"` // "INFERENCE", "EMBEDDING", etc.
	Duration   time.Duration `json:"duration"`
	CPUUsage   float64       `json:"cpu_usage"` // 0.0 - 1.0
	Successful bool          `json:"successful"`
	Timestamp  time.Time     `json:"timestamp"`
}

// AnomalyResult is the outcome of analyzing a task event.
type AnomalyResult struct {
	IsAnomaly   bool        `json:"is_anomaly"`
	Type        AnomalyType `json:"type"`
	Severity    Severity    `json:"severity"`
	Description string      `json:"description"`
	NodeID      string      `json:"node_id"`
	Timestamp   time.Time   `json:"timestamp"`
}

// NodeProfile holds statistical data about a node's behavior.
// Updated incrementally using Welford's online algorithm for mean/variance.
type NodeProfile struct {
	NodeID string `json:"node_id"`

	// Duration statistics (Welford's online algorithm)
	DurationCount int     `json:"duration_count"`
	DurationMean  float64 `json:"duration_mean"`  // Running mean in ms
	DurationM2    float64 `json:"duration_m2"`    // Running variance sum

	// Success rate (moving window)
	SuccessCount int `json:"success_count"`
	FailureCount int `json:"failure_count"`

	// CPU usage statistics
	CPUCount int     `json:"cpu_count"`
	CPUMean  float64 `json:"cpu_mean"`
	CPUM2    float64 `json:"cpu_m2"`

	// Earning rate (credits per hour)
	EarningCount int     `json:"earning_count"`
	EarningMean  float64 `json:"earning_mean"`
	EarningM2    float64 `json:"earning_m2"`

	// Anomaly tracking
	ConsecutiveAnomalies int       `json:"consecutive_anomalies"`
	TotalAnomalies       int       `json:"total_anomalies"`
	LastAnomaly          time.Time `json:"last_anomaly"`
	LastUpdate           time.Time `json:"last_update"`
	CreatedAt            time.Time `json:"created_at"`
}

// DurationStddev returns the standard deviation of task duration.
func (p *NodeProfile) DurationStddev() float64 {
	if p.DurationCount < 2 {
		return 0
	}
	return math.Sqrt(p.DurationM2 / float64(p.DurationCount-1))
}

// CPUStddev returns the standard deviation of CPU usage.
func (p *NodeProfile) CPUStddev() float64 {
	if p.CPUCount < 2 {
		return 0
	}
	return math.Sqrt(p.CPUM2 / float64(p.CPUCount-1))
}

// SuccessRate returns the fraction of successful tasks.
func (p *NodeProfile) SuccessRate() float64 {
	total := p.SuccessCount + p.FailureCount
	if total == 0 {
		return 1.0
	}
	return float64(p.SuccessCount) / float64(total)
}

// ThreatEntry represents a known bad actor in the threat intelligence feed.
type ThreatEntry struct {
	NodeID      string    `json:"node_id"`
	Reason      string    `json:"reason"`
	ReportedBy  string    `json:"reported_by"`
	ReportedAt  time.Time `json:"reported_at"`
	AutoBanned  bool      `json:"auto_banned"`
}

// DetectorStats provides an overview of the anomaly detector's state.
type DetectorStats struct {
	ProfileCount     int `json:"profile_count"`
	TotalAnomalies   int `json:"total_anomalies"`
	ThreatFeedSize   int `json:"threat_feed_size"`
	ActiveQuarantines int `json:"active_quarantines"`
}

// ─── Configuration ──────────────────────────────────────────────────────────

// DetectorConfig configures the anomaly detector.
type DetectorConfig struct {
	SigmaThreshold        float64 // Standard deviations for outlier (default: 3.0)
	MinSamples            int     // Minimum events before statistical checks (default: 5)
	MaxConsecutiveAnomaly int     // Anomalies before escalation (default: 3)
}

// DefaultDetectorConfig returns Phase 5 defaults.
func DefaultDetectorConfig() DetectorConfig {
	return DetectorConfig{
		SigmaThreshold:        SigmaThreshold,
		MinSamples:            MinSamplesForProfile,
		MaxConsecutiveAnomaly: MaxConsecutiveAnomalies,
	}
}

// ─── Detector ───────────────────────────────────────────────────────────────

// Detector runs anomaly detection on task events.
// Thread-safe via RWMutex.
type Detector struct {
	mu       sync.RWMutex
	config   DetectorConfig
	profiles map[string]*NodeProfile // nodeID → profile
	threats  []ThreatEntry           // Threat intelligence feed

	// Injectable clock for testing.
	now func() time.Time
}

// NewDetector creates an anomaly detector.
func NewDetector(cfg DetectorConfig) *Detector {
	return &Detector{
		config:   cfg,
		profiles: make(map[string]*NodeProfile),
		now:      time.Now,
	}
}

// ─── Event Analysis ─────────────────────────────────────────────────────────

// Analyze checks a task event for anomalies and updates the node profile.
// Returns the analysis result.
func (d *Detector) Analyze(event TaskEvent) AnomalyResult {
	d.mu.Lock()
	defer d.mu.Unlock()

	profile := d.getOrCreateProfile(event.NodeID)
	result := AnomalyResult{
		NodeID:    event.NodeID,
		Timestamp: event.Timestamp,
	}

	// Check 1: Duration outlier (only if enough samples)
	if profile.DurationCount >= d.config.MinSamples {
		durationMs := float64(event.Duration.Milliseconds())
		stddev := profile.DurationStddev()
		if stddev > 0 {
			zScore := math.Abs(durationMs-profile.DurationMean) / stddev
			if zScore > d.config.SigmaThreshold {
				result.IsAnomaly = true
				result.Type = AnomalyDurationOutlier
				result.Severity = SevWarning
				result.Description = fmt.Sprintf(
					"task duration %.0fms is %.1fσ from mean %.0fms (stddev=%.0fms)",
					durationMs, zScore, profile.DurationMean, stddev,
				)
			}
		}
	}

	// Check 2: Suspiciously low CPU for inference tasks
	if !result.IsAnomaly && event.TaskType == "INFERENCE" && event.CPUUsage < MinCPUForInference {
		result.IsAnomaly = true
		result.Type = AnomalyLowCPU
		result.Severity = SevCritical
		result.Description = fmt.Sprintf(
			"CPU usage %.4f for INFERENCE task — likely returning cached/fake results",
			event.CPUUsage,
		)
	}

	// Check 3: High failure rate (sudden spike above historical)
	if !result.IsAnomaly && !event.Successful {
		total := profile.SuccessCount + profile.FailureCount
		if total >= d.config.MinSamples {
			// If recent failure rate jumps to >50% when historical is <10%
			recentFailRate := float64(profile.FailureCount) / float64(total)
			if recentFailRate > 0.5 && profile.SuccessRate() > 0.8 {
				result.IsAnomaly = true
				result.Type = AnomalyHighFailRate
				result.Severity = SevWarning
				result.Description = fmt.Sprintf(
					"failure rate spiked to %.0f%% (historical success: %.0f%%)",
					recentFailRate*100, profile.SuccessRate()*100,
				)
			}
		}
	}

	// Update profile with this event (Welford's algorithm)
	d.updateProfile(profile, event)

	// Track consecutive anomalies for escalation
	if result.IsAnomaly {
		profile.ConsecutiveAnomalies++
		profile.TotalAnomalies++
		profile.LastAnomaly = d.now()

		// Escalate severity if consecutive anomalies exceed threshold
		if profile.ConsecutiveAnomalies >= d.config.MaxConsecutiveAnomaly {
			result.Severity = SevCritical
			result.Description += fmt.Sprintf(
				" [ESCALATED: %d consecutive anomalies]",
				profile.ConsecutiveAnomalies,
			)
		}
	} else {
		profile.ConsecutiveAnomalies = 0 // Reset on clean event
	}

	return result
}

// updateProfile updates a node's statistical profile with a new event.
// Uses Welford's online algorithm for numerically stable running mean/variance.
func (d *Detector) updateProfile(p *NodeProfile, event TaskEvent) {
	now := d.now()

	// Duration update (Welford's)
	durationMs := float64(event.Duration.Milliseconds())
	p.DurationCount++
	delta := durationMs - p.DurationMean
	p.DurationMean += delta / float64(p.DurationCount)
	delta2 := durationMs - p.DurationMean
	p.DurationM2 += delta * delta2

	// CPU update (Welford's)
	if event.CPUUsage > 0 {
		p.CPUCount++
		cpuDelta := event.CPUUsage - p.CPUMean
		p.CPUMean += cpuDelta / float64(p.CPUCount)
		cpuDelta2 := event.CPUUsage - p.CPUMean
		p.CPUM2 += cpuDelta * cpuDelta2
	}

	// Success/failure count
	if event.Successful {
		p.SuccessCount++
	} else {
		p.FailureCount++
	}

	p.LastUpdate = now
}

// getOrCreateProfile returns or initializes a node's profile.
func (d *Detector) getOrCreateProfile(nodeID string) *NodeProfile {
	if p, ok := d.profiles[nodeID]; ok {
		return p
	}
	now := d.now()
	p := &NodeProfile{
		NodeID:     nodeID,
		CreatedAt:  now,
		LastUpdate: now,
	}
	d.profiles[nodeID] = p
	return p
}

// ─── Threat Intelligence ────────────────────────────────────────────────────

// ReportThreat adds a node to the threat intelligence feed.
func (d *Detector) ReportThreat(nodeID, reason, reportedBy string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check for duplicate
	for _, t := range d.threats {
		if t.NodeID == nodeID && t.Reason == reason {
			return // Already reported
		}
	}

	entry := ThreatEntry{
		NodeID:     nodeID,
		Reason:     reason,
		ReportedBy: reportedBy,
		ReportedAt: d.now(),
	}
	d.threats = append(d.threats, entry)

	// Cap the feed
	if len(d.threats) > ThreatFeedMaxEntries {
		d.threats = d.threats[len(d.threats)-ThreatFeedMaxEntries:]
	}
}

// IsKnownThreat checks if a node is in the threat feed.
func (d *Detector) IsKnownThreat(nodeID string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, t := range d.threats {
		if t.NodeID == nodeID {
			return true
		}
	}
	return false
}

// ThreatFeed returns the current threat entries.
func (d *Detector) ThreatFeed() []ThreatEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]ThreatEntry, len(d.threats))
	copy(result, d.threats)
	return result
}

// ─── Queries ────────────────────────────────────────────────────────────────

// GetProfile returns a node's anomaly profile.
func (d *Detector) GetProfile(nodeID string) *NodeProfile {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.profiles[nodeID]
}

// ProfileCount returns the number of tracked node profiles.
func (d *Detector) ProfileCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.profiles)
}

// Stats returns aggregate detector metrics.
func (d *Detector) Stats() DetectorStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	stats := DetectorStats{
		ProfileCount:   len(d.profiles),
		ThreatFeedSize: len(d.threats),
	}

	for _, p := range d.profiles {
		stats.TotalAnomalies += p.TotalAnomalies
	}

	return stats
}

// CleanupStaleProfiles removes profiles older than ProfileExpiryDays.
func (d *Detector) CleanupStaleProfiles() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := d.now().AddDate(0, 0, -ProfileExpiryDays)
	removed := 0

	for nodeID, p := range d.profiles {
		if p.LastUpdate.Before(cutoff) {
			delete(d.profiles, nodeID)
			removed++
		}
	}
	return removed
}
