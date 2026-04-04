package db

import "fmt"

// migrations is an ordered list of SQL statements. Each entry is applied once,
// tracked by the schema_version table. New migrations are appended at the end.
var migrations = []string{
	// v1: statusline events
	`CREATE TABLE IF NOT EXISTS statusline_events (
		id               INTEGER PRIMARY KEY,
		ts               INTEGER NOT NULL,
		session_id       TEXT NOT NULL,
		agent            TEXT NOT NULL DEFAULT 'claude',
		model            TEXT,
		num_turns        INTEGER,
		cost_usd         REAL,
		input_tokens     INTEGER,
		output_tokens    INTEGER,
		cache_create_tok INTEGER,
		cache_read_tok   INTEGER,
		cwd              TEXT,
		raw_json         TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_sl_session ON statusline_events(session_id, ts);
	CREATE INDEX IF NOT EXISTS idx_sl_ts ON statusline_events(ts);`,

	// v2: shells
	`CREATE TABLE IF NOT EXISTS shells (
		id          TEXT PRIMARY KEY,
		name        TEXT,
		command     TEXT NOT NULL,
		cwd         TEXT,
		state       TEXT NOT NULL DEFAULT 'running',
		started_at  INTEGER NOT NULL,
		stopped_at  INTEGER,
		exit_code   INTEGER
	);`,
}

func (d *DB) migrate() error {
	// Ensure schema_version table exists
	if _, err := d.sql.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var current int
	row := d.sql.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version")
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	for i := current; i < len(migrations); i++ {
		tx, err := d.sql.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_version (version) VALUES (?)", i+1); err != nil {
			tx.Rollback()
			return fmt.Errorf("update schema version %d: %w", i+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", i+1, err)
		}
	}
	return nil
}
