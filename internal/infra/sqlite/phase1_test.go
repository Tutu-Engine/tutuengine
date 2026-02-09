package sqlite

import (
	"testing"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ─── Credit Ledger Tests ────────────────────────────────────────────────────

func TestInsertLedgerEntry(t *testing.T) {
	db := newTestDB(t)

	entry := domain.LedgerEntry{
		Timestamp:   time.Now(),
		Type:        domain.TxEarn,
		EntryType:   domain.EntryCredit,
		Account:     "node_balance",
		Amount:      50,
		TaskID:      "task-1",
		Description: "completed inference",
		Balance:     50,
	}

	id, err := db.InsertLedgerEntry(entry)
	if err != nil {
		t.Fatalf("InsertLedgerEntry() error: %v", err)
	}
	if id < 1 {
		t.Errorf("id = %d, want >= 1", id)
	}
}

func TestCreditBalance_Empty(t *testing.T) {
	db := newTestDB(t)

	bal, err := db.CreditBalance("node_balance")
	if err != nil {
		t.Fatalf("CreditBalance() error: %v", err)
	}
	if bal != 0 {
		t.Errorf("balance = %d, want 0", bal)
	}
}

func TestCreditBalance_AfterEntries(t *testing.T) {
	db := newTestDB(t)

	db.InsertLedgerEntry(domain.LedgerEntry{
		Timestamp: time.Now(), Type: domain.TxEarn, EntryType: domain.EntryCredit,
		Account: "node_balance", Amount: 50, Balance: 50,
	})
	db.InsertLedgerEntry(domain.LedgerEntry{
		Timestamp: time.Now(), Type: domain.TxEarn, EntryType: domain.EntryCredit,
		Account: "node_balance", Amount: 30, Balance: 80,
	})

	bal, err := db.CreditBalance("node_balance")
	if err != nil {
		t.Fatalf("CreditBalance() error: %v", err)
	}
	if bal != 80 {
		t.Errorf("balance = %d, want 80 (last entry)", bal)
	}
}

func TestLedgerEntries(t *testing.T) {
	db := newTestDB(t)

	for i := 0; i < 5; i++ {
		db.InsertLedgerEntry(domain.LedgerEntry{
			Timestamp: time.Now(), Type: domain.TxEarn, EntryType: domain.EntryCredit,
			Account: "node_balance", Amount: int64(10 * (i + 1)), Balance: int64(10 * (i + 1)),
		})
	}

	entries, err := db.LedgerEntries("node_balance", 3)
	if err != nil {
		t.Fatalf("LedgerEntries() error: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("entries = %d, want 3 (limited)", len(entries))
	}
	// Most recent first
	if entries[0].Balance < entries[1].Balance {
		t.Error("entries should be ordered by id DESC (most recent first)")
	}
}

func TestLedgerEntries_Empty(t *testing.T) {
	db := newTestDB(t)

	entries, err := db.LedgerEntries("node_balance", 10)
	if err != nil {
		t.Fatalf("LedgerEntries() error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %d, want 0", len(entries))
	}
}

// ─── Task Repository Tests ─────────────────────────────────────────────────

func TestInsertTask(t *testing.T) {
	db := newTestDB(t)

	task := domain.Task{
		ID:        "task-001",
		Type:      domain.TaskInference,
		Status:    domain.TaskQueued,
		Priority:  5,
		CreatedAt: time.Now(),
	}

	err := db.InsertTask(task)
	if err != nil {
		t.Fatalf("InsertTask() error: %v", err)
	}
}

func TestGetTask(t *testing.T) {
	db := newTestDB(t)

	now := time.Now().Truncate(time.Second) // SQLite stores seconds
	task := domain.Task{
		ID:        "task-001",
		Type:      domain.TaskInference,
		Status:    domain.TaskQueued,
		Priority:  5,
		CreatedAt: now,
	}
	db.InsertTask(task)

	got, err := db.GetTask("task-001")
	if err != nil {
		t.Fatalf("GetTask() error: %v", err)
	}
	if got == nil {
		t.Fatal("GetTask() returned nil")
	}
	if got.ID != "task-001" {
		t.Errorf("ID = %s, want task-001", got.ID)
	}
	if got.Type != domain.TaskInference {
		t.Errorf("Type = %s, want INFERENCE", got.Type)
	}
	if got.Status != domain.TaskQueued {
		t.Errorf("Status = %s, want QUEUED", got.Status)
	}
	if got.Priority != 5 {
		t.Errorf("Priority = %d, want 5", got.Priority)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	db := newTestDB(t)

	got, err := db.GetTask("nonexistent")
	if err != nil {
		t.Fatalf("GetTask() error: %v", err)
	}
	if got != nil {
		t.Errorf("GetTask() = %v, want nil", got)
	}
}

func TestUpdateTaskStatus_Executing(t *testing.T) {
	db := newTestDB(t)

	task := domain.Task{
		ID: "task-001", Type: domain.TaskInference,
		Status: domain.TaskQueued, CreatedAt: time.Now(),
	}
	db.InsertTask(task)

	err := db.UpdateTaskStatus("task-001", domain.TaskExecuting)
	if err != nil {
		t.Fatalf("UpdateTaskStatus() error: %v", err)
	}

	got, _ := db.GetTask("task-001")
	if got.Status != domain.TaskExecuting {
		t.Errorf("Status = %s, want EXECUTING", got.Status)
	}
	if got.StartedAt.IsZero() {
		t.Error("StartedAt should be set when status is EXECUTING")
	}
}

func TestUpdateTaskStatus_Completed(t *testing.T) {
	db := newTestDB(t)

	task := domain.Task{
		ID: "task-001", Type: domain.TaskInference,
		Status: domain.TaskQueued, CreatedAt: time.Now(),
	}
	db.InsertTask(task)

	db.UpdateTaskStatus("task-001", domain.TaskExecuting)
	db.UpdateTaskStatus("task-001", domain.TaskCompleted)

	got, _ := db.GetTask("task-001")
	if got.Status != domain.TaskCompleted {
		t.Errorf("Status = %s, want COMPLETED", got.Status)
	}
	if got.CompletedAt.IsZero() {
		t.Error("CompletedAt should be set when status is COMPLETED")
	}
}

func TestListTasks(t *testing.T) {
	db := newTestDB(t)

	for i := 0; i < 5; i++ {
		db.InsertTask(domain.Task{
			ID: "task-" + string(rune('A'+i)), Type: domain.TaskInference,
			Status: domain.TaskQueued, CreatedAt: time.Now(),
		})
	}
	// One completed task
	db.InsertTask(domain.Task{
		ID: "task-X", Type: domain.TaskInference,
		Status: domain.TaskCompleted, CreatedAt: time.Now(),
	})

	queued, err := db.ListTasks(domain.TaskQueued, 10)
	if err != nil {
		t.Fatalf("ListTasks() error: %v", err)
	}
	if len(queued) != 5 {
		t.Errorf("queued tasks = %d, want 5", len(queued))
	}

	completed, _ := db.ListTasks(domain.TaskCompleted, 10)
	if len(completed) != 1 {
		t.Errorf("completed tasks = %d, want 1", len(completed))
	}
}

func TestListTasks_WithLimit(t *testing.T) {
	db := newTestDB(t)

	for i := 0; i < 10; i++ {
		db.InsertTask(domain.Task{
			ID: "task-" + string(rune('A'+i)), Type: domain.TaskInference,
			Status: domain.TaskQueued, CreatedAt: time.Now(),
		})
	}

	tasks, _ := db.ListTasks(domain.TaskQueued, 3)
	if len(tasks) != 3 {
		t.Errorf("tasks = %d, want 3 (limited)", len(tasks))
	}
}

// ─── Peer Repository Tests ─────────────────────────────────────────────────

func TestUpsertPeer(t *testing.T) {
	db := newTestDB(t)

	peer := domain.Peer{
		NodeID:     "abc123",
		Region:     "us-west",
		Endpoint:   "10.0.0.1:9090",
		LastSeen:   time.Now(),
		Reputation: 0.8,
		State:      domain.PeerAlive,
	}

	err := db.UpsertPeer(peer)
	if err != nil {
		t.Fatalf("UpsertPeer() error: %v", err)
	}
}

func TestGetPeer(t *testing.T) {
	db := newTestDB(t)

	peer := domain.Peer{
		NodeID:     "abc123",
		Region:     "us-west",
		Endpoint:   "10.0.0.1:9090",
		LastSeen:   time.Now(),
		Reputation: 0.8,
		State:      domain.PeerAlive,
	}
	db.UpsertPeer(peer)

	got, err := db.GetPeer("abc123")
	if err != nil {
		t.Fatalf("GetPeer() error: %v", err)
	}
	if got == nil {
		t.Fatal("GetPeer() returned nil")
	}
	if got.NodeID != "abc123" {
		t.Errorf("NodeID = %s, want abc123", got.NodeID)
	}
	if got.Region != "us-west" {
		t.Errorf("Region = %s, want us-west", got.Region)
	}
	if got.Endpoint != "10.0.0.1:9090" {
		t.Errorf("Endpoint = %s, want 10.0.0.1:9090", got.Endpoint)
	}
}

func TestGetPeer_NotFound(t *testing.T) {
	db := newTestDB(t)

	got, err := db.GetPeer("nonexistent")
	if err != nil {
		t.Fatalf("GetPeer() error: %v", err)
	}
	if got != nil {
		t.Errorf("GetPeer() = %v, want nil", got)
	}
}

func TestUpsertPeer_Update(t *testing.T) {
	db := newTestDB(t)

	peer := domain.Peer{
		NodeID: "abc123", Region: "us-west",
		LastSeen: time.Now(), Reputation: 0.5, State: domain.PeerAlive,
	}
	db.UpsertPeer(peer)

	// Update
	peer.Region = "eu-central"
	peer.Reputation = 0.9
	db.UpsertPeer(peer)

	got, _ := db.GetPeer("abc123")
	if got.Region != "eu-central" {
		t.Errorf("Region = %s, want eu-central after update", got.Region)
	}
	if got.Reputation != 0.9 {
		t.Errorf("Reputation = %f, want 0.9 after update", got.Reputation)
	}
}

func TestListPeers_All(t *testing.T) {
	db := newTestDB(t)

	for _, id := range []string{"a", "b", "c"} {
		db.UpsertPeer(domain.Peer{
			NodeID: id, Region: "test", LastSeen: time.Now(),
			State: domain.PeerAlive,
		})
	}

	peers, err := db.ListPeers("")
	if err != nil {
		t.Fatalf("ListPeers() error: %v", err)
	}
	if len(peers) != 3 {
		t.Errorf("peers = %d, want 3", len(peers))
	}
}

func TestListPeers_ByState(t *testing.T) {
	db := newTestDB(t)

	db.UpsertPeer(domain.Peer{NodeID: "a", Region: "test", LastSeen: time.Now(), State: domain.PeerAlive})
	db.UpsertPeer(domain.Peer{NodeID: "b", Region: "test", LastSeen: time.Now(), State: domain.PeerSuspect})
	db.UpsertPeer(domain.Peer{NodeID: "c", Region: "test", LastSeen: time.Now(), State: domain.PeerDead})

	alive, _ := db.ListPeers(domain.PeerAlive)
	if len(alive) != 1 {
		t.Errorf("alive peers = %d, want 1", len(alive))
	}
}

func TestDeletePeer(t *testing.T) {
	db := newTestDB(t)

	db.UpsertPeer(domain.Peer{NodeID: "abc", Region: "test", LastSeen: time.Now(), State: domain.PeerAlive})
	err := db.DeletePeer("abc")
	if err != nil {
		t.Fatalf("DeletePeer() error: %v", err)
	}

	got, _ := db.GetPeer("abc")
	if got != nil {
		t.Error("peer should be deleted")
	}
}

func TestUpdatePeerState(t *testing.T) {
	db := newTestDB(t)

	db.UpsertPeer(domain.Peer{NodeID: "abc", Region: "test", LastSeen: time.Now(), State: domain.PeerAlive})
	err := db.UpdatePeerState("abc", domain.PeerSuspect)
	if err != nil {
		t.Fatalf("UpdatePeerState() error: %v", err)
	}

	got, _ := db.GetPeer("abc")
	if got.State != domain.PeerSuspect {
		t.Errorf("State = %s, want SUSPECT", got.State)
	}
}
