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
