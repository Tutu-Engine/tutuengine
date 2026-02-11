package sqlite

// Phase5Migrations returns the DDL for Phase 5: Supernova — Federation + Governance.
// Called from db.go's migrate() after Phase 4 migrations.
func Phase5Migrations() []string {
	return []string{
		// ─── Federation ─────────────────────────────────────────────────

		// Federated sub-networks owned by organizations
		`CREATE TABLE IF NOT EXISTS federations (
			id                TEXT PRIMARY KEY,
			name              TEXT NOT NULL,
			admin_node_id     TEXT NOT NULL,
			status            TEXT NOT NULL DEFAULT 'ACTIVE',
			sharing_policy    TEXT NOT NULL DEFAULT 'SPARE',
			revenue_share_pct INTEGER NOT NULL DEFAULT 80,
			data_sovereignty  BOOLEAN NOT NULL DEFAULT 1,
			allowed_regions   TEXT DEFAULT '',
			created_at        INTEGER NOT NULL,
			updated_at        INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_fed_status ON federations(status)`,
		`CREATE INDEX IF NOT EXISTS idx_fed_admin ON federations(admin_node_id)`,

		// Federation membership
		`CREATE TABLE IF NOT EXISTS federation_members (
			node_id     TEXT NOT NULL,
			fed_id      TEXT NOT NULL,
			role        TEXT NOT NULL DEFAULT 'member',
			joined_at   INTEGER NOT NULL,
			last_active INTEGER NOT NULL,
			PRIMARY KEY (node_id, fed_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_fedmem_fed ON federation_members(fed_id)`,

		// ─── Governance ─────────────────────────────────────────────────

		// Governance proposals
		`CREATE TABLE IF NOT EXISTS governance_proposals (
			id          TEXT PRIMARY KEY,
			title       TEXT NOT NULL,
			description TEXT DEFAULT '',
			category    TEXT NOT NULL,
			author      TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'DRAFT',
			param_key   TEXT DEFAULT '',
			param_value TEXT DEFAULT '',
			created_at  INTEGER NOT NULL,
			opened_at   INTEGER,
			closed_at   INTEGER,
			expires_at  INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_status ON governance_proposals(status)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_author ON governance_proposals(author)`,

		// Governance votes (credit-weighted)
		`CREATE TABLE IF NOT EXISTS governance_votes (
			proposal_id TEXT NOT NULL,
			node_id     TEXT NOT NULL,
			choice      TEXT NOT NULL,
			weight      INTEGER NOT NULL,
			cast_at     INTEGER NOT NULL,
			PRIMARY KEY (proposal_id, node_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_vote_proposal ON governance_votes(proposal_id)`,

		// ─── Reputation ─────────────────────────────────────────────────

		// Node reputation scores (EMA-based)
		`CREATE TABLE IF NOT EXISTS node_reputation (
			node_id       TEXT PRIMARY KEY,
			reliability   REAL NOT NULL DEFAULT 0.5,
			accuracy      REAL NOT NULL DEFAULT 0.5,
			availability  REAL NOT NULL DEFAULT 0.5,
			speed         REAL NOT NULL DEFAULT 0.5,
			longevity     REAL NOT NULL DEFAULT 0.0,
			penalties     REAL NOT NULL DEFAULT 0.0,
			task_count    INTEGER NOT NULL DEFAULT 0,
			days_active   INTEGER NOT NULL DEFAULT 0,
			last_update   INTEGER NOT NULL,
			last_decay    INTEGER NOT NULL,
			joined_at     INTEGER NOT NULL
		)`,

		// ─── Anomaly Detection ──────────────────────────────────────────

		// Node behavioral profiles for anomaly detection
		`CREATE TABLE IF NOT EXISTS anomaly_profiles (
			node_id            TEXT PRIMARY KEY,
			duration_count     INTEGER NOT NULL DEFAULT 0,
			duration_mean      REAL NOT NULL DEFAULT 0,
			duration_m2        REAL NOT NULL DEFAULT 0,
			success_count      INTEGER NOT NULL DEFAULT 0,
			failure_count      INTEGER NOT NULL DEFAULT 0,
			cpu_count          INTEGER NOT NULL DEFAULT 0,
			cpu_mean           REAL NOT NULL DEFAULT 0,
			cpu_m2             REAL NOT NULL DEFAULT 0,
			consecutive_anom   INTEGER NOT NULL DEFAULT 0,
			total_anomalies    INTEGER NOT NULL DEFAULT 0,
			last_anomaly       INTEGER,
			last_update        INTEGER NOT NULL,
			created_at         INTEGER NOT NULL
		)`,

		// Anomaly events log (ring buffer — keep last 10K per node)
		`CREATE TABLE IF NOT EXISTS anomaly_events (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			node_id     TEXT NOT NULL,
			type        TEXT NOT NULL,
			severity    TEXT NOT NULL,
			description TEXT NOT NULL,
			timestamp   INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_anom_node ON anomaly_events(node_id)`,
		`CREATE INDEX IF NOT EXISTS idx_anom_ts ON anomaly_events(timestamp)`,

		// Threat intelligence feed
		`CREATE TABLE IF NOT EXISTS threat_feed (
			node_id     TEXT NOT NULL,
			reason      TEXT NOT NULL,
			reported_by TEXT NOT NULL,
			reported_at INTEGER NOT NULL,
			auto_banned BOOLEAN NOT NULL DEFAULT 0,
			PRIMARY KEY (node_id, reason)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_threat_ts ON threat_feed(reported_at)`,
	}
}

