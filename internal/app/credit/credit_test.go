package credit

import (
	"testing"

	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/infra/sqlite"
)

func newTestDB(t *testing.T) *sqlite.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(dir)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ─── Service Tests ──────────────────────────────────────────────────────────

func TestService_InitialBalance(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	bal, err := svc.Balance()
	if err != nil {
		t.Fatalf("Balance() error: %v", err)
	}
	if bal != 0 {
		t.Errorf("initial balance = %d, want 0", bal)
	}
}

func TestService_Earn(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	err := svc.Earn(50, "task-1", "completed inference")
	if err != nil {
		t.Fatalf("Earn() error: %v", err)
	}

	bal, err := svc.Balance()
	if err != nil {
		t.Fatalf("Balance() error: %v", err)
	}
	if bal != 50 {
		t.Errorf("balance after earn = %d, want 50", bal)
	}
}

func TestService_EarnMultiple(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	svc.Earn(10, "t1", "first")
	svc.Earn(20, "t2", "second")
	svc.Earn(30, "t3", "third")

	bal, _ := svc.Balance()
	if bal != 60 {
		t.Errorf("balance = %d, want 60", bal)
	}
}

func TestService_EarnNegative(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	err := svc.Earn(-5, "task-bad", "negative")
	if err == nil {
		t.Error("Earn(-5) should return error")
	}
}

func TestService_EarnZero(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	err := svc.Earn(0, "task-zero", "zero")
	if err == nil {
		t.Error("Earn(0) should return error")
	}
}

func TestService_Spend(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	svc.Earn(100, "t1", "earn first")

	err := svc.Spend(30, "t2", "spend some")
	if err != nil {
		t.Fatalf("Spend() error: %v", err)
	}

	bal, _ := svc.Balance()
	if bal != 70 {
		t.Errorf("balance after earn 100, spend 30 = %d, want 70", bal)
	}
}

func TestService_SpendInsufficientFunds(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	svc.Earn(10, "t1", "small earn")

	err := svc.Spend(20, "t2", "too much")
	if err == nil {
		t.Error("Spend(20) with balance=10 should return error")
	}
}

func TestService_SpendNegative(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	err := svc.Spend(-5, "task-bad", "negative")
	if err == nil {
		t.Error("Spend(-5) should return error")
	}
}

func TestService_History(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	svc.Earn(10, "t1", "first")
	svc.Earn(20, "t2", "second")

	entries, err := svc.History(10)
	if err != nil {
		t.Fatalf("History() error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("History() = %d entries, want 2", len(entries))
	}
}

func TestService_HistoryEmpty(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	entries, err := svc.History(10)
	if err != nil {
		t.Fatalf("History() error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("History() = %d entries, want 0", len(entries))
	}
}

// ─── Earning Formula Tests ──────────────────────────────────────────────────

func TestEarningAmount_BasicInference(t *testing.T) {
	// 1000 tokens = complexity 1.0, no streak, neutral reputation
	credits := EarningAmount(domain.TaskInference, 1000, 0, 0.5)
	if credits < 1 {
		t.Errorf("credits = %d, want >= 1", credits)
	}
}

func TestEarningAmount_FineTuneHigher(t *testing.T) {
	// Use 50k tokens so values are well above minimum floor
	inference := EarningAmount(domain.TaskInference, 50000, 0, 0.5)
	fineTune := EarningAmount(domain.TaskFineTune, 50000, 0, 0.5)

	if fineTune <= inference {
		t.Errorf("fine_tune(%d) should be > inference(%d)", fineTune, inference)
	}
}

func TestEarningAmount_EmbeddingLower(t *testing.T) {
	inference := EarningAmount(domain.TaskInference, 50000, 0, 0.5)
	embedding := EarningAmount(domain.TaskEmbedding, 50000, 0, 0.5)

	if embedding >= inference {
		t.Errorf("embedding(%d) should be < inference(%d)", embedding, inference)
	}
}

func TestEarningAmount_StreakBonus(t *testing.T) {
	noStreak := EarningAmount(domain.TaskInference, 50000, 0, 0.5)
	withStreak := EarningAmount(domain.TaskInference, 50000, 10, 0.5)

	if withStreak <= noStreak {
		t.Errorf("10-day streak (%d) should earn more than no streak (%d)", withStreak, noStreak)
	}
}

func TestEarningAmount_StreakCap(t *testing.T) {
	// Streak bonus caps at 50% (10 days = 50%)
	tenDay := EarningAmount(domain.TaskInference, 50000, 10, 0.5)
	hundredDay := EarningAmount(domain.TaskInference, 50000, 100, 0.5)

	if hundredDay != tenDay {
		t.Errorf("100-day streak (%d) should equal 10-day (%d) due to cap", hundredDay, tenDay)
	}
}

func TestEarningAmount_ReputationBonus(t *testing.T) {
	lowRep := EarningAmount(domain.TaskInference, 50000, 0, 0.1)
	highRep := EarningAmount(domain.TaskInference, 50000, 0, 1.0)

	if highRep <= lowRep {
		t.Errorf("high rep (%d) should earn more than low rep (%d)", highRep, lowRep)
	}
}

func TestEarningAmount_MinimumCredits(t *testing.T) {
	// Very small task should still return at least 1 credit
	credits := EarningAmount(domain.TaskEmbedding, 1, 0, 0.0)
	if credits < 1 {
		t.Errorf("minimum credits = %d, want >= 1", credits)
	}
}

func TestMaxHourlyEarning(t *testing.T) {
	if MaxHourlyEarning != 100 {
		t.Errorf("MaxHourlyEarning = %d, want 100", MaxHourlyEarning)
	}
}
