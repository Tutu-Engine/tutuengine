package engagement

import (
	"time"

	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/infra/sqlite"
)

// AchievementService manages the 50+ achievement system.
// Architecture Part XIII: 5 categories, stat-based predicates.
// Each achievement checked against a UserStats snapshot.
type AchievementService struct {
	db          *sqlite.DB
	definitions []domain.AchievementDef
}

// NewAchievementService creates an achievement service with all definitions.
func NewAchievementService(db *sqlite.DB) *AchievementService {
	return &AchievementService{
		db:          db,
		definitions: AllAchievements(),
	}
}

// CheckAndUnlock evaluates all achievements against current stats.
// Returns newly unlocked achievements (idempotent — already-unlocked are skipped).
func (a *AchievementService) CheckAndUnlock(stats domain.UserStats) ([]domain.AchievementDef, error) {
	var newlyUnlocked []domain.AchievementDef

	for _, def := range a.definitions {
		// Skip if already unlocked
		unlocked, err := a.db.IsAchievementUnlocked(def.ID)
		if err != nil {
			return nil, err
		}
		if unlocked {
			continue
		}

		// Check predicate
		if def.Predicate != nil && def.Predicate(stats) {
			isNew, err := a.db.UnlockAchievement(def.ID, time.Now())
			if err != nil {
				return nil, err
			}
			if isNew {
				newlyUnlocked = append(newlyUnlocked, def)
			}
		}
	}

	return newlyUnlocked, nil
}

// ListUnlocked returns all achievements the user has earned.
func (a *AchievementService) ListUnlocked() ([]domain.UnlockedAchievement, error) {
	return a.db.ListUnlockedAchievements()
}

// UnlockedCount returns how many achievements are unlocked.
func (a *AchievementService) UnlockedCount() (int, error) {
	return a.db.UnlockedAchievementCount()
}

// TotalCount returns the total number of defined achievements.
func (a *AchievementService) TotalCount() int {
	return len(a.definitions)
}

// Definitions returns all achievement definitions (for display).
func (a *AchievementService) Definitions() []domain.AchievementDef {
	return a.definitions
}

// ─── Achievement Definitions (Architecture Part XIII) ───────────────────────
// 25 achievements across 5 categories. Each has a stat-based predicate.

