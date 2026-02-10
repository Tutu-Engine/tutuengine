package engagement

import (
	"fmt"
	"math"
	"strconv"

	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/infra/sqlite"
)

// LevelService manages the XP and level system.
// Architecture Part XIII: Exponential XP curve, L1-L100.
// Progressive unlocks — ALL local models free from L1.
type LevelService struct {
	db *sqlite.DB
}

// NewLevelService creates a level service.
func NewLevelService(db *sqlite.DB) *LevelService {
	return &LevelService{db: db}
}

// XPForLevel returns the cumulative XP required to reach a given level.
// Uses an exponential curve: 100 * 1.2^(level-1) for level >= 2.
func XPForLevel(level int) int64 {
	if level <= 1 {
		return 0
	}
	return int64(100 * math.Pow(1.2, float64(level-1)))
}

// LevelForXP returns the level for a given XP amount.
// Iterates upward until cumulative XP exceeds the target.
func LevelForXP(xp int64) int {
	level := 1
	for level < 100 {
		required := XPForLevel(level + 1)
		if xp < required {
			return level
		}
		level++
	}
	return 100 // Cap at 100
}

// CurrentLevel returns the user's current level and XP.
func (l *LevelService) CurrentLevel() (domain.UserLevel, error) {
	var ul domain.UserLevel

	xpStr, err := l.db.GetEngagement("xp")
	if err != nil {
		return ul, fmt.Errorf("get xp: %w", err)
	}
	if xpStr != "" {
		ul.CurrentXP, _ = strconv.ParseInt(xpStr, 10, 64)
	}

	ul.Level = LevelForXP(ul.CurrentXP)
	return ul, nil
}

// AddXP adds experience points and returns (newLevel, leveledUp, error).
// If the user gained a level, leveledUp is true.
func (l *LevelService) AddXP(amount int64, source domain.XPSource) (int, bool, error) {
	if amount <= 0 {
		return 0, false, fmt.Errorf("xp amount must be positive, got %d", amount)
	}

	current, err := l.CurrentLevel()
	if err != nil {
		return 0, false, err
	}

	oldLevel := current.Level
	newXP := current.CurrentXP + amount

	if err := l.db.SetEngagement("xp", strconv.FormatInt(newXP, 10)); err != nil {
		return 0, false, fmt.Errorf("save xp: %w", err)
	}

	newLevel := LevelForXP(newXP)

	// Persist level for quick reads
	if err := l.db.SetEngagement("level", strconv.Itoa(newLevel)); err != nil {
		return 0, false, fmt.Errorf("save level: %w", err)
	}

	return newLevel, newLevel > oldLevel, nil
}

// XPToNextLevel returns XP remaining until the next level.
func (l *LevelService) XPToNextLevel() (int64, error) {
	current, err := l.CurrentLevel()
	if err != nil {
		return 0, err
	}
	if current.Level >= 100 {
		return 0, nil // Max level
	}
	needed := XPForLevel(current.Level + 1)
	remaining := needed - current.CurrentXP
	if remaining < 0 {
		remaining = 0
	}
	return remaining, nil
}

// ProgressPct returns progress percentage toward next level (0.0–100.0).
func (l *LevelService) ProgressPct() (float64, error) {
	current, err := l.CurrentLevel()
	if err != nil {
		return 0, err
	}
	if current.Level >= 100 {
		return 100.0, nil
	}
	thisLevel := XPForLevel(current.Level)
	nextLevel := XPForLevel(current.Level + 1)
	span := nextLevel - thisLevel
	if span <= 0 {
		return 100.0, nil
	}
	progress := float64(current.CurrentXP-thisLevel) / float64(span) * 100.0
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	return progress, nil
}

// UnlocksForLevel returns the features unlocked at a specific level.
// Architecture Part XIII v3.0: ALL local models free from L1.
func UnlocksForLevel(level int) []string {
	unlocks := map[int][]string{
		1:   {"All local models", "Chat", "API"},
		5:   {"RAG pipeline"},
		10:  {"Custom agents"},
		15:  {"Code generation tools"},
		20:  {"Distributed inference"},
		25:  {"Automation engine"},
		30:  {"Distributed fine-tuning"},
		40:  {"API access (build apps)"},
		50:  {"Priority distributed tasks", "70B+ models"},
		60:  {"Enterprise API features"},
		75:  {"Proprietary model access (Claude, GPT)"},
		100: {"TuTu Founder badge", "Permanent 2× credit multiplier"},
	}
	if u, ok := unlocks[level]; ok {
		return u
	}
	return nil
}
