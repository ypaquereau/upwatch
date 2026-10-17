package store

import (
	"context"
	"errors"
	"testing"
)

// newMigratedStore returns a store with an empty, up-to-date schema.
func newMigratedStore(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}
	return s
}

func TestCreateTarget(t *testing.T) {
	ctx := context.Background()
	s := newMigratedStore(t)

	got, err := s.CreateTarget(ctx, "Example", "https://example.com", 30)
	if err != nil {
		t.Fatalf("CreateTarget() error: %v", err)
	}
	if got.ID == 0 || got.CreatedAt.IsZero() {
		t.Errorf("generated fields not filled: %+v", got)
	}
	if got.Name != "Example" || got.URL != "https://example.com" || got.IntervalS != 30 {
		t.Errorf("CreateTarget() = %+v, input fields not preserved", got)
	}

	t.Run("duplicate url", func(t *testing.T) {
		_, err := s.CreateTarget(ctx, "Other name", "https://example.com", 30)
		if !errors.Is(err, ErrDuplicate) {
			t.Errorf("error = %v, want ErrDuplicate", err)
		}
	})

	t.Run("interval below minimum", func(t *testing.T) {
		_, err := s.CreateTarget(ctx, "Fast", "https://fast.example.com", 5)
		if err == nil {
			t.Fatal("error = nil, want a CHECK constraint violation")
		}
		if errors.Is(err, ErrDuplicate) {
			t.Errorf("error = %v, must not be reported as a duplicate", err)
		}
	})
}
