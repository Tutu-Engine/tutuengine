package sqlite

// Phase6Migrations returns the DDL for Phase 6: Singularity — Self-Organizing Network.
// Called from db.go's migrate() after Phase 5 migrations.
//
// Tables:
//   - ml_scheduler_observations: UCB1 bandit reward observations
//   - scaling_decisions:         predictive scaler decisions
//   - healing_incidents:         autonomous incident lifecycle
//   - model_placements:          intelligence placement recommendations
//   - model_retirement_log:      retired model history
func Phase6Migrations() []string {
	return []string{
		// ─── ML Scheduler ───────────────────────────────────────────────

		// Observations from the UCB1 multi-armed bandit
		`CREATE TABLE IF NOT EXISTS ml_scheduler_observations (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			arm_key    TEXT NOT NULL,
			node_id    TEXT NOT NULL,
			reward     REAL NOT NULL,
			latency_ms REAL NOT NULL,
			credit_cost REAL NOT NULL,
			recorded_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mlobs_arm ON ml_scheduler_observations(arm_key)`,
		`CREATE INDEX IF NOT EXISTS idx_mlobs_node ON ml_scheduler_observations(node_id)`,
		`CREATE INDEX IF NOT EXISTS idx_mlobs_time ON ml_scheduler_observations(recorded_at)`,

		// ─── Predictive Scaling ─────────────────────────────────────────

		// Scaling decisions made by the predictive auto-scaler
		`CREATE TABLE IF NOT EXISTS scaling_decisions (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			direction        TEXT NOT NULL,
			current_capacity INTEGER NOT NULL,
			target_capacity  INTEGER NOT NULL,
			forecast_demand  REAL NOT NULL,
			confidence       REAL NOT NULL,
			proactive        BOOLEAN NOT NULL DEFAULT 0,
			reason           TEXT DEFAULT '',
			decided_at       INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_scale_dir ON scaling_decisions(direction)`,
		`CREATE INDEX IF NOT EXISTS idx_scale_time ON scaling_decisions(decided_at)`,

		// ─── Self-Healing ───────────────────────────────────────────────

		// Autonomous incident lifecycle tracking
		`CREATE TABLE IF NOT EXISTS healing_incidents (
			id              TEXT PRIMARY KEY,
			node_id         TEXT NOT NULL,
			failure_type    TEXT NOT NULL,
			state           TEXT NOT NULL,
			attempts        INTEGER NOT NULL DEFAULT 0,
			drained_tasks   INTEGER NOT NULL DEFAULT 0,
			actions_done    TEXT DEFAULT '',
			error           TEXT DEFAULT '',
			mttr_ms         INTEGER DEFAULT 0,
			detected_at     INTEGER NOT NULL,
			isolated_at     INTEGER,
			remediated_at   INTEGER,
			verified_at     INTEGER,
			resolved_at     INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_heal_node ON healing_incidents(node_id)`,
		`CREATE INDEX IF NOT EXISTS idx_heal_state ON healing_incidents(state)`,
		`CREATE INDEX IF NOT EXISTS idx_heal_type ON healing_incidents(failure_type)`,

		// ─── Network Intelligence ───────────────────────────────────────

		// Model placement recommendations
		`CREATE TABLE IF NOT EXISTS model_placements (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			rec_type   TEXT NOT NULL,
			model_name TEXT NOT NULL,
			from_node  TEXT DEFAULT '',
			to_node    TEXT DEFAULT '',
			score      REAL NOT NULL,
			reason     TEXT DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_place_model ON model_placements(model_name)`,
		`CREATE INDEX IF NOT EXISTS idx_place_time ON model_placements(created_at)`,

		// Retired model history
		`CREATE TABLE IF NOT EXISTS model_retirement_log (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			model_name    TEXT NOT NULL,
			last_requested INTEGER NOT NULL,
			days_since_use INTEGER NOT NULL,
			size_bytes    INTEGER DEFAULT 0,
			reason        TEXT DEFAULT '',
			retired_at    INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_retire_model ON model_retirement_log(model_name)`,
		`CREATE INDEX IF NOT EXISTS idx_retire_time ON model_retirement_log(retired_at)`,
	}
}
