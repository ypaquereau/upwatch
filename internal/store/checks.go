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

// Uptime summarises the checks of a target over a time window.
type Uptime struct {
	Checks    int
	Successes int
}

// Percent returns the share of successful checks, from 0 to 100. With no
// checks there is nothing to measure, so it returns 0; callers that need to
// tell "no data" from "all failed" should look at Checks.
func (u Uptime) Percent() float64 {
	if u.Checks == 0 {
		return 0
	}
	return float64(u.Successes) / float64(u.Checks) * 100
}

// TargetUptime counts the checks of a target recorded at or after since.
// The target is not required to exist: an unknown ID simply has no checks.
func (s *Store) TargetUptime(ctx context.Context, targetID int64, since time.Time) (Uptime, error) {
	var u Uptime
	err := s.pool.QueryRow(ctx,
		`SELECT count(*), count(*) FILTER (WHERE success)
		 FROM checks
		 WHERE target_id = $1 AND checked_at >= $2`,
		targetID, since,
	).Scan(&u.Checks, &u.Successes)
	if err != nil {
		return Uptime{}, fmt.Errorf("computing uptime for target %d: %w", targetID, err)
	}
	return u, nil
}
