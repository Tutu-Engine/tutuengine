package engagement

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/infra/sqlite"
)

// NotificationService manages smart notifications.
// Architecture Part XIII v3.0:
//   - Max 1 notification per day (hard cap)
//   - No notifications between 22:00–08:00 (user's timezone)
//   - Only notify for: achievement unlocked, level up, daily earnings summary
//   - NEVER notify for: streak at risk, network down, low credits
type NotificationService struct {
	db     *sqlite.DB
	policy domain.NotificationPolicy
}

// NewNotificationService creates a notification service with default policy.
func NewNotificationService(db *sqlite.DB) *NotificationService {
	return &NotificationService{
		db:     db,
		policy: domain.DefaultNotificationPolicy(),
	}
}

// NewNotificationServiceWithPolicy creates a notification service with custom policy.
func NewNotificationServiceWithPolicy(db *sqlite.DB, policy domain.NotificationPolicy) *NotificationService {
	return &NotificationService{db: db, policy: policy}
}

// Create creates a notification if policy allows it.
// Returns the notification ID (0 if suppressed by policy) and any error.
func (n *NotificationService) Create(notif domain.Notification) (int64, error) {
	// Check daily limit
	todayCount, err := n.db.NotificationCountToday()
	if err != nil {
		return 0, fmt.Errorf("count today: %w", err)
	}
	if todayCount >= n.policy.MaxPerDay {
		return 0, nil // Suppressed — daily limit reached
	}

	// Check quiet hours
	if n.isQuietHour(notif.CreatedAt) {
		return 0, nil // Suppressed — quiet hours
	}

	notif.CreatedAt = time.Now()
	notif.Shown = false

	id, err := n.db.InsertNotification(notif)
	if err != nil {
		return 0, fmt.Errorf("insert notification: %w", err)
	}
	return id, nil
}

// Pending returns unshown notifications.
func (n *NotificationService) Pending(limit int) ([]domain.Notification, error) {
	return n.db.ListPendingNotifications(limit)
}

// MarkShown marks a notification as shown.
func (n *NotificationService) MarkShown(id int64) error {
	return n.db.MarkNotificationShown(id)
}

// TodayCount returns how many notifications were sent today.
func (n *NotificationService) TodayCount() (int, error) {
	return n.db.NotificationCountToday()
}

// Policy returns the current notification policy.
func (n *NotificationService) Policy() domain.NotificationPolicy {
	return n.policy
}

// isQuietHour returns true if the given time falls within quiet hours.
// Policy: no notifications between QuietStart and QuietEnd.
func (n *NotificationService) isQuietHour(t time.Time) bool {
	startHour, startMin := parseHHMM(n.policy.QuietStart)
	endHour, endMin := parseHHMM(n.policy.QuietEnd)

	hour, min := t.Hour(), t.Minute()
	timeMinutes := hour*60 + min
	startMinutes := startHour*60 + startMin
	endMinutes := endHour*60 + endMin

	if startMinutes > endMinutes {
		// Wraps midnight: e.g., 22:00 – 08:00
		return timeMinutes >= startMinutes || timeMinutes < endMinutes
	}
	// Same day range
	return timeMinutes >= startMinutes && timeMinutes < endMinutes
}

// parseHHMM parses "HH:MM" into hour and minute.
func parseHHMM(s string) (int, int) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	h, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])
	return h, m
}
