//go:build cgo

package storage

import (
	"context"
	"testing"
	"time"

	"diskmon/internal/health"
	"diskmon/internal/smart"
)

func TestPruneSamplesDisabledForZeroRetention(t *testing.T) {
	db := openTestDuckDB(t)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	deleted, err := db.PruneSamples(ctx, 0, time.Now())
	if err != nil {
		t.Fatalf("PruneSamples returned error: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deleted with zero retention, got %d", deleted)
	}
}

func TestPruneSamplesDeletesOldSamplesButPreservesLatest(t *testing.T) {
	db := openTestDuckDB(t)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	seenAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	insertTestDrive(t, db, 1, "/dev/disk1", seenAt)

	info := smart.DriveInfo{Device: "/dev/disk1"}
	// Insert an old sample (10 days ago) and a recent one (1 hour ago).
	oldTime := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	recentTime := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)

	if _, _, err := db.InsertSample(ctx, info, smart.SmartSample{CollectedAt: oldTime, RawJSON: `{}`}, health.Result{Status: health.StatusGreen}); err != nil {
		t.Fatalf("insert old sample: %v", err)
	}
	if _, _, err := db.InsertSample(ctx, info, smart.SmartSample{CollectedAt: recentTime, RawJSON: `{}`}, health.Result{Status: health.StatusGreen}); err != nil {
		t.Fatalf("insert recent sample: %v", err)
	}

	var countBefore int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM smart_samples WHERE drive_id = 1`).Scan(&countBefore); err != nil {
		t.Fatalf("count before: %v", err)
	}
	if countBefore != 2 {
		t.Fatalf("expected 2 samples before prune, got %d", countBefore)
	}

	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	// 5-day retention: the old sample (11 days old) should be pruned.
	deleted, err := db.PruneSamples(ctx, 5*24*time.Hour, now)
	if err != nil {
		t.Fatalf("PruneSamples returned error: %v", err)
	}
	if deleted == 0 {
		t.Fatal("expected at least 1 row deleted")
	}

	var countAfter int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM smart_samples WHERE drive_id = 1`).Scan(&countAfter); err != nil {
		t.Fatalf("count after: %v", err)
	}
	if countAfter != 1 {
		t.Fatalf("expected 1 sample after prune (latest preserved), got %d", countAfter)
	}

	var remainingTime time.Time
	if err := db.db.QueryRowContext(ctx, `SELECT collected_at FROM smart_samples WHERE drive_id = 1`).Scan(&remainingTime); err != nil {
		t.Fatalf("query remaining sample: %v", err)
	}
	if !remainingTime.Equal(recentTime) {
		t.Fatalf("expected remaining sample at %s, got %s", recentTime, remainingTime)
	}
}
