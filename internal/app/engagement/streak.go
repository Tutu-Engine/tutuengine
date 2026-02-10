// Package engagement implements the TuTu engagement engine.
// Architecture Part XIII: Streaks, levels, achievements, quests, notifications.
// Design rule: Real value, not dark patterns.
package engagement

import (
	"fmt"
	"strconv"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/infra/sqlite"
)

// StreakService manages contribution streaks.
// A "day" counts if the node contributed ≥1 hour of compute.
// Bonus: +5% per consecutive day, capped at +50% (10-day max).
// v3.0: Streaks break SILENTLY — no "streak at risk!" notifications.
type StreakService struct {
	db *sqlite.DB
}

// NewStreakService creates a streak service.
func NewStreakService(db *sqlite.DB) *StreakService {
	return &StreakService{db: db}
}

// CurrentStreak loads the current streak state from the database.
func (s *StreakService) CurrentStreak() (domain.Streak, error) {
	var streak domain.Streak

	days, err := s.db.GetEngagement("streak_current")
	if err != nil {
		return streak, fmt.Errorf("get streak_current: %w", err)
	}
	if days != "" {
		streak.CurrentDays, _ = strconv.Atoi(days)
	}

	longest, err := s.db.GetEngagement("streak_longest")
	if err != nil {
		return streak, fmt.Errorf("get streak_longest: %w", err)
	}
	if longest != "" {
		streak.LongestDays, _ = strconv.Atoi(longest)
	}

	lastDate, err := s.db.GetEngagement("streak_last_date")
	if err != nil {
		return streak, fmt.Errorf("get streak_last_date: %w", err)
	}
	if lastDate != "" {
		ts, _ := strconv.ParseInt(lastDate, 10, 64)
		streak.LastDate = time.Unix(ts, 0)
	}

	freezeUsed, err := s.db.GetEngagement("streak_freeze_used")
	if err != nil {
		return streak, fmt.Errorf("get streak_freeze_used: %w", err)
	}
	streak.FreezeUsed = freezeUsed == "1"

	freezeWeek, err := s.db.GetEngagement("streak_freeze_week")
	if err != nil {
		return streak, fmt.Errorf("get streak_freeze_week: %w", err)
	}
	streak.FreezeWeekISO = freezeWeek

	return streak, nil
}

// RecordContribution records a day of contribution.
// If same day: no-op. If consecutive: extend streak.
// If gap >1 day: check for free weekly freeze, else reset silently.
func (s *StreakService) RecordContribution(day time.Time) error {
	streak, err := s.CurrentStreak()
	if err != nil {
		return err
	}

	today := day.Truncate(24 * time.Hour)

	// Same day — already counted
	if !streak.LastDate.IsZero() && today.Equal(streak.LastDate.Truncate(24*time.Hour)) {
		return nil
	}

	if streak.LastDate.IsZero() {
		// First contribution ever
		streak.CurrentDays = 1
	} else {
		gap := today.Sub(streak.LastDate.Truncate(24 * time.Hour))

		switch {
		case gap <= 24*time.Hour:
			// Consecutive day — extend streak
			streak.CurrentDays++

		case gap <= 48*time.Hour:
			// Missed exactly 1 day — try freeze
			currentWeek := isoWeek(today)
			if !streak.FreezeUsed || streak.FreezeWeekISO != currentWeek {
				// Use weekly freeze: streak continues
				streak.FreezeUsed = true
				streak.FreezeWeekISO = currentWeek
				streak.CurrentDays++ // Count today
			} else {
				// Freeze already used this week — streak breaks silently
				streak.CurrentDays = 1
			}

		default:
			// Gap > 2 days — streak breaks silently (v3.0: NO notifications)
			streak.CurrentDays = 1
		}
	}

	streak.LastDate = today
	if streak.CurrentDays > streak.LongestDays {
		streak.LongestDays = streak.CurrentDays
	}

	return s.saveStreak(streak)
}

// CreditMultiplier returns the streak credit multiplier.
// +5% per day, capped at +50%.
func (s *StreakService) CreditMultiplier() float64 {
	streak, _ := s.CurrentStreak()
	return streak.Multiplier()
}

// saveStreak persists streak state to the engagement KV table.
func (s *StreakService) saveStreak(streak domain.Streak) error {
	pairs := map[string]string{
		"streak_current":     strconv.Itoa(streak.CurrentDays),
		"streak_longest":     strconv.Itoa(streak.LongestDays),
		"streak_last_date":   strconv.FormatInt(streak.LastDate.Unix(), 10),
		"streak_freeze_used": boolStr(streak.FreezeUsed),
		"streak_freeze_week": streak.FreezeWeekISO,
	}
	for k, v := range pairs {
		if err := s.db.SetEngagement(k, v); err != nil {
			return fmt.Errorf("save %s: %w", k, err)
		}
	}
	return nil
}

// isoWeek returns "YYYY-Www" for the given time.
func isoWeek(t time.Time) string {
	year, week := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
