package store

import (
	"context"
	"os"
	"testing"
)

// newTestStore connects to the database named by TEST_DATABASE_URL and wipes
// the schema, so each test starts from nothing. The test is skipped when the
// variable is unset, which keeps `go test ./...` green without Docker.
//
// Never point TEST_DATABASE_URL at a database you care about.
func newTestStore(t *testing.T) *Store {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	s, err := New(ctx, url)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(s.Close)

	if _, err := s.pool.Exec(ctx, "DROP TABLE IF EXISTS checks, targets, schema_migrations"); err != nil {
		t.Fatalf("resetting schema: %v", err)
	}
	return s
}

func TestMigrate(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Running twice proves migrations are recorded and not re-applied.
	for i := 1; i <= 2; i++ {
		if err := s.Migrate(ctx); err != nil {
			t.Fatalf("Migrate() run %d error: %v", i, err)
		}
	}

	for _, table := range []string{"targets", "checks", "schema_migrations"} {
		var exists bool
		err := s.pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists)
		if err != nil {
			t.Fatalf("checking table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s does not exist after Migrate()", table)
		}
	}

	var n int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatalf("counting migrations: %v", err)
	}
	if n != 1 {
		t.Errorf("schema_migrations has %d rows, want 1", n)
	}
}
