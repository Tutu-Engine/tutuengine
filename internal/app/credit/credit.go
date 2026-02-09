// Package credit implements the double-entry credit ledger.
// Architecture Part X: Every credit operation creates matched DEBIT/CREDIT
// entries. SUM(debits) == SUM(credits) is an invariant.
package credit

import (
	"fmt"
	"math"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/infra/sqlite"
)

// Service manages the credit economy.
type Service struct {
	db *sqlite.DB
}

// NewService creates a credit service.
func NewService(db *sqlite.DB) *Service {
	return &Service{db: db}
}

// Balance returns the current node balance.
func (s *Service) Balance() (int64, error) {
	return s.db.CreditBalance("node_balance")
}

// Earn records credits earned from completing a task.
// Creates matched DEBIT (system_pool) and CREDIT (node_balance) entries.
func (s *Service) Earn(amount int64, taskID, reason string) error {
	if amount <= 0 {
		return fmt.Errorf("earn amount must be positive, got %d", amount)
	}

	now := time.Now()

	// Get current balances
	poolBal, err := s.db.CreditBalance("system_pool")
	if err != nil {
		return fmt.Errorf("get pool balance: %w", err)
	}
	nodeBal, err := s.db.CreditBalance("node_balance")
	if err != nil {
		return fmt.Errorf("get node balance: %w", err)
	}

	// DEBIT system_pool (source of credits)
	_, err = s.db.InsertLedgerEntry(domain.LedgerEntry{
		Timestamp:   now,
		Type:        domain.TxEarn,
		EntryType:   domain.EntryDebit,
		Account:     "system_pool",
		Amount:      amount,
		TaskID:      taskID,
		Description: reason,
		Balance:     poolBal - amount,
	})
	if err != nil {
		return fmt.Errorf("debit system_pool: %w", err)
	}

	// CREDIT node_balance (destination)
	_, err = s.db.InsertLedgerEntry(domain.LedgerEntry{
		Timestamp:   now,
		Type:        domain.TxEarn,
		EntryType:   domain.EntryCredit,
		Account:     "node_balance",
		Amount:      amount,
		TaskID:      taskID,
		Description: reason,
		Balance:     nodeBal + amount,
	})
	if err != nil {
		return fmt.Errorf("credit node_balance: %w", err)
	}

	return nil
}

// Spend records credits spent for consuming a service.
func (s *Service) Spend(amount int64, taskID, reason string) error {
	if amount <= 0 {
		return fmt.Errorf("spend amount must be positive, got %d", amount)
	}

	nodeBal, err := s.db.CreditBalance("node_balance")
	if err != nil {
		return fmt.Errorf("get node balance: %w", err)
	}
	if nodeBal < amount {
		return fmt.Errorf("insufficient credits: have %d, need %d", nodeBal, amount)
	}

	now := time.Now()
	poolBal, _ := s.db.CreditBalance("system_pool")

	// DEBIT node_balance
	_, err = s.db.InsertLedgerEntry(domain.LedgerEntry{
		Timestamp:   now,
		Type:        domain.TxSpend,
		EntryType:   domain.EntryDebit,
		Account:     "node_balance",
		Amount:      amount,
		TaskID:      taskID,
		Description: reason,
		Balance:     nodeBal - amount,
	})
	if err != nil {
		return err
	}

	// CREDIT system_pool
	_, err = s.db.InsertLedgerEntry(domain.LedgerEntry{
		Timestamp:   now,
		Type:        domain.TxSpend,
		EntryType:   domain.EntryCredit,
		Account:     "system_pool",
		Amount:      amount,
		TaskID:      taskID,
		Description: reason,
		Balance:     poolBal + amount,
	})
	return err
}

// History returns recent ledger entries for the node.
func (s *Service) History(limit int) ([]domain.LedgerEntry, error) {
	return s.db.LedgerEntries("node_balance", limit)
}

// ─── Earning Formula (Architecture Part X) ──────────────────────────────────
// credits = base * complexity * streak_multiplier * reputation_bonus

// EarningAmount computes credits earned for a task.
func EarningAmount(taskType domain.TaskType, tokensProcessed int, streakDays int, reputation float64) int64 {
	baseRates := map[domain.TaskType]float64{
		domain.TaskInference: 1.0,
		domain.TaskEmbedding: 0.3,
		domain.TaskFineTune:  10.0,
		domain.TaskAgent:     5.0,
	}

	base, ok := baseRates[taskType]
	if !ok {
		base = 1.0
	}

	complexity := float64(tokensProcessed) / 1000.0
	if complexity < 0.1 {
		complexity = 0.1 // Minimum
	}

	streakBonus := 1.0 + math.Min(float64(streakDays)*0.05, 0.50) // Max 50% bonus
	repBonus := 1.0 + (reputation - 0.5)                          // +/- 50% based on rep
	if repBonus < 0.5 {
		repBonus = 0.5 // Floor
	}

	result := base * complexity * streakBonus * repBonus
	if result < 1 {
		return 1 // Minimum 1 credit
	}
	return int64(result)
}

// MaxHourlyEarning is the anti-fraud earning cap per node per hour.
const MaxHourlyEarning int64 = 100
