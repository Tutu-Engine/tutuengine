package engagement_test

import (
	"os"
	"testing"
	"time"

	"github.com/tutu-network/tutu/internal/app/engagement"
	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/infra/sqlite"
)

// testDB creates a temporary SQLite database for testing.
func testDB(t *testing.T) *sqlite.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(dir)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ═══════════════════════════════════════════════════════════════════════════
// Streak Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestStreak_FirstContribution(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewStreakService(db)

	day := time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC)
	if err := svc.RecordContribution(day); err != nil {
		t.Fatalf("record: %v", err)
	}

	streak, err := svc.CurrentStreak()
	if err != nil {
		t.Fatalf("get streak: %v", err)
	}
	if streak.CurrentDays != 1 {
		t.Errorf("expected 1 day, got %d", streak.CurrentDays)
	}
	if streak.LongestDays != 1 {
		t.Errorf("expected longest 1, got %d", streak.LongestDays)
	}
}

func TestStreak_ConsecutiveDays(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewStreakService(db)

	base := time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		day := base.AddDate(0, 0, i)
		if err := svc.RecordContribution(day); err != nil {
			t.Fatalf("record day %d: %v", i, err)
		}
	}

	streak, err := svc.CurrentStreak()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if streak.CurrentDays != 5 {
		t.Errorf("expected 5 consecutive, got %d", streak.CurrentDays)
	}
}

func TestStreak_SameDayIdempotent(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewStreakService(db)

	day := time.Date(2025, 7, 1, 10, 0, 0, 0, time.UTC)
	_ = svc.RecordContribution(day)
	_ = svc.RecordContribution(day.Add(2 * time.Hour)) // Same day, different time
	_ = svc.RecordContribution(day.Add(5 * time.Hour))

	streak, _ := svc.CurrentStreak()
	if streak.CurrentDays != 1 {
		t.Errorf("expected 1 (idempotent), got %d", streak.CurrentDays)
	}
}

func TestStreak_BrokenSilently(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewStreakService(db)

	day1 := time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC)
	_ = svc.RecordContribution(day1)
	_ = svc.RecordContribution(day1.AddDate(0, 0, 1))
	_ = svc.RecordContribution(day1.AddDate(0, 0, 2))

	// Gap of 3 days — streak breaks
	_ = svc.RecordContribution(day1.AddDate(0, 0, 6))

	streak, _ := svc.CurrentStreak()
	if streak.CurrentDays != 1 {
		t.Errorf("expected streak reset to 1, got %d", streak.CurrentDays)
	}
	if streak.LongestDays != 3 {
		t.Errorf("expected longest preserved at 3, got %d", streak.LongestDays)
	}
}

func TestStreak_FreezeOnMissedDay(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewStreakService(db)

	day1 := time.Date(2025, 6, 30, 12, 0, 0, 0, time.UTC) // Monday
	_ = svc.RecordContribution(day1)
	_ = svc.RecordContribution(day1.AddDate(0, 0, 1))

	// Skip 1 day — freeze should auto-apply
	day4 := day1.AddDate(0, 0, 3)
	_ = svc.RecordContribution(day4)

	streak, _ := svc.CurrentStreak()
	if streak.CurrentDays != 3 {
		t.Errorf("expected freeze to preserve streak at 3, got %d", streak.CurrentDays)
	}
	if !streak.FreezeUsed {
		t.Error("expected freeze to be marked as used")
	}
}

func TestStreak_FreezeOnlyOncePerWeek(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewStreakService(db)

	day1 := time.Date(2025, 6, 30, 12, 0, 0, 0, time.UTC) // Monday
	_ = svc.RecordContribution(day1)
	_ = svc.RecordContribution(day1.AddDate(0, 0, 1)) // Tue

	// Miss Wed, freeze applies on Thu
	_ = svc.RecordContribution(day1.AddDate(0, 0, 3)) // Thu (freeze used)

	// Miss Fri, try to freeze again on Sat — same week, should reset
	_ = svc.RecordContribution(day1.AddDate(0, 0, 5)) // Sat

	streak, _ := svc.CurrentStreak()
	// Second miss in same week — streak should break
	if streak.CurrentDays != 1 {
		t.Errorf("expected reset (freeze already used), got %d", streak.CurrentDays)
	}
}

