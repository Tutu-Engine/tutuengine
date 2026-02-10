package engagement

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/infra/sqlite"
)

// QuestService manages weekly quests.
// Architecture Part XIII v3.0: weekly only (no daily — casual).
// 3 new quests generated every Monday, expire the following Monday.
type QuestService struct {
	db *sqlite.DB
}

// NewQuestService creates a quest service.
func NewQuestService(db *sqlite.DB) *QuestService {
	return &QuestService{db: db}
}

// questPool is the set of possible quest templates.
var questPool = []domain.QuestTemplate{
	{Type: domain.QuestInference, Target: 50, Description: "Run 50 inferences", RewardXP: 200, RewardCr: 30},
	{Type: domain.QuestInference, Target: 100, Description: "Run 100 inferences", RewardXP: 350, RewardCr: 60},
	{Type: domain.QuestUptime, Target: 20, Description: "20 hours network uptime", RewardXP: 200, RewardCr: 40},
	{Type: domain.QuestUptime, Target: 40, Description: "40 hours network uptime", RewardXP: 300, RewardCr: 50},
	{Type: domain.QuestModels, Target: 2, Description: "Try 2 new models", RewardXP: 150, RewardCr: 20},
	{Type: domain.QuestModels, Target: 5, Description: "Try 5 new models", RewardXP: 300, RewardCr: 50},
	{Type: domain.QuestAgent, Target: 3, Description: "Run 3 agent workflows", RewardXP: 250, RewardCr: 40},
	{Type: domain.QuestStreak, Target: 7, Description: "Maintain 7-day streak", RewardXP: 200, RewardCr: 30},
}

// GenerateWeeklyQuests creates 3 random quests for the current week.
// Quests expire next Monday at 00:00 UTC.
// If quests already exist for this week, returns existing ones.
func (q *QuestService) GenerateWeeklyQuests() ([]domain.Quest, error) {
	// Check if we already have active quests
	active, err := q.db.ListActiveQuests()
	if err != nil {
		return nil, err
	}
	if len(active) > 0 {
		return active, nil // Already generated for this week
	}

	return q.GenerateWeeklyQuestsAt(time.Now())
}

// GenerateWeeklyQuestsAt creates 3 random quests expiring at next Monday.
// Accepts a time parameter for testability.
func (q *QuestService) GenerateWeeklyQuestsAt(now time.Time) ([]domain.Quest, error) {
	expiry := nextMonday(now)

	// Pick 3 unique templates (no duplicate types)
	selected := pickUniqueQuests(questPool, 3, now.UnixNano())

	var quests []domain.Quest
	for i, tmpl := range selected {
		quest := domain.Quest{
			ID:            fmt.Sprintf("quest-%s-%d-%d", tmpl.Type, expiry.Unix(), i),
			Type:          tmpl.Type,
			Description:   tmpl.Description,
			Target:        tmpl.Target,
			Progress:      0,
			RewardXP:      tmpl.RewardXP,
			RewardCredits: tmpl.RewardCr,
			ExpiresAt:     expiry,
			Completed:     false,
		}
		if err := q.db.InsertQuest(quest); err != nil {
			return nil, fmt.Errorf("insert quest: %w", err)
		}
		quests = append(quests, quest)
	}

	return quests, nil
}

// ActiveQuests returns current non-expired, non-completed quests.
func (q *QuestService) ActiveQuests() ([]domain.Quest, error) {
	return q.db.ListActiveQuests()
}

// RecordProgress increments progress for quests matching the given type.
// Returns any quests that were completed by this progress.
func (q *QuestService) RecordProgress(questType domain.QuestType, delta int) ([]domain.Quest, error) {
	active, err := q.db.ListActiveQuests()
	if err != nil {
		return nil, err
	}

	var completed []domain.Quest
	for _, quest := range active {
		if quest.Type != questType {
			continue
		}

		updated, err := q.db.UpdateQuestProgress(quest.ID, delta)
		if err != nil {
			return nil, err
		}
		if updated != nil && updated.Progress >= updated.Target && !updated.Completed {
			if err := q.db.CompleteQuest(quest.ID); err != nil {
				return nil, err
			}
			updated.Completed = true
			completed = append(completed, *updated)
		}
	}

	return completed, nil
}

// CleanupExpired removes quests that expired before now.
func (q *QuestService) CleanupExpired() (int64, error) {
	return q.db.DeleteExpiredQuests(time.Now())
}

// nextMonday returns the next Monday at 00:00 UTC after the given time.
func nextMonday(t time.Time) time.Time {
	t = t.UTC().Truncate(24 * time.Hour)
	daysUntilMonday := (8 - int(t.Weekday())) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7 // If today is Monday, next Monday
	}
	return t.AddDate(0, 0, daysUntilMonday)
}

// pickUniqueQuests selects n random templates, preferring unique types.
func pickUniqueQuests(pool []domain.QuestTemplate, n int, seed int64) []domain.QuestTemplate {
	r := rand.New(rand.NewSource(seed))

	// Shuffle a copy
	shuffled := make([]domain.QuestTemplate, len(pool))
	copy(shuffled, pool)
	r.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	// Pick unique types first
	seen := make(map[domain.QuestType]bool)
	var result []domain.QuestTemplate
	for _, tmpl := range shuffled {
		if len(result) >= n {
			break
		}
		if !seen[tmpl.Type] {
			seen[tmpl.Type] = true
			result = append(result, tmpl)
		}
	}

	// If not enough unique types, fill with any
	for _, tmpl := range shuffled {
		if len(result) >= n {
			break
		}
		// Check if already in result by pointer (type+target combo)
		dup := false
		for _, r := range result {
			if r.Type == tmpl.Type && r.Target == tmpl.Target {
				dup = true
				break
			}
		}
		if !dup {
			result = append(result, tmpl)
		}
	}

	return result
}
