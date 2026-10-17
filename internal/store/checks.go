package store

import (
	"context"
	"fmt"
	"time"
)

// Check is the result of one probe of a target. StatusCode, LatencyMS and
// Error are pointers because the columns are NULL when they do not apply
// (e.g. no status code on a network error).
type Check struct {
	ID         int64
	TargetID   int64
	CheckedAt  time.Time
	StatusCode *int
	LatencyMS  *int
	Success    bool
	Error      *string
}

// RecordCheck stores a check result. The caller sets CheckedAt (the worker
// uses the time the probe started), which keeps the store deterministic and
// easy to test.
func (s *Store) RecordCheck(ctx context.Context, c Check) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO checks (target_id, checked_at, status_code, latency_ms, success, error)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		c.TargetID, c.CheckedAt, c.StatusCode, c.LatencyMS, c.Success, c.Error,
	)
	if err != nil {
		return fmt.Errorf("recording check for target %d: %w", c.TargetID, err)
	}
	return nil
}

// RecentChecks returns up to limit checks of a target, newest first. It
// returns an empty slice, not an error, when the target has no checks.
func (s *Store) RecentChecks(ctx context.Context, targetID int64, limit int) ([]Check, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, target_id, checked_at, status_code, latency_ms, success, error
		 FROM checks
		 WHERE target_id = $1
		 ORDER BY checked_at DESC
		 LIMIT $2`,
		targetID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing checks for target %d: %w", targetID, err)
	}
	defer rows.Close()

	checks := []Check{}
	for rows.Next() {
		var c Check
		if err := rows.Scan(&c.ID, &c.TargetID, &c.CheckedAt, &c.StatusCode, &c.LatencyMS, &c.Success, &c.Error); err != nil {
			return nil, fmt.Errorf("scanning check: %w", err)
		}
		checks = append(checks, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating checks: %w", err)
	}
	return checks, nil
}