// AllAchievements returns the full achievement catalog.
func AllAchievements() []domain.AchievementDef {
	return []domain.AchievementDef{
		// ── Getting Started (5) ────────────────────────────────────────
		{
			ID: "first_run", Name: "First Contact", Category: domain.CatGettingStarted,
			Icon: "🎯", RewardXP: 50, RewardCr: 10,
			Predicate: func(s domain.UserStats) bool { return s.TotalInferences > 0 },
		},
		{
			ID: "first_pull", Name: "Collector", Category: domain.CatGettingStarted,
			Icon: "📦", RewardXP: 30, RewardCr: 15,
			Predicate: func(s domain.UserStats) bool { return s.ModelsPulled > 0 },
		},
		{
			ID: "first_custom", Name: "Creator", Category: domain.CatGettingStarted,
			Icon: "🛠️", RewardXP: 100, RewardCr: 50,
			Predicate: func(s domain.UserStats) bool { return s.ModelsCreated > 0 },
		},
		{
			ID: "power_on", Name: "Power On", Category: domain.CatGettingStarted,
			Icon: "⚡", RewardXP: 20, RewardCr: 20,
			Predicate: func(s domain.UserStats) bool { return s.UptimeHours >= 1.0 },
		},
		{
			ID: "model_trio", Name: "Model Trio", Category: domain.CatGettingStarted,
			Icon: "🎲", RewardXP: 40, RewardCr: 25,
			Predicate: func(s domain.UserStats) bool { return s.ModelsInstalled >= 3 },
		},

		// ── Streaks (5) ────────────────────────────────────────────────
		{
			ID: "streak_7", Name: "Week Warrior", Category: domain.CatStreaks,
			Icon: "🔥", RewardXP: 200, RewardCr: 50,
			Predicate: func(s domain.UserStats) bool { return s.CurrentStreak >= 7 },
		},
		{
			ID: "streak_30", Name: "Monthly Machine", Category: domain.CatStreaks,
			Icon: "💪", RewardXP: 1000, RewardCr: 200,
			Predicate: func(s domain.UserStats) bool { return s.CurrentStreak >= 30 },
		},
		{
			ID: "streak_100", Name: "Centurion", Category: domain.CatStreaks,
			Icon: "🏛️", RewardXP: 5000, RewardCr: 1000,
			Predicate: func(s domain.UserStats) bool { return s.CurrentStreak >= 100 },
		},
		{
			ID: "streak_365", Name: "Year of Power", Category: domain.CatStreaks,
			Icon: "⭐", RewardXP: 25000, RewardCr: 5000,
			Predicate: func(s domain.UserStats) bool { return s.CurrentStreak >= 365 },
		},
		{
			ID: "streak_longest_14", Name: "Fortnight Force", Category: domain.CatStreaks,
			Icon: "📅", RewardXP: 300, RewardCr: 75,
			Predicate: func(s domain.UserStats) bool { return s.LongestStreak >= 14 },
		},

		// ── Contribution (5) ───────────────────────────────────────────
		{
			ID: "credits_100", Name: "First Paycheck", Category: domain.CatContribution,
			Icon: "💰", RewardXP: 100, RewardCr: 0,
			Predicate: func(s domain.UserStats) bool { return s.LifetimeCredits >= 100 },
		},
		{
			ID: "credits_10000", Name: "Credit Lord", Category: domain.CatContribution,
			Icon: "👑", RewardXP: 2000, RewardCr: 0,
			Predicate: func(s domain.UserStats) bool { return s.LifetimeCredits >= 10000 },
		},
		{
			ID: "overnight_earn", Name: "Sleep Earner", Category: domain.CatContribution,
			Icon: "😴", RewardXP: 150, RewardCr: 30,
			Predicate: func(s domain.UserStats) bool { return s.OvernightEarnings > 0 },
		},
		{
			ID: "tasks_1000", Name: "Task Master", Category: domain.CatContribution,
			Icon: "⚙️", RewardXP: 500, RewardCr: 100,
			Predicate: func(s domain.UserStats) bool { return s.TasksCompleted >= 1000 },
		},
		{
			ID: "gpu_hours_100", Name: "GPU Hero", Category: domain.CatContribution,
			Icon: "🖥️", RewardXP: 400, RewardCr: 80,
			Predicate: func(s domain.UserStats) bool { return s.GPUHours >= 100 },
		},

		// ── Social (5) ─────────────────────────────────────────────────
		{
			ID: "first_referral", Name: "Ambassador", Category: domain.CatSocial,
			Icon: "🤝", RewardXP: 300, RewardCr: 500,
			Predicate: func(s domain.UserStats) bool { return s.Referrals >= 1 },
		},
		{
			ID: "referrals_5", Name: "Recruiter", Category: domain.CatSocial,
			Icon: "📢", RewardXP: 800, RewardCr: 1000,
			Predicate: func(s domain.UserStats) bool { return s.Referrals >= 5 },
		},
		{
			ID: "referrals_25", Name: "Evangelist", Category: domain.CatSocial,
			Icon: "🌟", RewardXP: 2000, RewardCr: 2500,
			Predicate: func(s domain.UserStats) bool { return s.Referrals >= 25 },
		},
		{
			ID: "level_10", Name: "Rising Star", Category: domain.CatSocial,
			Icon: "🌅", RewardXP: 200, RewardCr: 50,
			Predicate: func(s domain.UserStats) bool { return s.Level >= 10 },
		},
		{
			ID: "level_50", Name: "Veteran", Category: domain.CatSocial,
			Icon: "🎖️", RewardXP: 2000, RewardCr: 500,
			Predicate: func(s domain.UserStats) bool { return s.Level >= 50 },
		},

		// ── Mastery (5) ────────────────────────────────────────────────
		{
			ID: "models_10", Name: "Model Hoarder", Category: domain.CatMastery,
			Icon: "📚", RewardXP: 500, RewardCr: 100,
			Predicate: func(s domain.UserStats) bool { return s.ModelsInstalled >= 10 },
		},
		{
			ID: "agent_first", Name: "Agent Smith", Category: domain.CatMastery,
			Icon: "🤖", RewardXP: 300, RewardCr: 50,
			Predicate: func(s domain.UserStats) bool { return s.AgentRuns >= 1 },
		},
		{
			ID: "agent_10", Name: "Agent Army", Category: domain.CatMastery,
			Icon: "🤖", RewardXP: 800, RewardCr: 200,
			Predicate: func(s domain.UserStats) bool { return s.AgentRuns >= 10 },
		},
		{
			ID: "uptime_500", Name: "Always On", Category: domain.CatMastery,
			Icon: "🏠", RewardXP: 1000, RewardCr: 300,
			Predicate: func(s domain.UserStats) bool { return s.UptimeHours >= 500 },
		},
		{
			ID: "level_100", Name: "TuTu Founder", Category: domain.CatMastery,
			Icon: "👑", RewardXP: 50000, RewardCr: 10000,
			Predicate: func(s domain.UserStats) bool { return s.Level >= 100 },
		},
	}
}
