//go:build cgo

package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/marcboeker/go-duckdb"
)

func TestOpenDuckDBMigratesLegacyDriveIdentityAndConsolidatesDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-storage.duckdb")
	createLegacyDuckDB(t, path)

	db, err := OpenDuckDB(path)
	if err != nil {
		t.Fatalf("OpenDuckDB(%q) error: %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()

	var driveCount int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM drives`).Scan(&driveCount); err != nil {
		t.Fatalf("count drives: %v", err)
	}
	if driveCount != 2 {
		t.Fatalf("expected 2 drive rows after consolidation, got %d", driveCount)
	}

	var (
		id          int64
		device      string
		identityKey string
		model       string
		serial      string
		wwn         string
		firstSeenAt time.Time
		lastSeenAt  time.Time
	)
	if err := db.db.QueryRowContext(ctx, `
		SELECT id, device, identity_key, model, serial, wwn, first_seen_at, last_seen_at
		FROM drives
		WHERE id = 11
	`).Scan(&id, &device, &identityKey, &model, &serial, &wwn, &firstSeenAt, &lastSeenAt); err != nil {
		t.Fatalf("query canonical drive: %v", err)
	}
	if device != "/dev/disk4" {
		t.Fatalf("canonical device=%q want %q", device, "/dev/disk4")
	}
	if identityKey != "wwn:0x5000cca123" {
		t.Fatalf("identity_key=%q want %q", identityKey, "wwn:0x5000cca123")
	}
	if model != "Samsung SSD 870 EVO" || serial != "SN123" || wwn != "0x5000cca123" {
		t.Fatalf("unexpected canonical identity fields: model=%q serial=%q wwn=%q", model, serial, wwn)
	}
	if !firstSeenAt.Equal(time.Date(2026, 3, 20, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("first_seen_at=%s want %s", firstSeenAt, time.Date(2026, 3, 20, 1, 0, 0, 0, time.UTC))
	}
	if !lastSeenAt.Equal(time.Date(2026, 3, 20, 5, 0, 0, 0, time.UTC)) {
		t.Fatalf("last_seen_at=%s want %s", lastSeenAt, time.Date(2026, 3, 20, 5, 0, 0, 0, time.UTC))
	}

	for _, table := range []string{"smart_samples", "drive_health", "smart_test_runs"} {
		var count int
		if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE drive_id = 10`).Scan(&count); err != nil {
			t.Fatalf("count %s legacy drive refs: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("expected no %s rows for duplicate drive after migration, got %d", table, count)
		}
	}

	var sampleDriveID int64
	if err := db.db.QueryRowContext(ctx, `SELECT drive_id FROM smart_samples WHERE id = 101`).Scan(&sampleDriveID); err != nil {
		t.Fatalf("query smart_samples remap: %v", err)
	}
	if sampleDriveID != 11 {
		t.Fatalf("smart_samples drive_id=%d want 11", sampleDriveID)
	}

	var healthDriveID int64
	if err := db.db.QueryRowContext(ctx, `SELECT drive_id FROM drive_health WHERE sample_id = 101`).Scan(&healthDriveID); err != nil {
		t.Fatalf("query drive_health remap: %v", err)
	}
	if healthDriveID != 11 {
		t.Fatalf("drive_health drive_id=%d want 11", healthDriveID)
	}

	var runDriveID int64
	if err := db.db.QueryRowContext(ctx, `SELECT drive_id FROM smart_test_runs WHERE id = 201`).Scan(&runDriveID); err != nil {
		t.Fatalf("query smart_test_runs remap: %v", err)
	}
	if runDriveID != 11 {
		t.Fatalf("smart_test_runs drive_id=%d want 11", runDriveID)
	}

	var stateCount int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_state WHERE drive_id = 11`).Scan(&stateCount); err != nil {
		t.Fatalf("count notification_state: %v", err)
	}
	if stateCount != 2 {
		t.Fatalf("expected 2 notification_state rows for canonical drive, got %d", stateCount)
	}

	var notifState string
	var notifUpdatedAt time.Time
	if err := db.db.QueryRowContext(ctx, `
		SELECT state, updated_at FROM notification_state
		WHERE drive_id = 11 AND notification_name = 'discord-primary'
	`).Scan(&notifState, &notifUpdatedAt); err != nil {
		t.Fatalf("query merged notification_state: %v", err)
	}
	if notifState != "FAIL" {
		t.Fatalf("notification state=%q want FAIL", notifState)
	}
	if !notifUpdatedAt.Equal(time.Date(2026, 3, 20, 6, 0, 0, 0, time.UTC)) {
		t.Fatalf("notification updated_at=%s want %s", notifUpdatedAt, time.Date(2026, 3, 20, 6, 0, 0, 0, time.UTC))
	}

	gotIndexes := queryIndexNames(t, ctx, db.db, []string{
		"idx_smart_samples_drive_id_id",
		"idx_drive_health_drive_id_sample_id",
		"idx_smart_samples_drive_id_collected_at",
		"idx_smart_attributes_sample_id_attribute_id",
		"idx_smart_test_runs_drive_id_started_at",
	})
	wantIndexes := []string{
		"idx_drive_health_drive_id_sample_id",
		"idx_smart_attributes_sample_id_attribute_id",
		"idx_smart_samples_drive_id_collected_at",
		"idx_smart_samples_drive_id_id",
		"idx_smart_test_runs_drive_id_started_at",
	}
	if len(gotIndexes) != len(wantIndexes) {
		t.Fatalf("expected %d query indexes after legacy migration, got %d: %v", len(wantIndexes), len(gotIndexes), gotIndexes)
	}
	for i := range wantIndexes {
		if gotIndexes[i] != wantIndexes[i] {
			t.Fatalf("query index[%d]=%q want %q (all=%v)", i, gotIndexes[i], wantIndexes[i], gotIndexes)
		}
	}
}

func TestOpenDuckDBMigrationIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-storage-idempotent.duckdb")
	createLegacyDuckDB(t, path)

	first, err := OpenDuckDB(path)
	if err != nil {
		t.Fatalf("first OpenDuckDB(%q) error: %v", path, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first db: %v", err)
	}

	second, err := OpenDuckDB(path)
	if err != nil {
		t.Fatalf("second OpenDuckDB(%q) error: %v", path, err)
	}
	t.Cleanup(func() { _ = second.Close() })

	ctx := context.Background()
	var driveCount int
	if err := second.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM drives`).Scan(&driveCount); err != nil {
		t.Fatalf("count drives after second open: %v", err)
	}
	if driveCount != 2 {
		t.Fatalf("expected 2 drive rows after second open, got %d", driveCount)
	}

	var canonicalCount int
	if err := second.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM drives WHERE identity_key = 'wwn:0x5000cca123'`).Scan(&canonicalCount); err != nil {
		t.Fatalf("count canonical identity rows: %v", err)
	}
	if canonicalCount != 1 {
		t.Fatalf("expected 1 canonical identity row after second open, got %d", canonicalCount)
	}

	var indexCount int
	if err := second.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM duckdb_indexes()
		WHERE table_name = 'drives' AND index_name = 'idx_drives_identity_key'
	`).Scan(&indexCount); err != nil {
		t.Fatalf("query drives identity index: %v", err)
	}
	if indexCount != 1 {
		t.Fatalf("expected drives identity index to exist once, got %d", indexCount)
	}
}

