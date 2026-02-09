// Package sqlite provides SQLite-based persistent storage for TuTu.
// Uses WAL mode for concurrent reads and crash-safe writes.
package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // Pure-Go SQLite driver (no CGO required)

	"github.com/tutu-network/tutu/internal/domain"
)

// DB wraps a SQLite connection with WAL mode and migrations.
type DB struct {
	db *sql.DB
}

// Open creates or opens the SQLite database at dir/state.db.
// Enables WAL mode, foreign keys, and 5-second busy timeout.
func Open(dir string) (*DB, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	dbPath := filepath.Join(dir, "state.db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	// Connection pool settings for SQLite
	db.SetMaxOpenConns(1) // SQLite is single-writer
	db.SetMaxIdleConns(1)

	d := &DB{db: db}
	if err := d.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return d, nil
}

// Close cleanly shuts down the database.
func (d *DB) Close() error {
	return d.db.Close()
}

// Ping checks database connectivity.
func (d *DB) Ping() error {
	return d.db.Ping()
}

// migrate runs idempotent schema migrations.
func (d *DB) migrate() error {
	migrations := []string{
		// Phase 0: Base schema
		`CREATE TABLE IF NOT EXISTS models (
			name         TEXT PRIMARY KEY,
			digest       TEXT NOT NULL,
			size_bytes   INTEGER NOT NULL,
			format       TEXT NOT NULL DEFAULT 'gguf',
			family       TEXT NOT NULL DEFAULT '',
			parameters   TEXT NOT NULL DEFAULT '',
			quantization TEXT NOT NULL DEFAULT '',
			pulled_at    INTEGER NOT NULL,
			last_used    INTEGER,
			pinned       BOOLEAN DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS node_info (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_models_used ON models(last_used)`,
	}

	for _, m := range migrations {
		if _, err := d.db.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, m)
		}
	}
	return nil
}

// ─── Model Repository ───────────────────────────────────────────────────────

// UpsertModel inserts or updates a model record.
func (d *DB) UpsertModel(info domain.ModelInfo) error {
	_, err := d.db.Exec(
		`INSERT INTO models (name, digest, size_bytes, format, family, parameters, quantization, pulled_at, last_used, pinned)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
			digest=excluded.digest,
			size_bytes=excluded.size_bytes,
			format=excluded.format,
			family=excluded.family,
			parameters=excluded.parameters,
			quantization=excluded.quantization,
			pulled_at=excluded.pulled_at,
			last_used=excluded.last_used,
			pinned=excluded.pinned`,
		info.Name, info.Digest, info.SizeBytes, info.Format,
		info.Family, info.Parameters, info.Quantization,
		info.PulledAt.Unix(), nullableUnix(info.LastUsed), info.Pinned,
	)
	return err
}

// GetModel retrieves a single model by name.
func (d *DB) GetModel(name string) (*domain.ModelInfo, error) {
	row := d.db.QueryRow(
		`SELECT name, digest, size_bytes, format, family, parameters, quantization, pulled_at, last_used, pinned
		 FROM models WHERE name = ?`, name,
	)
	return scanModel(row)
}

// ListModels returns all installed models ordered by last_used descending.
func (d *DB) ListModels() ([]domain.ModelInfo, error) {
	rows, err := d.db.Query(
		`SELECT name, digest, size_bytes, format, family, parameters, quantization, pulled_at, last_used, pinned
		 FROM models ORDER BY COALESCE(last_used, pulled_at) DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []domain.ModelInfo
	for rows.Next() {
		m, err := scanModelRows(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, *m)
	}
	return models, rows.Err()
}

// DeleteModel removes a model record.
func (d *DB) DeleteModel(name string) error {
	result, err := d.db.Exec(`DELETE FROM models WHERE name = ?`, name)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return domain.ErrModelNotFound
	}
	return nil
}

// TouchModel updates the last_used timestamp.
func (d *DB) TouchModel(name string) error {
	_, err := d.db.Exec(
		`UPDATE models SET last_used = ? WHERE name = ?`,
		time.Now().Unix(), name,
	)
	return err
}

// ─── Node Info ──────────────────────────────────────────────────────────────

// SetNodeInfo stores a key-value pair in node_info.
func (d *DB) SetNodeInfo(key, value string) error {
	_, err := d.db.Exec(
		`INSERT INTO node_info (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	)
	return err
}

// GetNodeInfo retrieves a value from node_info.
func (d *DB) GetNodeInfo(key string) (string, error) {
	var value string
	err := d.db.QueryRow(`SELECT value FROM node_info WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanModel(s scanner) (*domain.ModelInfo, error) {
	var m domain.ModelInfo
	var pulledAt int64
	var lastUsed sql.NullInt64

	err := s.Scan(&m.Name, &m.Digest, &m.SizeBytes, &m.Format,
		&m.Family, &m.Parameters, &m.Quantization,
		&pulledAt, &lastUsed, &m.Pinned)
	if err == sql.ErrNoRows {
		return nil, nil // Not found, no error
	}
	if err != nil {
		return nil, err
	}

	m.PulledAt = time.Unix(pulledAt, 0)
	if lastUsed.Valid {
		m.LastUsed = time.Unix(lastUsed.Int64, 0)
	}
	return &m, nil
}

func scanModelRows(rows *sql.Rows) (*domain.ModelInfo, error) {
	return scanModel(rows)
}

func nullableUnix(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.Unix(), Valid: true}
}
