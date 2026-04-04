package db

import (
	"fmt"
	"time"
)

// StatuslineRecord represents a single statusline telemetry snapshot.
type StatuslineRecord struct {
	ID             int64
	Ts             time.Time
	SessionID      string
	Agent          string
	Model          string
	NumTurns       int
	CostUSD        float64
	InputTokens    int
	OutputTokens   int
	CacheCreateTok int
	CacheReadTok   int
	CWD            string
	RawJSON        string
}

// StatuslineSummary aggregates cost and token data.
type StatuslineSummary struct {
	TotalCostUSD  float64            `json:"total_cost_usd"`
	TotalInputTok int                `json:"total_input_tokens"`
	TotalOutputTok int               `json:"total_output_tokens"`
	ByModel       map[string]float64 `json:"by_model"`
}

// InsertStatusline stores a statusline event in the database.
func (d *DB) InsertStatusline(r *StatuslineRecord) error {
	_, err := d.sql.Exec(
		`INSERT INTO statusline_events
			(ts, session_id, agent, model, num_turns, cost_usd,
			 input_tokens, output_tokens, cache_create_tok, cache_read_tok,
			 cwd, raw_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Ts.UnixMilli(), r.SessionID, r.Agent, r.Model, r.NumTurns, r.CostUSD,
		r.InputTokens, r.OutputTokens, r.CacheCreateTok, r.CacheReadTok,
		r.CWD, r.RawJSON,
	)
	if err != nil {
		return fmt.Errorf("insert statusline: %w", err)
	}
	return nil
}

// QueryStatusline retrieves statusline events, optionally filtered by session and time.
func (d *DB) QueryStatusline(sessionID string, since time.Time, limit int) ([]StatuslineRecord, error) {
	query := `SELECT id, ts, session_id, agent, COALESCE(model,''), num_turns,
		COALESCE(cost_usd,0), COALESCE(input_tokens,0), COALESCE(output_tokens,0),
		COALESCE(cache_create_tok,0), COALESCE(cache_read_tok,0),
		COALESCE(cwd,''), COALESCE(raw_json,'')
		FROM statusline_events WHERE 1=1`
	var args []any

	if sessionID != "" {
		query += " AND session_id = ?"
		args = append(args, sessionID)
	}
	if !since.IsZero() {
		query += " AND ts >= ?"
		args = append(args, since.UnixMilli())
	}
	query += " ORDER BY ts DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query statusline: %w", err)
	}
	defer rows.Close()

	var results []StatuslineRecord
	for rows.Next() {
		var r StatuslineRecord
		var tsMillis int64
		if err := rows.Scan(
			&r.ID, &tsMillis, &r.SessionID, &r.Agent, &r.Model, &r.NumTurns,
			&r.CostUSD, &r.InputTokens, &r.OutputTokens,
			&r.CacheCreateTok, &r.CacheReadTok,
			&r.CWD, &r.RawJSON,
		); err != nil {
			return nil, fmt.Errorf("scan statusline row: %w", err)
		}
		r.Ts = time.UnixMilli(tsMillis)
		results = append(results, r)
	}
	return results, rows.Err()
}

// QueryStatuslineSummary computes aggregate cost/token stats since the given time.
func (d *DB) QueryStatuslineSummary(since time.Time) (*StatuslineSummary, error) {
	summary := &StatuslineSummary{
		ByModel: make(map[string]float64),
	}

	// Get totals
	query := `SELECT COALESCE(SUM(cost_usd),0), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0)
		FROM statusline_events WHERE ts >= ?`
	row := d.sql.QueryRow(query, since.UnixMilli())
	if err := row.Scan(&summary.TotalCostUSD, &summary.TotalInputTok, &summary.TotalOutputTok); err != nil {
		return nil, fmt.Errorf("query statusline summary: %w", err)
	}

	// Get by-model breakdown
	modelQuery := `SELECT COALESCE(model,'unknown'), COALESCE(SUM(cost_usd),0)
		FROM statusline_events WHERE ts >= ? GROUP BY model`
	rows, err := d.sql.Query(modelQuery, since.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("query statusline by model: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var model string
		var cost float64
		if err := rows.Scan(&model, &cost); err != nil {
			return nil, fmt.Errorf("scan model row: %w", err)
		}
		summary.ByModel[model] = cost
	}
	return summary, rows.Err()
}