func TestStreak_CreditMultiplier(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewStreakService(db)

	base := time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		_ = svc.RecordContribution(base.AddDate(0, 0, i))
	}

	mult := svc.CreditMultiplier()
	expected := 1.50 // 10 days * 0.05 = 0.50, capped
	if mult != expected {
		t.Errorf("expected multiplier %.2f, got %.2f", expected, mult)
	}
}

func TestStreak_MultiplierCappedAt50Pct(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewStreakService(db)

	base := time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ { // 20 days > cap
		_ = svc.RecordContribution(base.AddDate(0, 0, i))
	}

	mult := svc.CreditMultiplier()
	if mult != 1.50 {
		t.Errorf("expected cap at 1.50, got %.2f", mult)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Level & XP Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestXPForLevel_Zero(t *testing.T) {
	if xp := engagement.XPForLevel(0); xp != 0 {
		t.Errorf("level 0 should need 0 XP, got %d", xp)
	}
	if xp := engagement.XPForLevel(1); xp != 0 {
		t.Errorf("level 1 should need 0 XP, got %d", xp)
	}
}

func TestXPForLevel_Exponential(t *testing.T) {
	// Level 2 = 100 * 1.2^1 = 120
	xp2 := engagement.XPForLevel(2)
	if xp2 != 120 {
		t.Errorf("level 2 expected 120, got %d", xp2)
	}

	// Each level requires more than the last
	prev := engagement.XPForLevel(2)
	for lvl := 3; lvl <= 20; lvl++ {
		xp := engagement.XPForLevel(lvl)
		if xp <= prev {
			t.Errorf("level %d XP (%d) not greater than level %d (%d)", lvl, xp, lvl-1, prev)
		}
		prev = xp
	}
}

func TestLevelForXP(t *testing.T) {
	// XP thresholds: L1=0, L2=120, L3=144, L4=172, ...
	tests := []struct {
		xp   int64
		want int
	}{
		{0, 1},
		{100, 1},
		{119, 1},
		{120, 2},   // Exactly L2 threshold
		{143, 2},   // Just below L3
		{144, 3},   // Exactly L3 threshold
		{500, 9},   // Between L9 (429) and L10 (515)
	}
	for _, tt := range tests {
		got := engagement.LevelForXP(tt.xp)
		if got != tt.want {
			t.Errorf("LevelForXP(%d) = %d, want %d", tt.xp, got, tt.want)
		}
	}
}

func TestLevel_AddXP(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewLevelService(db)

	level, leveledUp, err := svc.AddXP(150, domain.XPTaskCompleted)
	if err != nil {
		t.Fatalf("addXP: %v", err)
	}
	if level < 1 {
		t.Errorf("expected level >= 1, got %d", level)
	}

	// At 150 XP: level should be 3 (L2=120, L3=144, 150>=144)
	if level != 3 {
		t.Errorf("expected level 3 at 150 XP, got %d", level)
	}
	if !leveledUp {
		t.Error("expected leveledUp = true (went from 1 to 3)")
	}
}

func TestLevel_AddXP_NoLevelUp(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewLevelService(db)

	// Add 50 XP — not enough to reach L2 (needs 120)
	level, leveledUp, err := svc.AddXP(50, domain.XPTaskCompleted)
	if err != nil {
		t.Fatalf("addXP: %v", err)
	}
	if level != 1 {
		t.Errorf("expected level 1, got %d", level)
	}
	if leveledUp {
		t.Error("expected no level up with only 50 XP")
	}
}

func TestLevel_CurrentLevel(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewLevelService(db)

	// Fresh — should be level 1 with 0 XP
	ul, err := svc.CurrentLevel()
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if ul.Level != 1 {
		t.Errorf("fresh level expected 1, got %d", ul.Level)
	}
	if ul.CurrentXP != 0 {
		t.Errorf("fresh XP expected 0, got %d", ul.CurrentXP)
	}
}

func TestLevel_ProgressPct(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewLevelService(db)

	// Add 60 XP — halfway between L1 (0) and L2 (120)
	_, _, _ = svc.AddXP(60, domain.XPTaskCompleted)

	pct, err := svc.ProgressPct()
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if pct != 50.0 {
		t.Errorf("expected 50%%, got %.1f%%", pct)
	}
}

func TestLevel_XPToNextLevel(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewLevelService(db)

	remaining, err := svc.XPToNextLevel()
	if err != nil {
		t.Fatalf("xp to next: %v", err)
	}
	// At L1 with 0 XP, need 120 for L2
	if remaining != 120 {
		t.Errorf("expected 120 remaining, got %d", remaining)
	}
}

func TestUnlocksForLevel(t *testing.T) {
	unlocks := engagement.UnlocksForLevel(1)
	if len(unlocks) == 0 {
		t.Error("L1 should have unlocks")
	}

	unlocks50 := engagement.UnlocksForLevel(50)
	if len(unlocks50) == 0 {
		t.Error("L50 should have unlocks")
	}

	// Non-milestone level should return nil
	if u := engagement.UnlocksForLevel(3); u != nil {
		t.Errorf("level 3 shouldn't have unlocks, got %v", u)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Achievement Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestAchievement_CheckAndUnlock(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewAchievementService(db)

	stats := domain.UserStats{
		TotalInferences: 1, // Triggers "first_run"
		ModelsPulled:    1, // Triggers "first_pull"
	}

	unlocked, err := svc.CheckAndUnlock(stats)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(unlocked) < 2 {
		t.Errorf("expected at least 2 unlocked (first_run, first_pull), got %d", len(unlocked))
	}

	// Verify "first_run" is in the list
	found := false
	for _, a := range unlocked {
		if a.ID == "first_run" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'first_run' in unlocked achievements")
	}
}

func TestAchievement_Idempotent(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewAchievementService(db)

	stats := domain.UserStats{TotalInferences: 1}
	unlocked1, _ := svc.CheckAndUnlock(stats)
	unlocked2, _ := svc.CheckAndUnlock(stats)

	if len(unlocked2) != 0 {
		t.Errorf("second check should return 0 new, got %d", len(unlocked2))
	}
	_ = unlocked1
}

func TestAchievement_StreakAchievement(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewAchievementService(db)

	stats := domain.UserStats{CurrentStreak: 7}
	unlocked, _ := svc.CheckAndUnlock(stats)

	found := false
	for _, a := range unlocked {
		if a.ID == "streak_7" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'streak_7' achievement at 7-day streak")
	}
}

func TestAchievement_TotalCount(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewAchievementService(db)

	total := svc.TotalCount()
	if total != 25 {
		t.Errorf("expected 25 achievements, got %d", total)
	}
}

func TestAchievement_ListUnlocked(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewAchievementService(db)

	// Unlock some
	_, _ = svc.CheckAndUnlock(domain.UserStats{TotalInferences: 1, UptimeHours: 2})

	list, err := svc.ListUnlocked()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) < 1 {
		t.Error("expected at least 1 unlocked achievement")
	}
}

func TestAchievement_UnlockedCount(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewAchievementService(db)

	_, _ = svc.CheckAndUnlock(domain.UserStats{TotalInferences: 1})
	count, _ := svc.UnlockedCount()
	if count < 1 {
		t.Errorf("expected count >= 1, got %d", count)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Quest Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestQuest_GenerateWeeklyQuests(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewQuestService(db)

	now := time.Now().Add(24 * time.Hour) // Tomorrow — ensures quests are in the future
	quests, err := svc.GenerateWeeklyQuestsAt(now)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(quests) != 3 {
		t.Errorf("expected 3 quests, got %d", len(quests))
	}

	// All should expire next Monday
	for _, q := range quests {
		if q.ExpiresAt.Weekday() != time.Monday {
			t.Errorf("quest should expire on Monday, expires on %s", q.ExpiresAt.Weekday())
		}
	}
}

func TestQuest_GenerateIdempotent(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewQuestService(db)

	now := time.Now().Add(24 * time.Hour)
	q1, _ := svc.GenerateWeeklyQuestsAt(now)

	// Second call should return existing
	q2, err := svc.GenerateWeeklyQuests()
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if len(q2) != len(q1) {
		t.Errorf("expected same quests, got %d vs %d", len(q2), len(q1))
	}
}

func TestQuest_RecordProgress(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewQuestService(db)

	now := time.Now().Add(24 * time.Hour) // Tomorrow — ensures quests are in the future
	quests, _ := svc.GenerateWeeklyQuestsAt(now)

	// Find any quest and progress it
	if len(quests) == 0 {
		t.Fatal("no quests generated")
	}

	qType := quests[0].Type
	target := quests[0].Target

	// Record partial progress
	completed, err := svc.RecordProgress(qType, target-1)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if len(completed) != 0 {
		t.Error("should not be completed yet")
	}

	// Complete it
	completed, err = svc.RecordProgress(qType, 1)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(completed) != 1 {
		t.Errorf("expected 1 completed quest, got %d", len(completed))
	}
}

func TestQuest_ActiveQuests(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewQuestService(db)

	now := time.Now().Add(24 * time.Hour) // Tomorrow — ensures expiry is well in the future
	generated, _ := svc.GenerateWeeklyQuestsAt(now)
	if len(generated) != 3 {
		t.Fatalf("expected 3 generated, got %d", len(generated))
	}

	// Check expiry is in the future
	for _, q := range generated {
		if q.ExpiresAt.Before(time.Now()) {
			t.Fatalf("quest %s expires in past: %v (now: %v)", q.ID, q.ExpiresAt, time.Now())
		}
	}

	active, err := svc.ActiveQuests()
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if len(active) != 3 {
		t.Errorf("expected 3 active, got %d", len(active))
	}
}

func TestQuest_ProgressPct(t *testing.T) {
	q := domain.Quest{Target: 100, Progress: 75}
	pct := q.ProgressPct()
	if pct != 75.0 {
		t.Errorf("expected 75%%, got %.1f%%", pct)
	}
}

func TestQuest_IsExpired(t *testing.T) {
	past := domain.Quest{ExpiresAt: time.Now().Add(-1 * time.Hour)}
	if !past.IsExpired() {
		t.Error("should be expired")
	}

	future := domain.Quest{ExpiresAt: time.Now().Add(1 * time.Hour)}
	if future.IsExpired() {
		t.Error("should not be expired")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Notification Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestNotification_Create(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewNotificationServiceWithPolicy(db, domain.NotificationPolicy{
		MaxPerDay:  1,
		QuietStart: "22:00",
		QuietEnd:   "08:00",
	})

	notif := domain.Notification{
		Type:      domain.NotifyAchievement,
		Title:     "Achievement Unlocked!",
		Body:      "First Contact — Used AI for the first time.",
		CreatedAt: time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC), // Noon — not quiet
	}

	id, err := svc.Create(notif)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero ID")
	}
}

func TestNotification_DailyLimit(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewNotificationServiceWithPolicy(db, domain.NotificationPolicy{
		MaxPerDay:  1,
		QuietStart: "23:00",
		QuietEnd:   "05:00",
	})

	notif := domain.Notification{
		Type:      domain.NotifyAchievement,
		Title:     "First",
		Body:      "First notification",
		CreatedAt: time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC),
	}

	id1, _ := svc.Create(notif)
	if id1 == 0 {
		t.Error("first should succeed")
	}

	// Second should be suppressed
	notif.Title = "Second"
	id2, err := svc.Create(notif)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if id2 != 0 {
		t.Error("second should be suppressed (daily limit)")
	}
}

func TestNotification_QuietHours(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewNotificationServiceWithPolicy(db, domain.NotificationPolicy{
		MaxPerDay:  5,
		QuietStart: "22:00",
		QuietEnd:   "08:00",
	})

	// Midnight — inside quiet hours
	notif := domain.Notification{
		Type:      domain.NotifyDailySummary,
		Title:     "Late Night",
		Body:      "Should be suppressed",
		CreatedAt: time.Date(2025, 7, 1, 0, 30, 0, 0, time.UTC),
	}

	id, err := svc.Create(notif)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id != 0 {
		t.Error("expected suppression during quiet hours (00:30)")
	}

	// 11 PM — also quiet
	notif.CreatedAt = time.Date(2025, 7, 1, 23, 0, 0, 0, time.UTC)
	id, _ = svc.Create(notif)
	if id != 0 {
		t.Error("expected suppression during quiet hours (23:00)")
	}
}

func TestNotification_OutsideQuietHours(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewNotificationServiceWithPolicy(db, domain.NotificationPolicy{
		MaxPerDay:  5,
		QuietStart: "22:00",
		QuietEnd:   "08:00",
	})

	// 10 AM — outside quiet hours
	notif := domain.Notification{
		Type:      domain.NotifyLevelUp,
		Title:     "Level Up!",
		Body:      "You reached level 5!",
		CreatedAt: time.Date(2025, 7, 1, 10, 0, 0, 0, time.UTC),
	}

	id, err := svc.Create(notif)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == 0 {
		t.Error("expected notification to be created outside quiet hours")
	}
}

func TestNotification_Pending(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewNotificationServiceWithPolicy(db, domain.NotificationPolicy{
		MaxPerDay:  10,
		QuietStart: "23:00",
		QuietEnd:   "05:00",
	})

	notif := domain.Notification{
		Type:      domain.NotifyAchievement,
		Title:     "Test",
		Body:      "Test body",
		CreatedAt: time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC),
	}
	svc.Create(notif)

	pending, err := svc.Pending(10)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("expected 1 pending, got %d", len(pending))
	}
}

func TestNotification_MarkShown(t *testing.T) {
	db := testDB(t)
	svc := engagement.NewNotificationServiceWithPolicy(db, domain.NotificationPolicy{
		MaxPerDay:  10,
		QuietStart: "23:00",
		QuietEnd:   "05:00",
	})

	notif := domain.Notification{
		Type:      domain.NotifyMilestone,
		Title:     "Milestone!",
		Body:      "1M nodes reached!",
		CreatedAt: time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC),
	}
	id, _ := svc.Create(notif)

	if err := svc.MarkShown(id); err != nil {
		t.Fatalf("mark shown: %v", err)
	}

	pending, _ := svc.Pending(10)
	if len(pending) != 0 {
		t.Error("should be 0 pending after marking shown")
	}
}

func TestNotification_DefaultPolicy(t *testing.T) {
	policy := domain.DefaultNotificationPolicy()
	if policy.MaxPerDay != 1 {
		t.Errorf("expected max 1/day, got %d", policy.MaxPerDay)
	}
	if policy.QuietStart != "22:00" {
		t.Errorf("expected quiet start 22:00, got %s", policy.QuietStart)
	}
	if policy.QuietEnd != "08:00" {
		t.Errorf("expected quiet end 08:00, got %s", policy.QuietEnd)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Domain Type Tests
// ═══════════════════════════════════════════════════════════════════════════

func TestStreak_Multiplier(t *testing.T) {
	tests := []struct {
		days int
		want float64
	}{
		{0, 1.0},
		{1, 1.05},
		{5, 1.25},
		{10, 1.50},
		{20, 1.50}, // Capped
		{100, 1.50},
	}
	for _, tt := range tests {
		s := domain.Streak{CurrentDays: tt.days}
		got := s.Multiplier()
		if got != tt.want {
			t.Errorf("Multiplier(%d days) = %.2f, want %.2f", tt.days, got, tt.want)
		}
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
