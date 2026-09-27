//go:build cgo

package storage

import (
	"context"
	"database/sql"
	"path/filepath"
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

func TestTierSchemaMigrationPreservesExistingForeignKeyTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.duckdb")
	db, err := OpenDuckDB(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	insertTestDrive(t, db, 1, "/dev/existing", now)
	temp := 20
	id, _, err := db.InsertSample(ctx, smart.DriveInfo{Device: "/dev/existing"}, smart.SmartSample{CollectedAt: now.Add(-30 * 24 * time.Hour), Temperature: &temp, RawJSON: `{}`, Attributes: []smart.SmartAttribute{{AttributeID: 5, Name: "Attr", Value: 10}}}, health.Result{Status: health.StatusRed, Score: 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.InsertSample(ctx, smart.DriveInfo{Device: "/dev/existing"}, smart.SmartSample{CollectedAt: now, RawJSON: `{}`}, health.Result{Status: health.StatusGreen, Score: 95}); err != nil {
		t.Fatal(err)
	}
	// Model an existing database from the previous rollup schema: these tables
	// lack temp_count, while the large raw/child tables and their FKs remain.
	for _, table := range []string{"smart_rollup_pending_samples", "smart_daily_rollups", "smart_hourly_rollups"} {
		if _, err := db.db.ExecContext(ctx, "DROP TABLE "+table); err != nil {
			t.Fatal(err)
		}
	}
	legacyRollup := `(drive_id BIGINT NOT NULL,bucket_start TIMESTAMP NOT NULL,bucket_end TIMESTAMP NOT NULL,sample_count BIGINT NOT NULL,temp_min DOUBLE,temp_max DOUBLE,temp_avg DOUBLE,temp_last INTEGER,power_on_hours_last BIGINT,reallocated_max BIGINT,reallocated_last BIGINT,pending_max BIGINT,pending_last BIGINT,uncorrectable_max BIGINT,uncorrectable_last BIGINT,wear_min BIGINT,wear_last BIGINT,health_status TEXT,health_score INTEGER,last_sample_at TIMESTAMP NOT NULL,last_sample_id BIGINT NOT NULL,PRIMARY KEY(drive_id,bucket_start))`
	for _, table := range []string{"smart_hourly_rollups", "smart_daily_rollups"} {
		if _, err := db.db.ExecContext(ctx, "CREATE TABLE "+table+" "+legacyRollup); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.db.ExecContext(ctx, `CREATE TABLE smart_rollup_pending_samples(sample_id BIGINT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = OpenDuckDB(path)
	if err != nil {
		t.Fatalf("migrate existing db: %v", err)
	}
	defer db.Close()
	var migratedTempCount int64
	if err := db.db.QueryRowContext(ctx, `SELECT temp_count FROM smart_hourly_rollups LIMIT 1`).Scan(&migratedTempCount); err != sql.ErrNoRows {
		t.Fatalf("legacy rollup temp_count migration query=%v", err)
	}
	var attr, healthRows int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM smart_attributes WHERE sample_id=?`, id).Scan(&attr); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM drive_health WHERE sample_id=?`, id).Scan(&healthRows); err != nil {
		t.Fatal(err)
	}
	if attr != 1 || healthRows != 1 {
		t.Fatalf("migration changed existing rows: attrs=%d health=%d", attr, healthRows)
	}
	policy := TierPolicy{RawRetention: 28 * 24 * time.Hour, HourlyRetention: 180 * 24 * time.Hour, DailyRetention: 5 * 365 * 24 * time.Hour}
	if _, err := db.TierMaintenance(ctx, now, policy); err != nil {
		t.Fatalf("maintenance with migrated legacy FKs: %v", err)
	}
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM smart_samples WHERE id=?`, id).Scan(&attr); err != nil {
		t.Fatal(err)
	}
	if attr != 0 {
		t.Fatalf("old raw sample not replaced after FK-preserving migration: %d", attr)
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

func TestTierMaintenanceRollsRawSamplesAndKeepsLatest(t *testing.T) {
	db := openTestDuckDB(t)
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	intPointer := func(v int) *int { return &v }
	insertTestDrive(t, db, 1, "/dev/tier", now)
	info := smart.DriveInfo{Device: "/dev/tier"}
	for i := 0; i < 3; i++ {
		at := now.Add(-30 * 24 * time.Hour).Add(time.Duration(i) * time.Minute)
		_, _, err := db.InsertSample(ctx, info, smart.SmartSample{CollectedAt: at, Temperature: intPointer(30 + i), RawJSON: `{}`, Attributes: []smart.SmartAttribute{{AttributeID: 5, Name: "Attr", Value: 10}}}, health.Result{Status: health.StatusGreen, Score: 95})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, _, err := db.InsertSample(ctx, info, smart.SmartSample{CollectedAt: now, Temperature: intPointer(40), RawJSON: `{}`, Attributes: []smart.SmartAttribute{{AttributeID: 5, Name: "Attr", Value: 20}}}, health.Result{Status: health.StatusGreen, Score: 95})
	if err != nil {
		t.Fatal(err)
	}
	policy := TierPolicy{RawRetention: 28 * 24 * time.Hour, HourlyRetention: 180 * 24 * time.Hour, DailyRetention: 5 * 365 * 24 * time.Hour, MaxSamplesPerBucket: 2}
	result, err := db.TierMaintenance(ctx, now, policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.RawSamplesRolled != 2 || result.RawSamplesRemaining != 1 {
		t.Fatalf("unexpected bounded result: %+v", result)
	}
	if result.PendingSamplesDeleted != 2 || result.PendingRowsRemaining != 0 {
		t.Fatalf("pending cleanup not reported: %+v", result)
	}
	var n int
	if err := db.db.QueryRowContext(ctx, `SELECT sample_count FROM smart_hourly_rollups`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("rollup count=%d want 2", n)
	}
	var samples, children int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM smart_samples`).Scan(&samples); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM drive_health`).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if samples != 2 || children != 2 {
		t.Fatalf("latest/remaining children lost: samples=%d health=%d", samples, children)
	}
	var attributes int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM smart_attributes`).Scan(&attributes); err != nil {
		t.Fatal(err)
	}
	if attributes != 2 {
		t.Fatalf("raw attribute rows=%d want remaining raw and latest sample attributes", attributes)
	}
}

func TestTierMaintenanceRollsWholeDayInBoundedWeightedBatches(t *testing.T) {
	db := openTestDuckDB(t)
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	insertTestDrive(t, db, 1, "/dev/day", now)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, row := range []struct {
		count, tempCount int
		avg, min, max    float64
		status           string
		score            int
	}{
		{2, 2, 20, 10, 30, "GREEN", 95}, {3, 1, 50, 50, 50, "RED", 20}, {1, 1, 40, 40, 40, "YELLOW", 65},
	} {
		_, err := db.db.ExecContext(ctx, `INSERT INTO smart_hourly_rollups(drive_id,bucket_start,bucket_end,sample_count,temp_count,temp_min,temp_max,temp_avg,temp_last,health_status,health_score,last_sample_at,last_sample_id) VALUES(1,?,?,?,?,?,?,?,?,?,?,?,?)`, day.Add(time.Duration(i)*time.Hour), day.Add(time.Duration(i+1)*time.Hour), row.count, row.tempCount, row.min, row.max, row.avg, int(row.avg), row.status, row.score, day.Add(time.Duration(i)*time.Hour), int64(i+10))
		if err != nil {
			t.Fatal(err)
		}
	}
	policy := TierPolicy{RawRetention: 24 * time.Hour, HourlyRetention: 10 * 24 * time.Hour, DailyRetention: 5 * 365 * 24 * time.Hour, MaxSamplesPerBucket: 1}
	for i := 0; i < 3; i++ {
		r, err := db.TierMaintenance(ctx, now, policy)
		if err != nil {
			t.Fatal(err)
		}
		if i < 2 && r.HourlyRowsRolled != 1 {
			t.Fatalf("pass %d rolled %d hourly rows", i, r.HourlyRowsRolled)
		}
	}
	var n, tempN int
	var avg, min, max float64
	var status string
	var score int
	if err := db.db.QueryRowContext(ctx, `SELECT sample_count,temp_count,temp_avg,temp_min,temp_max,health_status,health_score FROM smart_daily_rollups WHERE drive_id=1`).Scan(&n, &tempN, &avg, &min, &max, &status, &score); err != nil {
		t.Fatal(err)
	}
	if n != 6 || tempN != 4 || avg != 32.5 || min != 10 || max != 50 || status != "RED" || score != 20 {
		t.Fatalf("daily rollup n=%d tempN=%d avg=%v min=%v max=%v health=%s/%d", n, tempN, avg, min, max, status, score)
	}
	var remaining int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM smart_hourly_rollups`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("hourly rows remaining=%d", remaining)
	}
}

func TestTierMaintenanceZeroRawSkipsTierAndReplaysPendingDeletes(t *testing.T) {
	db := openTestDuckDB(t)
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	insertTestDrive(t, db, 1, "/dev/disabled-raw", now)
	info := smart.DriveInfo{Device: "/dev/disabled-raw"}
	old := now.Add(-40 * 24 * time.Hour)
	var ids []int64
	for i := 0; i < 3; i++ {
		id, _, err := db.InsertSample(ctx, info, smart.SmartSample{CollectedAt: old.Add(time.Duration(i) * time.Minute), RawJSON: `{}`}, health.Result{Status: health.StatusGreen, Score: 95})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, _, err := db.InsertSample(ctx, info, smart.SmartSample{CollectedAt: now, RawJSON: `{}`}, health.Result{Status: health.StatusGreen, Score: 95}); err != nil {
		t.Fatal(err)
	}
	// Simulate a prior rollup commit interrupted before physical FK child cleanup.
	if _, err := db.db.ExecContext(ctx, `INSERT INTO smart_rollup_pending_samples VALUES (?)`, ids[0]); err != nil {
		t.Fatal(err)
	}
	dry, err := db.TierMaintenance(ctx, now, TierPolicy{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if dry.PendingSamplesDeleted != 0 || dry.PendingRowsRemaining != 1 || dry.RawSamplesRemaining != 0 || dry.HourlyRowsRemaining != 0 || dry.DailyRowsRemaining != 0 {
		t.Fatalf("dry-run pending/disabled tier counts: %+v", dry)
	}
	var staged int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM smart_rollup_pending_samples`).Scan(&staged); err != nil {
		t.Fatal(err)
	}
	if staged != 1 {
		t.Fatal("dry-run modified pending deletion journal")
	}
	r, err := db.TierMaintenance(ctx, now, TierPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if r.RawSamplesRolled != 0 || r.HourlyRowsRolled != 0 || r.DailyRowsDeleted != 0 || r.RawSamplesRemaining != 0 || r.HourlyRowsRemaining != 0 || r.DailyRowsRemaining != 0 || r.PendingSamplesDeleted != 1 || r.PendingRowsRemaining != 0 {
		t.Fatalf("disabled tiers/pending replay reported unexpected work: %+v", r)
	}
	var raw, pending int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM smart_samples WHERE collected_at<?`, now.Add(-28*24*time.Hour)).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM smart_rollup_pending_samples`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if raw != 2 || pending != 0 {
		t.Fatalf("raw disabled/replay: old raw=%d pending=%d", raw, pending)
	}
}

func TestTierMaintenanceZeroHourlySkipsRollupButRunsOtherTiers(t *testing.T) {
	db := openTestDuckDB(t)
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	insertTestDrive(t, db, 1, "/dev/disabled-hourly", now)
	day := now.Add(-20 * 24 * time.Hour).Truncate(24 * time.Hour)
	_, err := db.db.ExecContext(ctx, `INSERT INTO smart_hourly_rollups(drive_id,bucket_start,bucket_end,sample_count,temp_count,last_sample_at,last_sample_id) VALUES(1,?,?,1,0,?,1)`, day, day.Add(time.Hour), day)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.db.ExecContext(ctx, `INSERT INTO smart_daily_rollups(drive_id,bucket_start,bucket_end,sample_count,temp_count,last_sample_at,last_sample_id) VALUES(1,?,?,1,0,?,2)`, day.Add(-10*24*time.Hour), day.Add(-9*24*time.Hour), day.Add(-10*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	p := TierPolicy{RawRetention: 0, HourlyRetention: 0, DailyRetention: 10 * 24 * time.Hour}
	r, err := db.TierMaintenance(ctx, now, p)
	if err != nil {
		t.Fatal(err)
	}
	if r.HourlyRowsRolled != 0 || r.HourlyRowsRemaining != 0 || r.DailyRowsDeleted != 1 || r.DailyRowsRemaining != 0 {
		t.Fatalf("unexpected disabled-hourly result: %+v", r)
	}
	var hours, dailies int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM smart_hourly_rollups`).Scan(&hours); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM smart_daily_rollups`).Scan(&dailies); err != nil {
		t.Fatal(err)
	}
	if hours != 1 || dailies != 0 {
		t.Fatalf("hourly disabled stage: hours=%d daily=%d", hours, dailies)
	}
}

func TestTierMaintenanceZeroDailySkipsDeletionAndRemainingCount(t *testing.T) {
	db := openTestDuckDB(t)
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	insertTestDrive(t, db, 1, "/dev/disabled-daily", now)
	oldRaw := now.Add(-30 * 24 * time.Hour)
	for _, at := range []time.Time{oldRaw, now} {
		if _, _, err := db.InsertSample(ctx, smart.DriveInfo{Device: "/dev/disabled-daily"}, smart.SmartSample{CollectedAt: at, RawJSON: `{}`}, health.Result{Status: health.StatusGreen, Score: 95}); err != nil {
			t.Fatal(err)
		}
	}
	old := now.Add(-6 * 365 * 24 * time.Hour)
	_, err := db.db.ExecContext(ctx, `INSERT INTO smart_daily_rollups(drive_id,bucket_start,bucket_end,sample_count,temp_count,last_sample_at,last_sample_id) VALUES(1,?,?,1,0,?,1)`, old, old.Add(24*time.Hour), old)
	if err != nil {
		t.Fatal(err)
	}
	r, err := db.TierMaintenance(ctx, now, TierPolicy{RawRetention: 28 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if r.RawSamplesRolled != 1 || r.DailyRowsDeleted != 0 || r.DailyRowsRemaining != 0 {
		t.Fatalf("disabled daily tier reported work: %+v", r)
	}
	var count int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM smart_daily_rollups`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("disabled daily tier deleted row: count=%d", count)
	}
}

func TestTierMaintenanceRejectsNegativeRetention(t *testing.T) {
	db := openTestDuckDB(t)
	defer db.Close()
	if _, err := db.TierMaintenance(context.Background(), time.Now(), TierPolicy{HourlyRetention: -time.Second}); err == nil {
		t.Fatal("expected negative retention error")
	}
}