// ─── Federation CRUD ────────────────────────────────────────────────────────

// InsertFederation persists a new federation.
func (d *DB) InsertFederation(id, name, adminNodeID, status, sharingPolicy string, revenueSharePct int, dataSovereignty bool, allowedRegions string, createdAt, updatedAt int64) error {
	_, err := d.db.Exec(
		`INSERT INTO federations (id, name, admin_node_id, status, sharing_policy, revenue_share_pct, data_sovereignty, allowed_regions, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, adminNodeID, status, sharingPolicy, revenueSharePct, dataSovereignty, allowedRegions, createdAt, updatedAt,
	)
	return err
}

// UpdateFederationStatus changes a federation's status.
func (d *DB) UpdateFederationStatus(fedID, status string, updatedAt int64) error {
	_, err := d.db.Exec(
		`UPDATE federations SET status = ?, updated_at = ? WHERE id = ?`,
		status, updatedAt, fedID,
	)
	return err
}

// ListActiveFederations returns all non-dissolved federations.
func (d *DB) ListActiveFederations() ([]map[string]interface{}, error) {
	rows, err := d.db.Query(
		`SELECT id, name, admin_node_id, status, sharing_policy, revenue_share_pct, member_count
		 FROM federations f
		 LEFT JOIN (SELECT fed_id, COUNT(*) as member_count FROM federation_members GROUP BY fed_id) m
		 ON f.id = m.fed_id
		 WHERE f.status != 'DISSOLVED'
		 ORDER BY f.created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var id, name, admin, status, policy string
		var revPct int
		var memberCount *int
		if err := rows.Scan(&id, &name, &admin, &status, &policy, &revPct, &memberCount); err != nil {
			return nil, err
		}
		mc := 0
		if memberCount != nil {
			mc = *memberCount
		}
		results = append(results, map[string]interface{}{
			"id": id, "name": name, "admin": admin,
			"status": status, "policy": policy,
			"revenue_pct": revPct, "members": mc,
		})
	}
	return results, rows.Err()
}

// InsertFederationMember adds a node to a federation.
func (d *DB) InsertFederationMember(nodeID, fedID, role string, joinedAt, lastActive int64) error {
	_, err := d.db.Exec(
		`INSERT OR REPLACE INTO federation_members (node_id, fed_id, role, joined_at, last_active)
		 VALUES (?, ?, ?, ?, ?)`,
		nodeID, fedID, role, joinedAt, lastActive,
	)
	return err
}

// RemoveFederationMember removes a node from a federation.
func (d *DB) RemoveFederationMember(nodeID, fedID string) error {
	_, err := d.db.Exec(
		`DELETE FROM federation_members WHERE node_id = ? AND fed_id = ?`,
		nodeID, fedID,
	)
	return err
}

// FederationMemberCount returns number of members in a federation.
func (d *DB) FederationMemberCount(fedID string) (int, error) {
	var count int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM federation_members WHERE fed_id = ?`, fedID,
	).Scan(&count)
	return count, err
}

// ─── Governance CRUD ────────────────────────────────────────────────────────

// InsertProposal persists a governance proposal.
func (d *DB) InsertProposal(id, title, description, category, author, status, paramKey, paramValue string, createdAt int64) error {
	_, err := d.db.Exec(
		`INSERT INTO governance_proposals (id, title, description, category, author, status, param_key, param_value, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, title, description, category, author, status, paramKey, paramValue, createdAt,
	)
	return err
}

// UpdateProposalStatus updates a proposal's status and timestamps.
func (d *DB) UpdateProposalStatus(propID, status string, closedAt *int64) error {
	if closedAt != nil {
		_, err := d.db.Exec(
			`UPDATE governance_proposals SET status = ?, closed_at = ? WHERE id = ?`,
			status, *closedAt, propID,
		)
		return err
	}
	_, err := d.db.Exec(
		`UPDATE governance_proposals SET status = ? WHERE id = ?`,
		status, propID,
	)
	return err
}

