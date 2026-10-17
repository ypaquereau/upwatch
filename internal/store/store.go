// Package store is the only package that talks to PostgreSQL.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store gives access to the application data through a shared connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// New opens a connection pool and checks that the database answers, so a
// misconfigured instance fails at startup instead of on the first request.
func New(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	// pgxpool.New is lazy: only a ping proves the database is reachable.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return &Store{pool: pool}, nil
}

// Ping reports whether the database is reachable (used by the readiness probe).
func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("pinging database: %w", err)
	}
	return nil
}

// Close releases all pooled connections.
func (s *Store) Close() {
	s.pool.Close()
}
