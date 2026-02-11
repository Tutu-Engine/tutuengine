package universal

import (
	"testing"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// fixedTime returns a deterministic time for testing.
func fixedTime() time.Time {
	return time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC)
}

// ═══════════════════════════════════════════════════════════════════════════
// AccessManager Construction
// ═══════════════════════════════════════════════════════════════════════════

func TestNewAccessManager(t *testing.T) {
	am := NewAccessManager(DefaultConfig())
	if am == nil {
		t.Fatal("expected non-nil AccessManager")
	}
	stats := am.GetStats()
	if stats.TotalUsers != 0 {
		t.Fatalf("expected 0 users, got %d", stats.TotalUsers)
	}
	if !stats.FreeTierEnabled {
		t.Fatal("expected free tier enabled by default")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// CheckAccess Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestCheckAccess_FreeTierAllowed(t *testing.T) {
	am := NewAccessManager(DefaultConfig())
	am.now = fixedTime

	// First request for a new user should succeed
	if err := am.CheckAccess("user-1"); err != nil {
		t.Fatalf("expected free tier access, got: %v", err)
	}
}

func TestCheckAccess_FreeTierExhausted(t *testing.T) {
	am := NewAccessManager(DefaultConfig())
	am.now = fixedTime

	// Use up the free tier (100 inferences)
	for i := 0; i < 100; i++ {
		am.RecordInference("user-1", 100)
	}

	err := am.CheckAccess("user-1")
	if err != domain.ErrFreeTierExhausted {
		t.Fatalf("expected ErrFreeTierExhausted, got: %v", err)
	}
}

func TestCheckAccess_ProTierExhausted(t *testing.T) {
	am := NewAccessManager(DefaultConfig())
	am.now = fixedTime

	_ = am.SetUserTier("pro-user", domain.AccessTierPro)

	// Use up pro tier (10000 inferences)
	for i := 0; i < 10000; i++ {
		am.RecordInference("pro-user", 100)
	}

	err := am.CheckAccess("pro-user")
	if err != domain.ErrQuotaExceeded {
		t.Fatalf("expected ErrQuotaExceeded, got: %v", err)
	}
}

func TestCheckAccess_EnterpriseTierUnlimited(t *testing.T) {
	am := NewAccessManager(DefaultConfig())
	am.now = fixedTime

	_ = am.SetUserTier("enterprise-user", domain.AccessTierEnterprise)

	// Use a huge number — enterprise is unlimited
	for i := 0; i < 100; i++ {
		am.RecordInference("enterprise-user", 10000)
	}

	if err := am.CheckAccess("enterprise-user"); err != nil {
		t.Fatalf("enterprise should be unlimited, got: %v", err)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// RecordInference Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestRecordInference(t *testing.T) {
	am := NewAccessManager(DefaultConfig())
	am.now = fixedTime

	am.RecordInference("user-1", 500)
	am.RecordInference("user-1", 300)

	usage := am.GetUsage("user-1")
	if usage.InferencesToday != 2 {
		t.Fatalf("expected 2 inferences, got %d", usage.InferencesToday)
	}
	if usage.TokensToday != 800 {
		t.Fatalf("expected 800 tokens, got %d", usage.TokensToday)
	}
}

func TestRecordInference_AggregateStats(t *testing.T) {
	am := NewAccessManager(DefaultConfig())
	am.now = fixedTime

	am.RecordInference("free-user", 100)
	_ = am.SetUserTier("pro-user", domain.AccessTierPro)
	am.RecordInference("pro-user", 200)

	stats := am.GetStats()
	if stats.TotalFreeInferences != 1 {
		t.Fatalf("expected 1 free inference, got %d", stats.TotalFreeInferences)
	}
	if stats.TotalProInferences != 1 {
		t.Fatalf("expected 1 pro inference, got %d", stats.TotalProInferences)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Remaining Quota Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestRemainingQuota(t *testing.T) {
	tests := []struct {
		name       string
		tier       domain.AccessTier
		used       int
		wantRemain int64
	}{
		{"free tier full quota", domain.AccessTierFree, 0, 100},
		{"free tier half used", domain.AccessTierFree, 50, 50},
		{"free tier fully used", domain.AccessTierFree, 100, 0},
		{"enterprise unlimited", domain.AccessTierEnterprise, 1000, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			am := NewAccessManager(DefaultConfig())
			am.now = fixedTime

			if tt.tier != domain.AccessTierFree {
				_ = am.SetUserTier("user", tt.tier)
			}
			for i := 0; i < tt.used; i++ {
				am.RecordInference("user", 100)
			}

			remaining := am.RemainingQuota("user")
			if remaining != tt.wantRemain {
				t.Errorf("RemainingQuota = %d, want %d", remaining, tt.wantRemain)
			}
		})
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Tier Management Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestSetUserTier(t *testing.T) {
	am := NewAccessManager(DefaultConfig())
	am.now = fixedTime

	if err := am.SetUserTier("user-1", domain.AccessTierPro); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	usage := am.GetUsage("user-1")
	if usage.Tier != domain.AccessTierPro {
		t.Fatalf("expected pro tier, got %s", usage.Tier)
	}
}

func TestSetUserTier_InvalidTier(t *testing.T) {
	am := NewAccessManager(DefaultConfig())
	if err := am.SetUserTier("user-1", "invalid"); err == nil {
		t.Fatal("expected error for invalid tier")
	}
}

func TestSetUserTier_UpdateExisting(t *testing.T) {
	am := NewAccessManager(DefaultConfig())
	am.now = fixedTime

	am.RecordInference("user-1", 100) // Creates as free tier
	_ = am.SetUserTier("user-1", domain.AccessTierPro)

	usage := am.GetUsage("user-1")
	if usage.Tier != domain.AccessTierPro {
		t.Fatalf("expected pro after upgrade, got %s", usage.Tier)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Education Verification Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestVerifyEducation_ValidDomain(t *testing.T) {
	am := NewAccessManager(DefaultConfig())
	am.now = fixedTime

	err := am.VerifyEducation("student-1", "MIT", "alice@mit.edu")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !am.IsEducationVerified("student-1") {
		t.Fatal("expected education verified")
	}
}

func TestVerifyEducation_InvalidDomain(t *testing.T) {
	am := NewAccessManager(DefaultConfig())
	am.now = fixedTime

	err := am.VerifyEducation("student-1", "FakeU", "alice@gmail.com")
	if err != domain.ErrEduTierUnverified {
		t.Fatalf("expected ErrEduTierUnverified, got: %v", err)
	}
}

func TestIsEducationVerified_NotVerified(t *testing.T) {
	am := NewAccessManager(DefaultConfig())
	am.now = fixedTime

	if am.IsEducationVerified("unknown-user") {
		t.Fatal("expected not verified for unknown user")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Daily Reset Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestResetDailyQuotas(t *testing.T) {
	am := NewAccessManager(DefaultConfig())
	am.now = fixedTime

	// Use some quota
	am.RecordInference("user-1", 100)
	am.RecordInference("user-1", 200)

	// Reset
	am.ResetDailyQuotas()

	usage := am.GetUsage("user-1")
	if usage.InferencesToday != 0 {
		t.Fatalf("expected 0 inferences after reset, got %d", usage.InferencesToday)
	}
	if usage.TokensToday != 0 {
		t.Fatalf("expected 0 tokens after reset, got %d", usage.TokensToday)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Statistics Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestGetStats_TierCounts(t *testing.T) {
	am := NewAccessManager(DefaultConfig())
	am.now = fixedTime

	// Create users on different tiers
	am.RecordInference("free-1", 100)
	am.RecordInference("free-2", 100)
	_ = am.SetUserTier("pro-1", domain.AccessTierPro)
	am.RecordInference("pro-1", 200)
	_ = am.SetUserTier("ent-1", domain.AccessTierEnterprise)
	am.RecordInference("ent-1", 300)

	stats := am.GetStats()
	if stats.TotalUsers != 4 {
		t.Fatalf("expected 4 total users, got %d", stats.TotalUsers)
	}
	if stats.FreeUsers != 2 {
		t.Fatalf("expected 2 free users, got %d", stats.FreeUsers)
	}
	if stats.ProUsers != 1 {
		t.Fatalf("expected 1 pro user, got %d", stats.ProUsers)
	}
	if stats.EnterpriseUsers != 1 {
		t.Fatalf("expected 1 enterprise user, got %d", stats.EnterpriseUsers)
	}
}
