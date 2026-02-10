package sqlite

import (
	"database/sql"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ─── Engagement Key-Value ───────────────────────────────────────────────────

// SetEngagement stores an engagement key-value pair.
func (d *DB) SetEngagement(key, value string) error {
	_, err := d.db.Exec(
		`INSERT INTO engagement (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	)
	return err
}

// GetEngagement retrieves an engagement value by key.
// Returns "" if key not found.
func (d *DB) GetEngagement(key string) (string, error) {
	var value string
	err := d.db.QueryRow(`SELECT value FROM engagement WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// ─── Achievements ───────────────────────────────────────────────────────────

// UnlockAchievement records an achievement as unlocked.
// Returns false if already unlocked (idempotent).
func (d *DB) UnlockAchievement(id string, at time.Time) (bool, error) {
	result, err := d.db.Exec(
		`INSERT OR IGNORE INTO achievements (id, unlocked_at, notified) VALUES (?, ?, 0)`,
		id, at.Unix(),
	)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n > 0, nil // true = newly unlocked
}

// IsAchievementUnlocked checks whether an achievement has been unlocked.
func (d *DB) IsAchievementUnlocked(id string) (bool, error) {
	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM achievements WHERE id = ?`, id).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ListUnlockedAchievements returns all unlocked achievements.
func (d *DB) ListUnlockedAchievements() ([]domain.UnlockedAchievement, error) {
	rows, err := d.db.Query(
		`SELECT id, unlocked_at, notified FROM achievements ORDER BY unlocked_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var achievements []domain.UnlockedAchievement
	for rows.Next() {
		var a domain.UnlockedAchievement
		var unlockedAt int64
		if err := rows.Scan(&a.ID, &unlockedAt, &a.Notified); err != nil {
			return nil, err
		}
		a.UnlockedAt = time.Unix(unlockedAt, 0)
		achievements = append(achievements, a)
	}
	return achievements, rows.Err()
}

// MarkAchievementNotified marks an achievement notification as shown.
func (d *DB) MarkAchievementNotified(id string) error {
	_, err := d.db.Exec(`UPDATE achievements SET notified = 1 WHERE id = ?`, id)
	return err
}

// UnlockedAchievementCount returns the total number of unlocked achievements.
func (d *DB) UnlockedAchievementCount() (int, error) {
	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM achievements`).Scan(&count)
	return count, err
}

// ─── Quests ─────────────────────────────────────────────────────────────────

// InsertQuest creates a new quest.
func (d *DB) InsertQuest(q domain.Quest) error {
	_, err := d.db.Exec(
		`INSERT INTO quests (id, type, description, target, progress, reward_xp, reward_credits, expires_at, completed)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		q.ID, string(q.Type), q.Description, q.Target, q.Progress,
		q.RewardXP, q.RewardCredits, q.ExpiresAt.Unix(), q.Completed,
	)
	return err
}

// GetQuest retrieves a quest by ID.
func (d *DB) GetQuest(id string) (*domain.Quest, error) {
	row := d.db.QueryRow(
		`SELECT id, type, description, target, progress, reward_xp, reward_credits, expires_at, completed
		 FROM quests WHERE id = ?`, id,
	)
	return scanQuest(row)
}

// ListActiveQuests returns non-expired, non-completed quests.
func (d *DB) ListActiveQuests() ([]domain.Quest, error) {
	now := time.Now().Unix()
	rows, err := d.db.Query(
		`SELECT id, type, description, target, progress, reward_xp, reward_credits, expires_at, completed
		 FROM quests WHERE completed = 0 AND expires_at > ? ORDER BY expires_at ASC`, now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var quests []domain.Quest
	for rows.Next() {
		q, err := scanQuestRows(rows)
		if err != nil {
			return nil, err
		}
		quests = append(quests, *q)
	}
	return quests, rows.Err()
}

// ListAllQuests returns all quests regardless of status.
func (d *DB) ListAllQuests() ([]domain.Quest, error) {
	rows, err := d.db.Query(
		`SELECT id, type, description, target, progress, reward_xp, reward_credits, expires_at, completed
		 FROM quests ORDER BY expires_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var quests []domain.Quest
	for rows.Next() {
		q, err := scanQuestRows(rows)
		if err != nil {
			return nil, err
		}
		quests = append(quests, *q)
	}
	return quests, rows.Err()
}

// UpdateQuestProgress increments quest progress. Returns the updated quest.
func (d *DB) UpdateQuestProgress(id string, delta int) (*domain.Quest, error) {
	_, err := d.db.Exec(
		`UPDATE quests SET progress = MIN(progress + ?, target) WHERE id = ? AND completed = 0`,
		delta, id,
	)
	if err != nil {
		return nil, err
	}
	return d.GetQuest(id)
}

// CompleteQuest marks a quest as completed.
func (d *DB) CompleteQuest(id string) error {
	_, err := d.db.Exec(`UPDATE quests SET completed = 1 WHERE id = ?`, id)
	return err
}

// DeleteExpiredQuests removes quests that expired before the given time.
func (d *DB) DeleteExpiredQuests(before time.Time) (int64, error) {
	result, err := d.db.Exec(
		`DELETE FROM quests WHERE expires_at < ? AND completed = 0`, before.Unix(),
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// ─── Notifications ──────────────────────────────────────────────────────────

// InsertNotification creates a new notification.
func (d *DB) InsertNotification(n domain.Notification) (int64, error) {
	result, err := d.db.Exec(
		`INSERT INTO notifications (type, title, body, created_at, shown)
		 VALUES (?, ?, ?, ?, ?)`,
		string(n.Type), n.Title, n.Body, n.CreatedAt.Unix(), n.Shown,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// NotificationCountToday returns how many notifications were created today.
func (d *DB) NotificationCountToday() (int, error) {
	startOfDay := time.Now().Truncate(24 * time.Hour).Unix()
	var count int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM notifications WHERE created_at >= ?`, startOfDay,
	).Scan(&count)
	return count, err
}

// ListPendingNotifications returns unshown notifications.
func (d *DB) ListPendingNotifications(limit int) ([]domain.Notification, error) {
	rows, err := d.db.Query(
		`SELECT id, type, title, body, created_at, shown
		 FROM notifications WHERE shown = 0 ORDER BY created_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notifs []domain.Notification
	for rows.Next() {
		n, err := scanNotifRows(rows)
		if err != nil {
			return nil, err
		}
		notifs = append(notifs, *n)
	}
	return notifs, rows.Err()
}

// MarkNotificationShown marks a notification as shown.
func (d *DB) MarkNotificationShown(id int64) error {
	_, err := d.db.Exec(`UPDATE notifications SET shown = 1 WHERE id = ?`, id)
	return err
}

// ─── Quest Scanners ─────────────────────────────────────────────────────────

func scanQuest(s scanner) (*domain.Quest, error) {
	var q domain.Quest
	var expiresAt int64
	err := s.Scan(&q.ID, &q.Type, &q.Description, &q.Target, &q.Progress,
		&q.RewardXP, &q.RewardCredits, &expiresAt, &q.Completed)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	q.ExpiresAt = time.Unix(expiresAt, 0)
	return &q, nil
}

func scanQuestRows(rows *sql.Rows) (*domain.Quest, error) {
	return scanQuest(rows)
}

func scanNotifRows(rows *sql.Rows) (*domain.Notification, error) {
	var n domain.Notification
	var createdAt int64
	err := rows.Scan(&n.ID, &n.Type, &n.Title, &n.Body, &createdAt, &n.Shown)
	if err != nil {
		return nil, err
	}
	n.CreatedAt = time.Unix(createdAt, 0)
	return &n, nil
}
