package store

import (
	"context"
	"embed"
	"fmt"
	"path"

	"github.com/jackc/pgx/v5"
)

// The SQL files are compiled into the binary, so the runtime image only needs
// the executable.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationsLockID is an arbitrary key for the PostgreSQL advisory lock that
// serialises concurrent migrators (e.g. two pods starting during a rollout).
const migrationsLockID = 727274

// Migrate applies, in filename order, the embedded migrations that have not
// been applied yet. Everything runs in a single transaction: either all
// pending migrations are applied and recorded, or none are.
func (s *Store) Migrate(ctx context.Context) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("listing migrations: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning migration transaction: %w", err)
	}
	// Rollback is a no-op once the transaction is committed.
	defer func() { _ = tx.Rollback(ctx) }()

	// Held until the transaction ends. Other migrators wait here, then see our
	// schema_migrations rows and skip what is already applied.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationsLockID); err != nil {
		return fmt.Errorf("acquiring migration lock: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT        PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	for _, e := range entries {
		version := e.Name()
		if err := applyMigration(ctx, tx, version); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing migrations: %w", err)
	}
	return nil
}

// applyMigration runs one embedded file unless its version is already recorded.
func applyMigration(ctx context.Context, tx pgx.Tx, version string) error {
	var applied bool
	err := tx.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", version,
	).Scan(&applied)
	if err != nil {
		return fmt.Errorf("checking migration %s: %w", version, err)
	}
	if applied {
		return nil
	}

	sql, err := migrationsFS.ReadFile(path.Join("migrations", version))
	if err != nil {
		return fmt.Errorf("reading migration %s: %w", version, err)
	}
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		return fmt.Errorf("applying migration %s: %w", version, err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
		return fmt.Errorf("recording migration %s: %w", version, err)
	}
	return nil
}
