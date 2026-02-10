// Package domain — Phase 2 engagement types.
// The engagement engine drives user retention through streaks, levels,
// achievements, quests, and smart notifications.
// Architecture Part XIII: Real value, not dark patterns.
package domain

import "time"

// ─── Streak Types ───────────────────────────────────────────────────────────

// Streak tracks consecutive days of contribution.
// Bonus: +5% per day, capped at +50% (10-day streak).
type Streak struct {
	CurrentDays   int       `json:"current_days"`
	LongestDays   int       `json:"longest_days"`
	LastDate      time.Time `json:"last_date"`
	FreezeUsed    bool      `json:"freeze_used"`    // 1 free freeze per week
	FreezeWeekISO string    `json:"freeze_week_iso"` // "2025-W28" — tracks when freeze was used
}

// Multiplier returns the credit multiplier for this streak.
// +5% per consecutive day, capped at +50%.
func (s Streak) Multiplier() float64 {
	bonus := float64(s.CurrentDays) * 0.05
	if bonus > 0.50 {
		bonus = 0.50
	}
	return 1.0 + bonus
}

// ─── Level / XP Types ───────────────────────────────────────────────────────

// UserLevel represents the user's current level and XP progress.
type UserLevel struct {
	Level     int   `json:"level"`
	CurrentXP int64 `json:"current_xp"`
}

// XPSource categorizes how XP was earned.
type XPSource string

const (
	XPTaskCompleted   XPSource = "TASK_COMPLETED"
	XPHourContributed XPSource = "HOUR_CONTRIBUTED"
	XPAchievement     XPSource = "ACHIEVEMENT"
	XPReferral        XPSource = "REFERRAL"
	XPQuestCompleted  XPSource = "QUEST_COMPLETED"
)

// ─── Achievement Types ──────────────────────────────────────────────────────

// AchievementCategory groups achievements by theme.
type AchievementCategory string

const (
	CatGettingStarted AchievementCategory = "getting_started"
	CatStreaks        AchievementCategory = "streaks"
	CatContribution   AchievementCategory = "contribution"
	CatSocial         AchievementCategory = "social"
	CatMastery        AchievementCategory = "mastery"
)

// AchievementDef defines a single achievement's requirements.
type AchievementDef struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Category   AchievementCategory `json:"category"`
	Icon       string              `json:"icon"`
	RewardXP   int64               `json:"reward_xp"`
	RewardCr   int64               `json:"reward_cr"` // 0 if no credit reward
	Predicate  func(UserStats) bool `json:"-"`         // Check function (not serialized)
}

// UnlockedAchievement records when an achievement was earned.
type UnlockedAchievement struct {
	ID         string    `json:"id"`
	UnlockedAt time.Time `json:"unlocked_at"`
	Notified   bool      `json:"notified"`
}

// UserStats is a snapshot of user state fed to achievement predicates.
type UserStats struct {
	TotalInferences  int64   `json:"total_inferences"`
	ModelsPulled     int     `json:"models_pulled"`
	ModelsCreated    int     `json:"models_created"`
	ModelsInstalled  int     `json:"models_installed"`
	CurrentStreak    int     `json:"current_streak"`
	LongestStreak    int     `json:"longest_streak"`
	LifetimeCredits  int64   `json:"lifetime_credits"`
	OvernightEarnings int64  `json:"overnight_earnings"` // Credits earned 00:00–06:00
	Referrals        int     `json:"referrals"`
	AgentRuns        int     `json:"agent_runs"`
	TasksCompleted   int64   `json:"tasks_completed"`
	GPUHours         float64 `json:"gpu_hours"`
	UptimeHours      float64 `json:"uptime_hours"`
	Level            int     `json:"level"`
}

// ─── Quest Types ────────────────────────────────────────────────────────────

// QuestType categorizes the kind of quest.
type QuestType string

const (
	QuestInference QuestType = "inference"
	QuestUptime    QuestType = "uptime"
	QuestModels    QuestType = "models"
	QuestAgent     QuestType = "agent"
	QuestStreak    QuestType = "streak"
)

// Quest represents a weekly challenge with progress tracking.
type Quest struct {
	ID            string    `json:"id"`
	Type          QuestType `json:"type"`
	Description   string    `json:"description"`
	Target        int       `json:"target"`
	Progress      int       `json:"progress"`
	RewardXP      int64     `json:"reward_xp"`
	RewardCredits int64     `json:"reward_credits"`
	ExpiresAt     time.Time `json:"expires_at"`
	Completed     bool      `json:"completed"`
}

// IsExpired returns true if the quest deadline has passed.
func (q Quest) IsExpired() bool {
	return time.Now().After(q.ExpiresAt)
}

// ProgressPct returns completion percentage (0-100).
func (q Quest) ProgressPct() float64 {
	if q.Target <= 0 {
		return 100.0
	}
	pct := float64(q.Progress) / float64(q.Target) * 100.0
	if pct > 100.0 {
		pct = 100.0
	}
	return pct
}

// QuestTemplate defines the pool of possible quests.
type QuestTemplate struct {
	Type        QuestType `json:"type"`
	Target      int       `json:"target"`
	Description string    `json:"description"`
	RewardXP    int64     `json:"reward_xp"`
	RewardCr    int64     `json:"reward_cr"`
}

// ─── Notification Types ─────────────────────────────────────────────────────

// NotificationType categorizes notifications.
type NotificationType string

const (
	NotifyAchievement    NotificationType = "achievement"
	NotifyLevelUp        NotificationType = "level_up"
	NotifyDailySummary   NotificationType = "daily_summary"
	NotifyQuestComplete  NotificationType = "quest_complete"
	NotifyMilestone      NotificationType = "milestone"
)

// Notification is a user-facing message.
type Notification struct {
	ID        int64            `json:"id"`
	Type      NotificationType `json:"type"`
	Title     string           `json:"title"`
	Body      string           `json:"body"`
	CreatedAt time.Time        `json:"created_at"`
	Shown     bool             `json:"shown"`
}

// NotificationPolicy governs how often notifications are sent.
// Architecture Part XIII v3.0: max 1/day, quiet hours respected.
type NotificationPolicy struct {
	MaxPerDay  int    `json:"max_per_day"`  // Default: 1
	QuietStart string `json:"quiet_start"`  // "22:00"
	QuietEnd   string `json:"quiet_end"`    // "08:00"
}

// DefaultNotificationPolicy returns the v3.0 policy.
func DefaultNotificationPolicy() NotificationPolicy {
	return NotificationPolicy{
		MaxPerDay:  1,
		QuietStart: "22:00",
		QuietEnd:   "08:00",
	}
}
