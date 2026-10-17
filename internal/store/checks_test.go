package store

import (
	"context"
	"testing"
	"time"
)

func intPtr(n int) *int             { return &n }
func strPtr(s string) *string       { return &s }
func ago(d time.Duration) time.Time { return time.Now().Add(-d).UTC().Truncate(time.Microsecond) }

// mustTarget creates a target with a unique URL derived from name.
func mustTarget(t *testing.T, s *Store, name string, intervalS int) Target {
	t.Helper()
	tg, err := s.CreateTarget(context.Background(), name, "https://"+name+".example.com", intervalS)
	if err != nil {
		t.Fatalf("CreateTarget(%s) error: %v", name, err)
	}
	return tg
}

func TestRecordAndRecentChecks(t *testing.T) {
	ctx := context.Background()
	s := newMigratedStore(t)
	tg := mustTarget(t, s, "a", 30)

	oldest := Check{TargetID: tg.ID, CheckedAt: ago(3 * time.Minute), StatusCode: intPtr(200), LatencyMS: intPtr(12), Success: true}
	middle := Check{TargetID: tg.ID, CheckedAt: ago(2 * time.Minute), StatusCode: intPtr(500), LatencyMS: intPtr(30), Success: false}
	newest := Check{TargetID: tg.ID, CheckedAt: ago(1 * time.Minute), Success: false, Error: strPtr("dial tcp: no such host")}
	for _, c := range []Check{middle, oldest, newest} { // inserted out of order on purpose
		if err := s.RecordCheck(ctx, c); err != nil {
			t.Fatalf("RecordCheck() error: %v", err)
		}
	}

	got, err := s.RecentChecks(ctx, tg.ID, 2)
	if err != nil {
		t.Fatalf("RecentChecks() error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (limit)", len(got))
	}
	if !got[0].CheckedAt.Equal(newest.CheckedAt) || !got[1].CheckedAt.Equal(middle.CheckedAt) {
		t.Errorf("not ordered newest first: %v, %v", got[0].CheckedAt, got[1].CheckedAt)
	}

	// NULL columns must come back as nil pointers.
	if got[0].StatusCode != nil || got[0].LatencyMS != nil {
		t.Errorf("network error check should have nil status/latency: %+v", got[0])
	}
	if got[0].Error == nil || *got[0].Error != "dial tcp: no such host" {
		t.Errorf("error message not preserved: %v", got[0].Error)
	}
	if got[1].StatusCode == nil || *got[1].StatusCode != 500 {
		t.Errorf("status code not preserved: %v", got[1].StatusCode)
	}

	empty, err := s.RecentChecks(ctx, tg.ID+1000, 20)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("unknown target: got (%v, %v), want empty non-nil slice and nil error", empty, err)
	}
}
