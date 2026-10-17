package store

import (
	"context"
	"errors"
	"testing"
	"time"
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

func TestGetTarget(t *testing.T) {
	ctx := context.Background()
	s := newMigratedStore(t)

	created, err := s.CreateTarget(ctx, "Example", "https://example.com", 30)
	if err != nil {
		t.Fatalf("CreateTarget() error: %v", err)
	}

	got, err := s.GetTarget(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTarget() error: %v", err)
	}
	if got != created {
		t.Errorf("GetTarget() = %+v, want %+v", got, created)
	}

	_, err = s.GetTarget(ctx, created.ID+1000)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: error = %v, want ErrNotFound", err)
	}
}

func TestDeleteTarget(t *testing.T) {
	ctx := context.Background()
	s := newMigratedStore(t)

	created, err := s.CreateTarget(ctx, "Example", "https://example.com", 30)
	if err != nil {
		t.Fatalf("CreateTarget() error: %v", err)
	}

	if err := s.DeleteTarget(ctx, created.ID); err != nil {
		t.Fatalf("DeleteTarget() error: %v", err)
	}
	if _, err := s.GetTarget(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete: error = %v, want ErrNotFound", err)
	}
	if err := s.DeleteTarget(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: error = %v, want ErrNotFound", err)
	}
}

func TestListTargets(t *testing.T) {
	ctx := context.Background()
	s := newMigratedStore(t)

	empty, err := s.ListTargets(ctx)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("no targets: got (%v, %v), want empty non-nil slice and nil error", empty, err)
	}

	a := mustTarget(t, s, "a", 30)
	b := mustTarget(t, s, "b", 30)
	for _, c := range []Check{
		{TargetID: a.ID, CheckedAt: ago(2 * time.Minute), StatusCode: intPtr(500), LatencyMS: intPtr(9), Success: false},
		{TargetID: a.ID, CheckedAt: ago(1 * time.Minute), StatusCode: intPtr(200), LatencyMS: intPtr(7), Success: true},
	} {
		if err := s.RecordCheck(ctx, c); err != nil {
			t.Fatalf("RecordCheck() error: %v", err)
		}
	}

	got, err := s.ListTargets(ctx)
	if err != nil {
		t.Fatalf("ListTargets() error: %v", err)
	}
	if len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID {
		t.Fatalf("unexpected targets: %+v", got)
	}
	if got[0].LastCheck == nil || !got[0].LastCheck.Success || *got[0].LastCheck.LatencyMS != 7 {
		t.Errorf("a.LastCheck = %+v, want the newest (successful) check", got[0].LastCheck)
	}
	if got[1].LastCheck != nil {
		t.Errorf("b.LastCheck = %+v, want nil (never checked)", got[1].LastCheck)
	}
}

func TestDueTargets(t *testing.T) {
	ctx := context.Background()
	s := newMigratedStore(t)

	never := mustTarget(t, s, "never", 30)
	stale := mustTarget(t, s, "stale", 30)
	fresh := mustTarget(t, s, "fresh", 30)
	for _, c := range []Check{
		{TargetID: stale.ID, CheckedAt: ago(60 * time.Second), Success: true},
		{TargetID: fresh.ID, CheckedAt: ago(5 * time.Second), Success: true},
	} {
		if err := s.RecordCheck(ctx, c); err != nil {
			t.Fatalf("RecordCheck() error: %v", err)
		}
	}

	got, err := s.DueTargets(ctx)
	if err != nil {
		t.Fatalf("DueTargets() error: %v", err)
	}
	if len(got) != 2 || got[0].ID != never.ID || got[1].ID != stale.ID {
		t.Errorf("DueTargets() = %+v, want [never, stale] (fresh was checked 5s ago)", got)
	}
}
