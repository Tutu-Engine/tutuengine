package sqlite

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/tutu-network/tutu/internal/domain"
)

// ─── Credit Ledger ──────────────────────────────────────────────────────────

// InsertLedgerEntry adds a credit ledger entry.
func (d *DB) InsertLedgerEntry(entry domain.LedgerEntry) (int64, error) {
	result, err := d.db.Exec(
		`INSERT INTO credit_ledger (timestamp, type, entry_type, account, amount, task_id, description, balance)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.Timestamp.Unix(), string(entry.Type), string(entry.EntryType),
		entry.Account, entry.Amount, entry.TaskID, entry.Description, entry.Balance,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// CreditBalance returns the current balance for an account.
func (d *DB) CreditBalance(account string) (int64, error) {
	var balance sql.NullInt64
	err := d.db.QueryRow(
		`SELECT balance FROM credit_ledger WHERE account = ? ORDER BY id DESC LIMIT 1`,
		account,
	).Scan(&balance)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return balance.Int64, nil
}

// LedgerEntries returns recent ledger entries for an account.
func (d *DB) LedgerEntries(account string, limit int) ([]domain.LedgerEntry, error) {
	rows, err := d.db.Query(
		`SELECT id, timestamp, type, entry_type, account, amount, task_id, description, balance
		 FROM credit_ledger WHERE account = ? ORDER BY id DESC LIMIT ?`,
		account, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var ts int64
		var taskID, desc sql.NullString
		err := rows.Scan(&e.ID, &ts, &e.Type, &e.EntryType, &e.Account,
			&e.Amount, &taskID, &desc, &e.Balance)
		if err != nil {
			return nil, err
		}
		e.Timestamp = time.Unix(ts, 0)
		if taskID.Valid {
			e.TaskID = taskID.String
		}
		if desc.Valid {
			e.Description = desc.String
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ─── Task Repository ────────────────────────────────────────────────────────

// InsertTask creates a new task record.
func (d *DB) InsertTask(task domain.Task) error {
	_, err := d.db.Exec(
		`INSERT INTO tasks (id, type, status, priority, created_at, started_at, completed_at, credits, result_hash, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, string(task.Type), string(task.Status), task.Priority,
		task.CreatedAt.Unix(), nullableUnix(task.StartedAt), nullableUnix(task.CompletedAt),
		task.Credits, nullStr(task.ResultHash), nullStr(task.Error),
	)
	return err
}

// UpdateTaskStatus updates a task's status and optional fields.
func (d *DB) UpdateTaskStatus(id string, status domain.TaskStatus) error {
	now := time.Now().Unix()
	var err error
	switch status {
	case domain.TaskExecuting:
		_, err = d.db.Exec(`UPDATE tasks SET status = ?, started_at = ? WHERE id = ?`,
			string(status), now, id)
	case domain.TaskCompleted, domain.TaskFailed, domain.TaskCancelled:
		_, err = d.db.Exec(`UPDATE tasks SET status = ?, completed_at = ? WHERE id = ?`,
			string(status), now, id)
	default:
		_, err = d.db.Exec(`UPDATE tasks SET status = ? WHERE id = ?`, string(status), id)
	}
	return err
}

// GetTask retrieves a task by ID.
func (d *DB) GetTask(id string) (*domain.Task, error) {
	row := d.db.QueryRow(
		`SELECT id, type, status, priority, created_at, started_at, completed_at, credits, result_hash, error
		 FROM tasks WHERE id = ?`, id,
	)
	return scanTask(row)
}

// ListTasks returns tasks filtered by status.
func (d *DB) ListTasks(status domain.TaskStatus, limit int) ([]domain.Task, error) {
	rows, err := d.db.Query(
		`SELECT id, type, status, priority, created_at, started_at, completed_at, credits, result_hash, error
		 FROM tasks WHERE status = ? ORDER BY created_at DESC LIMIT ?`,
		string(status), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []domain.Task
	for rows.Next() {
		t, err := scanTaskRows(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *t)
	}
	return tasks, rows.Err()
}

func scanTask(s scanner) (*domain.Task, error) {
	var t domain.Task
	var createdAt int64
	var startedAt, completedAt sql.NullInt64
	var credits sql.NullInt64
	var resultHash, taskErr sql.NullString

	err := s.Scan(&t.ID, &t.Type, &t.Status, &t.Priority,
		&createdAt, &startedAt, &completedAt, &credits, &resultHash, &taskErr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}

	t.CreatedAt = time.Unix(createdAt, 0)
	if startedAt.Valid {
		t.StartedAt = time.Unix(startedAt.Int64, 0)
	}
	if completedAt.Valid {
		t.CompletedAt = time.Unix(completedAt.Int64, 0)
	}
	if credits.Valid {
		t.Credits = credits.Int64
	}
	if resultHash.Valid {
		t.ResultHash = resultHash.String
	}
	if taskErr.Valid {
		t.Error = taskErr.String
	}
	return &t, nil
}

func scanTaskRows(rows *sql.Rows) (*domain.Task, error) {
	return scanTask(rows)
}

// ─── Peer Repository ────────────────────────────────────────────────────────

// UpsertPeer inserts or updates a peer record.
func (d *DB) UpsertPeer(peer domain.Peer) error {
	_, err := d.db.Exec(
		`INSERT INTO peers (node_id, region, endpoint, last_seen, reputation, state)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(node_id) DO UPDATE SET
			region=excluded.region,
			endpoint=excluded.endpoint,
			last_seen=excluded.last_seen,
			reputation=excluded.reputation,
			state=excluded.state`,
		peer.NodeID, peer.Region, peer.Endpoint,
		peer.LastSeen.Unix(), peer.Reputation, string(peer.State),
	)
	return err
}

// GetPeer retrieves a peer by node ID.
func (d *DB) GetPeer(nodeID string) (*domain.Peer, error) {
	row := d.db.QueryRow(
		`SELECT node_id, region, endpoint, last_seen, reputation, state
		 FROM peers WHERE node_id = ?`, nodeID,
	)
	return scanPeer(row)
}

// ListPeers returns all known peers, optionally filtered by state.
func (d *DB) ListPeers(state domain.PeerState) ([]domain.Peer, error) {
	var rows *sql.Rows
	var err error
	if state == "" {
		rows, err = d.db.Query(
			`SELECT node_id, region, endpoint, last_seen, reputation, state
			 FROM peers ORDER BY last_seen DESC`)
	} else {
		rows, err = d.db.Query(
			`SELECT node_id, region, endpoint, last_seen, reputation, state
			 FROM peers WHERE state = ? ORDER BY last_seen DESC`, string(state))
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var peers []domain.Peer
	for rows.Next() {
		p, err := scanPeerRows(rows)
		if err != nil {
			return nil, err
		}
		peers = append(peers, *p)
	}
	return peers, rows.Err()
}

// DeletePeer removes a peer record.
func (d *DB) DeletePeer(nodeID string) error {
	_, err := d.db.Exec(`DELETE FROM peers WHERE node_id = ?`, nodeID)
	return err
}

// UpdatePeerState changes a peer's gossip state.
func (d *DB) UpdatePeerState(nodeID string, state domain.PeerState) error {
	_, err := d.db.Exec(`UPDATE peers SET state = ? WHERE node_id = ?`, string(state), nodeID)
	return err
}

func scanPeer(s scanner) (*domain.Peer, error) {
	var p domain.Peer
	var lastSeen int64
	var endpoint sql.NullString

	err := s.Scan(&p.NodeID, &p.Region, &endpoint, &lastSeen, &p.Reputation, &p.State)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan peer: %w", err)
	}

	p.LastSeen = time.Unix(lastSeen, 0)
	if endpoint.Valid {
		p.Endpoint = endpoint.String
	}
	return &p, nil
}

func scanPeerRows(rows *sql.Rows) (*domain.Peer, error) {
	return scanPeer(rows)
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
