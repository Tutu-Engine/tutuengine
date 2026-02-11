// Package universal implements Phase 7 universal access tier management.
//
// This package enforces the tiered access system that makes AI available to everyone:
//   - Free tier: 100 inferences/day — funded by enterprise MCP revenue
//   - Education tier: unlimited for verified students/researchers
//   - Pro tier: 10K/day for credit-funded power users
//   - Enterprise tier: unlimited with SLA guarantees
//
// The key insight: the free tier is what makes TuTu a public good.
// Enterprise revenue funds the free tier — everyone benefits from AI.
//
// Architecture Reference: Phase 7 Gate Check "Free tier operational and funded".
package universal

import (
	"fmt"
	"sync"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ═══════════════════════════════════════════════════════════════════════════
// Configuration
// ═══════════════════════════════════════════════════════════════════════════

// Config controls the universal access system.
type Config struct {
	// Quotas per tier — defaults from domain.DefaultTierQuotas()
	Quotas map[domain.AccessTier]domain.TierQuota

	// FreeTierEnabled controls whether the free tier is active.
	// Requires enterprise revenue to fund it.
	FreeTierEnabled bool

	// EducationDomains are recognized academic email domains.
	EducationDomains []string

	// GracePeriodMinutes: extra time given after quota exhaustion
	// before requests are hard-rejected (allows finishing current work).
	GracePeriodMinutes int

	// DefaultTier is the tier assigned to new/anonymous users.
	DefaultTier domain.AccessTier
}

// DefaultConfig returns the architecture-specified tier settings.
func DefaultConfig() Config {
	return Config{
		Quotas:          domain.DefaultTierQuotas(),
		FreeTierEnabled: true,
		EducationDomains: []string{
			".edu", ".ac.uk", ".edu.au", ".ac.jp", ".edu.cn",
			".edu.br", ".ac.in", ".edu.sg", ".ac.nz", ".edu.za",
		},
		GracePeriodMinutes: 5,
		DefaultTier:        domain.AccessTierFree,
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Access Manager — enforces tier quotas
// ═══════════════════════════════════════════════════════════════════════════

// AccessManager enforces universal access tier quotas.
// It tracks per-user usage, checks quotas before allowing requests,
// and manages education verification.
type AccessManager struct {
	mu     sync.RWMutex
	config Config

	// Per-user usage tracking (userID → usage)
	usage map[string]*domain.TierUsage

	// Education verifications (userID → verification)
	eduVerifications map[string]*domain.EducationVerification

	// Aggregate statistics
	totalFreeInferences       int64
	totalEducationInferences  int64
	totalProInferences        int64
	totalEnterpriseInferences int64

	// Injectable clock for testing
	now func() time.Time
}

// NewAccessManager creates an AccessManager with the given configuration.
func NewAccessManager(cfg Config) *AccessManager {
	return &AccessManager{
		config:           cfg,
		usage:            make(map[string]*domain.TierUsage),
		eduVerifications: make(map[string]*domain.EducationVerification),
		now:              time.Now,
	}
}

// CheckAccess determines whether a user can make another inference.
// Returns nil if allowed, or an error explaining why not.
//
// This is the hot path — called before every inference request.
// It must be fast (< 1ms).
func (am *AccessManager) CheckAccess(userID string) error {
	am.mu.RLock()
	defer am.mu.RUnlock()

	tier := am.userTier(userID)
	quota, ok := am.config.Quotas[tier]
	if !ok {
		return fmt.Errorf("unknown tier: %q", tier)
	}

	usage := am.getOrCreateUsage(userID, tier)

	if usage.IsExhausted(quota) {
		if tier == domain.AccessTierFree {
			return domain.ErrFreeTierExhausted
		}
		return domain.ErrQuotaExceeded
	}

	return nil
}

// RecordInference increments the usage counter for a user.
// Call this AFTER a successful inference.
func (am *AccessManager) RecordInference(userID string, tokensUsed int64) {
	am.mu.Lock()
	defer am.mu.Unlock()

	tier := am.userTier(userID)
	usage := am.getOrCreateUsageLocked(userID, tier)
	usage.InferencesToday++
	usage.TokensToday += tokensUsed

	// Update aggregate stats
	switch tier {
	case domain.AccessTierFree:
		am.totalFreeInferences++
	case domain.AccessTierEducation:
		am.totalEducationInferences++
	case domain.AccessTierPro:
		am.totalProInferences++
	case domain.AccessTierEnterprise:
		am.totalEnterpriseInferences++
	}
}

// GetUsage returns the current usage for a user.
func (am *AccessManager) GetUsage(userID string) domain.TierUsage {
	am.mu.RLock()
	defer am.mu.RUnlock()

	tier := am.userTier(userID)
	return *am.getOrCreateUsage(userID, tier)
}

// RemainingQuota returns how many inferences a user has left today.
func (am *AccessManager) RemainingQuota(userID string) int64 {
	am.mu.RLock()
	defer am.mu.RUnlock()

	tier := am.userTier(userID)
	quota := am.config.Quotas[tier]
	usage := am.getOrCreateUsage(userID, tier)
	return usage.RemainingInferences(quota)
}

// ═══════════════════════════════════════════════════════════════════════════
// Tier Management
// ═══════════════════════════════════════════════════════════════════════════

// SetUserTier explicitly sets a user's tier (e.g., after payment or verification).
func (am *AccessManager) SetUserTier(userID string, tier domain.AccessTier) error {
	if !tier.IsValid() {
		return fmt.Errorf("invalid tier: %q", tier)
	}

	am.mu.Lock()
	defer am.mu.Unlock()

	if usage, ok := am.usage[userID]; ok {
		usage.Tier = tier
	} else {
		am.usage[userID] = &domain.TierUsage{
			UserID:  userID,
			Tier:    tier,
			ResetAt: am.nextMidnightUTC(),
		}
	}
	return nil
}

// VerifyEducation records a successful education tier verification.
func (am *AccessManager) VerifyEducation(userID, institution, email string) error {
	// Validate email domain
	validDomain := false
	for _, d := range am.config.EducationDomains {
		if len(email) > len(d) && email[len(email)-len(d):] == d {
			validDomain = true
			break
		}
	}
	if !validDomain {
		return domain.ErrEduTierUnverified
	}

	am.mu.Lock()
	defer am.mu.Unlock()

	now := am.now()
	am.eduVerifications[userID] = &domain.EducationVerification{
		UserID:      userID,
		Institution: institution,
		Email:       email,
		Status:      "verified",
		VerifiedAt:  now,
		ExpiresAt:   now.AddDate(1, 0, 0), // 1 year
	}

	// Upgrade tier
	if usage, ok := am.usage[userID]; ok {
		usage.Tier = domain.AccessTierEducation
	}

	return nil
}

// IsEducationVerified checks if a user has active education verification.
func (am *AccessManager) IsEducationVerified(userID string) bool {
	am.mu.RLock()
	defer am.mu.RUnlock()

	if ev, ok := am.eduVerifications[userID]; ok {
		return ev.IsVerified()
	}
	return false
}

// ═══════════════════════════════════════════════════════════════════════════
// Daily Reset
// ═══════════════════════════════════════════════════════════════════════════

// ResetDailyQuotas resets all users' daily usage counters.
// This should be called at midnight UTC by a scheduled job.
func (am *AccessManager) ResetDailyQuotas() {
	am.mu.Lock()
	defer am.mu.Unlock()

	nextReset := am.nextMidnightUTC()
	for _, usage := range am.usage {
		usage.InferencesToday = 0
		usage.TokensToday = 0
		usage.ResetAt = nextReset
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Statistics
// ═══════════════════════════════════════════════════════════════════════════

// Stats returns aggregate access statistics.
type Stats struct {
	TotalUsers               int   `json:"total_users"`
	FreeUsers                int   `json:"free_users"`
	EducationUsers           int   `json:"education_users"`
	ProUsers                 int   `json:"pro_users"`
	EnterpriseUsers          int   `json:"enterprise_users"`
	TotalFreeInferences      int64 `json:"total_free_inferences"`
	TotalEducationInferences int64 `json:"total_education_inferences"`
	TotalProInferences       int64 `json:"total_pro_inferences"`
	TotalEnterpriseInferences int64 `json:"total_enterprise_inferences"`
	FreeTierEnabled          bool  `json:"free_tier_enabled"`
}

// GetStats returns current access statistics.
func (am *AccessManager) GetStats() Stats {
	am.mu.RLock()
	defer am.mu.RUnlock()

	stats := Stats{
		TotalUsers:                len(am.usage),
		FreeTierEnabled:           am.config.FreeTierEnabled,
		TotalFreeInferences:       am.totalFreeInferences,
		TotalEducationInferences:  am.totalEducationInferences,
		TotalProInferences:        am.totalProInferences,
		TotalEnterpriseInferences: am.totalEnterpriseInferences,
	}

	for _, u := range am.usage {
		switch u.Tier {
		case domain.AccessTierFree:
			stats.FreeUsers++
		case domain.AccessTierEducation:
			stats.EducationUsers++
		case domain.AccessTierPro:
			stats.ProUsers++
		case domain.AccessTierEnterprise:
			stats.EnterpriseUsers++
		}
	}

	return stats
}

// ═══════════════════════════════════════════════════════════════════════════
// Internal helpers
// ═══════════════════════════════════════════════════════════════════════════

// userTier returns the user's current tier (caller must hold at least RLock).
func (am *AccessManager) userTier(userID string) domain.AccessTier {
	if usage, ok := am.usage[userID]; ok {
		return usage.Tier
	}
	return am.config.DefaultTier
}

// getOrCreateUsage returns usage for a user, creating if needed (RLock held).
func (am *AccessManager) getOrCreateUsage(userID string, tier domain.AccessTier) *domain.TierUsage {
	if usage, ok := am.usage[userID]; ok {
		return usage
	}
	// Return a temporary zero usage — caller has RLock, can't create
	return &domain.TierUsage{
		UserID:  userID,
		Tier:    tier,
		ResetAt: am.nextMidnightUTC(),
	}
}

// getOrCreateUsageLocked returns or creates usage (Lock held — can write).
func (am *AccessManager) getOrCreateUsageLocked(userID string, tier domain.AccessTier) *domain.TierUsage {
	if usage, ok := am.usage[userID]; ok {
		return usage
	}
	usage := &domain.TierUsage{
		UserID:  userID,
		Tier:    tier,
		ResetAt: am.nextMidnightUTC(),
	}
	am.usage[userID] = usage
	return usage
}

// nextMidnightUTC returns the next midnight UTC time.
func (am *AccessManager) nextMidnightUTC() time.Time {
	now := am.now().UTC()
	tomorrow := now.AddDate(0, 0, 1)
	return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)
}