// ListProposalsByStatus returns proposals with a given status.
func (d *DB) ListProposalsByStatus(status string) ([]map[string]interface{}, error) {
	rows, err := d.db.Query(
		`SELECT id, title, category, author, status, created_at FROM governance_proposals WHERE status = ? ORDER BY created_at DESC`,
		status,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var id, title, cat, author, st string
		var created int64
		if err := rows.Scan(&id, &title, &cat, &author, &st, &created); err != nil {
			return nil, err
		}
		results = append(results, map[string]interface{}{
			"id": id, "title": title, "category": cat,
			"author": author, "status": st, "created_at": created,
		})
	}
	return results, rows.Err()
}

// InsertVote records a governance vote.
func (d *DB) InsertVote(proposalID, nodeID, choice string, weight, castAt int64) error {
	_, err := d.db.Exec(
		`INSERT OR REPLACE INTO governance_votes (proposal_id, node_id, choice, weight, cast_at)
		 VALUES (?, ?, ?, ?, ?)`,
		proposalID, nodeID, choice, weight, castAt,
	)
	return err
}

// VoteTally returns the aggregated vote counts for a proposal.
func (d *DB) VoteTally(proposalID string) (forWeight, againstWeight, abstainWeight int64, voterCount int, err error) {
	rows, err := d.db.Query(
		`SELECT choice, SUM(weight), COUNT(*) FROM governance_votes WHERE proposal_id = ? GROUP BY choice`,
		proposalID,
	)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var choice string
		var sumWeight int64
		var count int
		if err := rows.Scan(&choice, &sumWeight, &count); err != nil {
			return 0, 0, 0, 0, err
		}
		voterCount += count
		switch choice {
		case "FOR":
			forWeight = sumWeight
		case "AGAINST":
			againstWeight = sumWeight
		case "ABSTAIN":
			abstainWeight = sumWeight
		}
	}
	return forWeight, againstWeight, abstainWeight, voterCount, rows.Err()
}

// ─── Reputation CRUD ────────────────────────────────────────────────────────

// UpsertReputation inserts or updates a node's reputation record.
func (d *DB) UpsertReputation(nodeID string, reliability, accuracy, availability, speed, longevity, penalties float64, taskCount, daysActive int, lastUpdate, lastDecay, joinedAt int64) error {
	_, err := d.db.Exec(
		`INSERT INTO node_reputation (node_id, reliability, accuracy, availability, speed, longevity, penalties, task_count, days_active, last_update, last_decay, joined_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(node_id) DO UPDATE SET
			reliability=excluded.reliability, accuracy=excluded.accuracy,
			availability=excluded.availability, speed=excluded.speed,
			longevity=excluded.longevity, penalties=excluded.penalties,
			task_count=excluded.task_count, days_active=excluded.days_active,
			last_update=excluded.last_update, last_decay=excluded.last_decay`,
		nodeID, reliability, accuracy, availability, speed, longevity, penalties, taskCount, daysActive, lastUpdate, lastDecay, joinedAt,
	)
	return err
}

// GetReputation returns a node's stored reputation scores.
func (d *DB) GetReputation(nodeID string) (reliability, accuracy, availability, speed, longevity, penalties float64, taskCount, daysActive int, err error) {
	err = d.db.QueryRow(
		`SELECT reliability, accuracy, availability, speed, longevity, penalties, task_count, days_active
		 FROM node_reputation WHERE node_id = ?`, nodeID,
	).Scan(&reliability, &accuracy, &availability, &speed, &longevity, &penalties, &taskCount, &daysActive)
	return
}

// ─── Anomaly CRUD ───────────────────────────────────────────────────────────

// InsertAnomalyEvent logs an anomaly detection event.
func (d *DB) InsertAnomalyEvent(nodeID, anomalyType, severity, description string, timestamp int64) error {
	_, err := d.db.Exec(
		`INSERT INTO anomaly_events (node_id, type, severity, description, timestamp)
		 VALUES (?, ?, ?, ?, ?)`,
		nodeID, anomalyType, severity, description, timestamp,
	)
	return err
}

// AnomalyEventCount returns total anomaly events for a node.
func (d *DB) AnomalyEventCount(nodeID string) (int, error) {
	var count int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM anomaly_events WHERE node_id = ?`, nodeID,
	).Scan(&count)
	return count, err
}

// InsertThreatEntry adds to the threat intelligence feed.
func (d *DB) InsertThreatEntry(nodeID, reason, reportedBy string, reportedAt int64, autoBanned bool) error {
	_, err := d.db.Exec(
		`INSERT OR REPLACE INTO threat_feed (node_id, reason, reported_by, reported_at, auto_banned)
		 VALUES (?, ?, ?, ?, ?)`,
		nodeID, reason, reportedBy, reportedAt, autoBanned,
	)
	return err
}

// IsNodeThreat checks if a node is in the threat feed.
func (d *DB) IsNodeThreat(nodeID string) (bool, error) {
	var count int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM threat_feed WHERE node_id = ?`, nodeID,
	).Scan(&count)
	return count > 0, err
}
