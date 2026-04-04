package db

import (
	"encoding/json"
	"fmt"
	"time"
)

// ShellRecord represents a shell process in the database.
type ShellRecord struct {
	ID        string
	Name      string
	Command   []string
	CWD       string
	State     string // running, stopped, killed
	StartedAt time.Time
	StoppedAt *time.Time
	ExitCode  *int
}

// InsertShell stores a new shell record.
func (d *DB) InsertShell(r *ShellRecord) error {
	cmdJSON, err := json.Marshal(r.Command)
	if err != nil {
		return fmt.Errorf("marshal command: %w", err)
	}
	_, err = d.sql.Exec(
		`INSERT INTO shells (id, name, command, cwd, state, started_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		r.ID, r.Name, string(cmdJSON), r.CWD, r.State, r.StartedAt.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("insert shell: %w", err)
	}
	return nil
}

// UpdateShellState updates a shell's state and optional exit code.
func (d *DB) UpdateShellState(id, state string, exitCode *int) error {
	now := time.Now().UnixMilli()
	_, err := d.sql.Exec(
		`UPDATE shells SET state = ?, stopped_at = ?, exit_code = ? WHERE id = ?`,
		state, now, exitCode, id,
	)
	if err != nil {
		return fmt.Errorf("update shell state: %w", err)
	}
	return nil
}

// ListShells returns shell records, optionally including stopped ones.
func (d *DB) ListShells(includeStopped bool) ([]ShellRecord, error) {
	query := `SELECT id, COALESCE(name,''), command, COALESCE(cwd,''), state, started_at, stopped_at, exit_code
		FROM shells`
	if !includeStopped {
		query += " WHERE state = 'running'"
	}
	query += " ORDER BY started_at DESC"

	rows, err := d.sql.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list shells: %w", err)
	}
	defer rows.Close()

	var results []ShellRecord
	for rows.Next() {
		var r ShellRecord
		var cmdJSON string
		var startedMillis int64
		var stoppedMillis *int64
		if err := rows.Scan(&r.ID, &r.Name, &cmdJSON, &r.CWD, &r.State, &startedMillis, &stoppedMillis, &r.ExitCode); err != nil {
			return nil, fmt.Errorf("scan shell row: %w", err)
		}
		if err := json.Unmarshal([]byte(cmdJSON), &r.Command); err != nil {
			r.Command = []string{cmdJSON} // fallback
		}
		r.StartedAt = time.UnixMilli(startedMillis)
		if stoppedMillis != nil {
			t := time.UnixMilli(*stoppedMillis)
			r.StoppedAt = &t
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// GetShell returns a single shell record by ID.
func (d *DB) GetShell(id string) (*ShellRecord, error) {
	var r ShellRecord
	var cmdJSON string
	var startedMillis int64
	var stoppedMillis *int64
	err := d.sql.QueryRow(
		`SELECT id, COALESCE(name,''), command, COALESCE(cwd,''), state, started_at, stopped_at, exit_code
		FROM shells WHERE id = ?`, id,
	).Scan(&r.ID, &r.Name, &cmdJSON, &r.CWD, &r.State, &startedMillis, &stoppedMillis, &r.ExitCode)
	if err != nil {
		return nil, fmt.Errorf("get shell %s: %w", id, err)
	}
	if err := json.Unmarshal([]byte(cmdJSON), &r.Command); err != nil {
		r.Command = []string{cmdJSON}
	}
	r.StartedAt = time.UnixMilli(startedMillis)
	if stoppedMillis != nil {
		t := time.UnixMilli(*stoppedMillis)
		r.StoppedAt = &t
	}
	return &r, nil
}