func createLegacyDuckDB(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatalf("sql.Open legacy duckdb: %v", err)
	}
	defer db.Close()

	legacySchema := []string{
		`CREATE TABLE drives (
			id BIGINT PRIMARY KEY,
			device TEXT NOT NULL UNIQUE,
			model TEXT,
			serial TEXT,
			wwn TEXT,
			first_seen_at TIMESTAMP NOT NULL,
			last_seen_at TIMESTAMP NOT NULL
		)`,
		`CREATE SEQUENCE seq_drives START 1`,
		`CREATE TABLE smart_samples (
			id BIGINT PRIMARY KEY,
			drive_id BIGINT NOT NULL,
			collected_at TIMESTAMP NOT NULL,
			temperature INTEGER,
			power_on_hours BIGINT,
			reallocated_sectors BIGINT,
			pending_sectors BIGINT,
			uncorrectable_sectors BIGINT,
			wear_level BIGINT,
			raw_json JSON,
			FOREIGN KEY (drive_id) REFERENCES drives(id)
		)`,
		`CREATE SEQUENCE seq_samples START 1`,
		`CREATE TABLE smart_attributes (
			sample_id BIGINT NOT NULL,
			attribute_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			value INTEGER,
			worst INTEGER,
			threshold INTEGER,
			raw TEXT,
			FOREIGN KEY (sample_id) REFERENCES smart_samples(id)
		)`,
		`CREATE TABLE drive_health (
			drive_id BIGINT NOT NULL,
			sample_id BIGINT NOT NULL,
			status TEXT NOT NULL,
			score INTEGER NOT NULL,
			reasons TEXT,
			FOREIGN KEY (drive_id) REFERENCES drives(id),
			FOREIGN KEY (sample_id) REFERENCES smart_samples(id)
		)`,
		`CREATE TABLE smart_test_runs (
			id BIGINT PRIMARY KEY,
			drive_id BIGINT NOT NULL,
			test_type TEXT NOT NULL,
			scheduled_at TIMESTAMP NOT NULL,
			started_at TIMESTAMP NOT NULL,
			finished_at TIMESTAMP NOT NULL,
			status TEXT NOT NULL,
			message TEXT,
			FOREIGN KEY (drive_id) REFERENCES drives(id)
		)`,
		`CREATE SEQUENCE seq_smart_test_runs START 1`,
		`CREATE TABLE notification_state (
			drive_id BIGINT NOT NULL,
			notification_name TEXT NOT NULL,
			state TEXT NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			PRIMARY KEY (drive_id, notification_name),
			FOREIGN KEY (drive_id) REFERENCES drives(id)
		)`,
		`INSERT INTO drives (id, device, model, serial, wwn, first_seen_at, last_seen_at) VALUES
			(10, '/dev/disk2', 'Samsung SSD 870 EVO', 'SN123', '', '2026-03-20 01:00:00', '2026-03-20 02:00:00'),
			(11, '/dev/disk4', 'Samsung SSD 870 EVO', 'SN123', '0x5000cca123', '2026-03-20 03:00:00', '2026-03-20 05:00:00'),
			(20, '/dev/disk8', 'Crucial MX500', 'SN999', '', '2026-03-20 04:00:00', '2026-03-20 04:30:00')`,
		`INSERT INTO smart_samples (id, drive_id, collected_at, raw_json) VALUES
			(101, 10, '2026-03-20 02:15:00', '{}'),
			(102, 11, '2026-03-20 05:15:00', '{}')`,
		`INSERT INTO drive_health (drive_id, sample_id, status, score, reasons) VALUES
			(10, 101, 'GREEN', 95, ''),
			(11, 102, 'YELLOW', 80, 'temp')`,
		`INSERT INTO smart_test_runs (id, drive_id, test_type, scheduled_at, started_at, finished_at, status, message) VALUES
			(201, 10, 'short', '2026-03-20 02:20:00', '2026-03-20 02:21:00', '2026-03-20 02:30:00', 'completed', ''),
			(202, 11, 'long', '2026-03-20 05:20:00', '2026-03-20 05:21:00', '2026-03-20 05:30:00', 'completed', '')`,
		`INSERT INTO notification_state (drive_id, notification_name, state, updated_at) VALUES
			(10, 'discord-primary', 'PASS', '2026-03-20 04:00:00'),
			(11, 'discord-primary', 'FAIL', '2026-03-20 06:00:00'),
			(10, 'email-secondary', 'WARN', '2026-03-20 03:30:00')`,
	}

	for _, stmt := range legacySchema {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec legacy schema statement %q: %v", stmt, err)
		}
	}
}

func queryIndexNames(t *testing.T, ctx context.Context, db *sql.DB, expected []string) []string {
	t.Helper()

	rows, err := db.QueryContext(ctx, `
		SELECT index_name
		FROM duckdb_indexes()
		WHERE index_name IN (`+placeholders(len(expected))+`)
	`, stringArgs(expected)...)
	if err != nil {
		t.Fatalf("query expected indexes: %v", err)
	}
	defer rows.Close()

	got := make([]string, 0, len(expected))
	for rows.Next() {
		var indexName string
		if err := rows.Scan(&indexName); err != nil {
			t.Fatalf("scan index name: %v", err)
		}
		got = append(got, indexName)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate index names: %v", err)
	}

	sort.Strings(got)
	return got
}

func placeholders(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}

func stringArgs(values []string) []any {
	args := make([]any, len(values))
	for i, value := range values {
		args[i] = value
	}
	return args
}
