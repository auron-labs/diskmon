//go:build cgo

package storage

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestMarkIncompleteSmartTestRuns(t *testing.T) {
	db := openTestDuckDB(t)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	seenAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	insertTestDrive(t, db, 1, "/dev/disk1", seenAt)

	_, err := db.db.ExecContext(ctx, `
		INSERT INTO smart_test_runs (id, drive_id, test_type, scheduled_at, started_at, finished_at, status, message)
		VALUES
			(1, 1, 'short', ?, ?, ?, 'STARTED', 'initial output'),
			(2, 1, 'short', ?, ?, ?, 'passed', 'completed successfully'),
			(3, 1, 'short', ?, ?, ?, 'UNKNOWN', 'self-test result unavailable'),
			(4, 1, 'short', ?, ?, ?, 'INCOMPLETE', 'already marked incomplete'),
			(5, 1, 'short', ?, ?, ?, 'in_progress', NULL)
	`,
		seenAt, seenAt, seenAt,
		seenAt, seenAt, seenAt,
		seenAt, seenAt, seenAt,
		seenAt, seenAt, seenAt,
		seenAt, seenAt, seenAt,
	)
	if err != nil {
		t.Fatalf("insert smart_test_runs: %v", err)
	}

	now := time.Date(2026, 7, 8, 12, 30, 0, 0, time.UTC)
	updated, err := db.MarkIncompleteSmartTestRuns(ctx, now, nil)
	if err != nil {
		t.Fatalf("MarkIncompleteSmartTestRuns returned error: %v", err)
	}
	if updated != 2 {
		t.Fatalf("updated=%d want 2", updated)
	}

	type run struct {
		status  string
		message sql.NullString
	}
	loadRuns := func() map[int64]run {
		t.Helper()
		runs := map[int64]run{}
		rows, err := db.db.QueryContext(ctx, `SELECT id, status, message FROM smart_test_runs ORDER BY id`)
		if err != nil {
			t.Fatalf("query smart_test_runs: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var status string
			var message sql.NullString
			if err := rows.Scan(&id, &status, &message); err != nil {
				t.Fatalf("scan smart_test_runs: %v", err)
			}
			runs[id] = run{status: status, message: message}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate smart_test_runs: %v", err)
		}
		return runs
	}

	runs := loadRuns()
	if len(runs) != 5 {
		t.Fatalf("row count=%d want 5", len(runs))
	}

	if runs[1].status != SmartTestStatusIncomplete {
		t.Fatalf("run 1 status=%q want %q", runs[1].status, SmartTestStatusIncomplete)
	}
	if !runs[1].message.Valid || runs[1].message.String != "initial output\ndaemon restarted before final SMART self-test result was recorded at 2026-07-08T12:30:00Z; previous status: STARTED" {
		t.Fatalf("run 1 message=%v", runs[1].message)
	}
	if runs[2].status != "passed" {
		t.Fatalf("run 2 status=%q want %q", runs[2].status, "passed")
	}
	if !runs[2].message.Valid || runs[2].message.String != "completed successfully" {
		t.Fatalf("run 2 message=%v", runs[2].message)
	}
	if runs[3].status != SmartTestStatusUnknown {
		t.Fatalf("run 3 status=%q want %q", runs[3].status, SmartTestStatusUnknown)
	}
	if runs[4].status != SmartTestStatusIncomplete {
		t.Fatalf("run 4 status=%q want %q", runs[4].status, SmartTestStatusIncomplete)
	}
	if runs[5].status != SmartTestStatusIncomplete {
		t.Fatalf("run 5 status=%q want %q", runs[5].status, SmartTestStatusIncomplete)
	}
	if !runs[5].message.Valid || runs[5].message.String != "daemon restarted before final SMART self-test result was recorded at 2026-07-08T12:30:00Z; previous status: IN_PROGRESS" {
		t.Fatalf("run 5 message=%v", runs[5].message)
	}

	firstPass := map[int64]run{}
	for id, value := range runs {
		firstPass[id] = value
	}

	updated, err = db.MarkIncompleteSmartTestRuns(ctx, now.Add(time.Minute), nil)
	if err != nil {
		t.Fatalf("second MarkIncompleteSmartTestRuns returned error: %v", err)
	}
	if updated != 0 {
		t.Fatalf("second updated=%d want 0", updated)
	}

	runs = loadRuns()
	if len(runs) != len(firstPass) {
		t.Fatalf("row count after second run=%d want %d", len(runs), len(firstPass))
	}
	for id, want := range firstPass {
		if got := runs[id]; got != want {
			t.Fatalf("run %d after second pass=%+v want %+v", id, got, want)
		}
	}
}

func TestMarkIncompleteSmartTestRunsSkipsDevicesWithTestInProgress(t *testing.T) {
	db := openTestDuckDB(t)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	seenAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	insertTestDrive(t, db, 1, "/dev/disk1", seenAt)
	insertTestDrive(t, db, 2, "/dev/disk2", seenAt)

	if _, err := db.db.ExecContext(ctx, `
		INSERT INTO smart_test_runs (id, drive_id, test_type, scheduled_at, started_at, finished_at, status, message)
		VALUES
			(10, 1, 'short', ?, ?, ?, 'STARTED', 'initial output'),
			(11, 2, 'short', ?, ?, ?, 'STARTED', 'initial output')
	`,
		seenAt, seenAt, seenAt,
		seenAt, seenAt, seenAt,
	); err != nil {
		t.Fatalf("insert smart_test_runs: %v", err)
	}

	now := time.Date(2026, 7, 8, 12, 30, 0, 0, time.UTC)
	updated, err := db.MarkIncompleteSmartTestRuns(ctx, now, []string{"/dev/disk2"})
	if err != nil {
		t.Fatalf("MarkIncompleteSmartTestRuns returned error: %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated=%d want 1 (disk2 should be skipped)", updated)
	}

	var status1, status2 string
	if err := db.db.QueryRowContext(ctx, `SELECT status FROM smart_test_runs WHERE id = 10`).Scan(&status1); err != nil {
		t.Fatalf("query run 10: %v", err)
	}
	if status1 != SmartTestStatusIncomplete {
		t.Fatalf("run 10 status=%q want %q", status1, SmartTestStatusIncomplete)
	}
	if err := db.db.QueryRowContext(ctx, `SELECT status FROM smart_test_runs WHERE id = 11`).Scan(&status2); err != nil {
		t.Fatalf("query run 11: %v", err)
	}
	if status2 != "STARTED" {
		t.Fatalf("run 11 status=%q want STARTED (skipped)", status2)
	}
}
