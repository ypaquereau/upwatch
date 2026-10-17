package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Sentinel errors let callers react (HTTP 404/409) without importing pgx.
var (
	// ErrNotFound is returned when the requested row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrDuplicate is returned when a unique constraint is violated.
	ErrDuplicate = errors.New("already exists")
)

// pgUniqueViolation is the SQLSTATE code PostgreSQL raises on a unique violation.
const pgUniqueViolation = "23505"

// Target is a URL being monitored.
type Target struct {
	ID        int64
	Name      string
	URL       string
	IntervalS int
	CreatedAt time.Time
}

// CreateTarget inserts a target and returns it with its generated ID and
// creation time. It returns ErrDuplicate if the URL is already monitored.
// Defaults and input validation belong to the caller; the database CHECK
// constraints are only a safety net.
func (s *Store) CreateTarget(ctx context.Context, name, url string, intervalS int) (Target, error) {
	t := Target{Name: name, URL: url, IntervalS: intervalS}

	err := s.pool.QueryRow(ctx,
		`INSERT INTO targets (name, url, interval_s)
		 VALUES ($1, $2, $3)
		 RETURNING id, created_at`,
		name, url, intervalS,
	).Scan(&t.ID, &t.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return Target{}, fmt.Errorf("creating target %q: %w", url, ErrDuplicate)
		}
		return Target{}, fmt.Errorf("creating target %q: %w", url, err)
	}
	return t, nil
}

// GetTarget returns the target with the given ID, or ErrNotFound.
func (s *Store) GetTarget(ctx context.Context, id int64) (Target, error) {
	var t Target
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, url, interval_s, created_at FROM targets WHERE id = $1`, id,
	).Scan(&t.ID, &t.Name, &t.URL, &t.IntervalS, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, fmt.Errorf("getting target %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return Target{}, fmt.Errorf("getting target %d: %w", id, err)
	}
	return t, nil
}

// DeleteTarget removes a target and, through ON DELETE CASCADE, its checks.
// It returns ErrNotFound if no such target exists.
func (s *Store) DeleteTarget(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM targets WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting target %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("deleting target %d: %w", id, ErrNotFound)
	}
	return nil
}

// TargetStatus is a target together with its most recent check, if any.
type TargetStatus struct {
	Target
	LastCheck *Check
}

// ListTargets returns every target ordered by ID, each with its latest check
// (nil when it has never been checked).
//
// The LATERAL subquery picks the newest check per target and is served by the
// (target_id, checked_at DESC) index; LEFT JOIN keeps targets with no checks.
func (s *Store) ListTargets(ctx context.Context) ([]TargetStatus, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.name, t.url, t.interval_s, t.created_at,
		       c.id, c.checked_at, c.status_code, c.latency_ms, c.success, c.error
		FROM targets t
		LEFT JOIN LATERAL (
			SELECT id, checked_at, status_code, latency_ms, success, error
			FROM checks
			WHERE target_id = t.id
			ORDER BY checked_at DESC
			LIMIT 1
		) c ON true
		ORDER BY t.id`)
	if err != nil {
		return nil, fmt.Errorf("listing targets: %w", err)
	}
	defer rows.Close()

	statuses := []TargetStatus{}
	for rows.Next() {
		var (
			ts        TargetStatus
			checkID   *int64
			checkedAt *time.Time
			code      *int
			latency   *int
			success   *bool
			errMsg    *string
		)
		if err := rows.Scan(
			&ts.ID, &ts.Name, &ts.URL, &ts.IntervalS, &ts.CreatedAt,
			&checkID, &checkedAt, &code, &latency, &success, &errMsg,
		); err != nil {
			return nil, fmt.Errorf("scanning target: %w", err)
		}
		if checkID != nil { // the LEFT JOIN found a check
			ts.LastCheck = &Check{
				ID: *checkID, TargetID: ts.ID, CheckedAt: *checkedAt,
				StatusCode: code, LatencyMS: latency, Success: *success, Error: errMsg,
			}
		}
		statuses = append(statuses, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating targets: %w", err)
	}
	return statuses, nil
}

// DueTargets returns the targets that must be checked now: those never
// checked, or whose latest check is at least interval_s seconds old.
// The worker polls this every second, so it relies on the same
// (target_id, checked_at DESC) index as ListTargets.
func (s *Store) DueTargets(ctx context.Context) ([]Target, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.name, t.url, t.interval_s, t.created_at
		FROM targets t
		LEFT JOIN LATERAL (
			SELECT checked_at
			FROM checks
			WHERE target_id = t.id
			ORDER BY checked_at DESC
			LIMIT 1
		) c ON true
		WHERE c.checked_at IS NULL
		   OR c.checked_at <= now() - t.interval_s * interval '1 second'
		ORDER BY t.id`)
	if err != nil {
		return nil, fmt.Errorf("listing due targets: %w", err)
	}
	defer rows.Close()

	targets := []Target{}
	for rows.Next() {
		var t Target
		if err := rows.Scan(&t.ID, &t.Name, &t.URL, &t.IntervalS, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning due target: %w", err)
		}
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating due targets: %w", err)
	}
	return targets, nil
}
