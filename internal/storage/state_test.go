//go:build cgo

package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"diskmon/internal/smart"
)

func TestGetNotificationStateMissing(t *testing.T) {
	db := openTestDuckDB(t)
	t.Cleanup(func() { _ = db.Close() })

	got, err := db.GetNotificationState(context.Background(), 1, "discord-primary")
	if err != nil {
		t.Fatalf("GetNotificationState returned error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil state for missing row, got %+v", *got)
	}
}

func TestUpsertNotificationStateCreatesAndUpdatesSingleRow(t *testing.T) {
	db := openTestDuckDB(t)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	insertTestDrive(t, db, 7, "/dev/sda", time.Date(2026, 3, 20, 1, 0, 0, 0, time.UTC))

	firstAt := time.Date(2026, 3, 20, 2, 0, 0, 0, time.UTC)
	if err := db.UpsertNotificationState(ctx, 7, "discord-primary", "PASS", firstAt); err != nil {
		t.Fatalf("first UpsertNotificationState returned error: %v", err)
	}

	got, err := db.GetNotificationState(ctx, 7, "discord-primary")
	if err != nil {
		t.Fatalf("GetNotificationState returned error: %v", err)
	}
	if got == nil {
		t.Fatal("expected state row after first upsert, got nil")
	}
	if got.State != "PASS" {
		t.Fatalf("expected PASS state, got %q", got.State)
	}
	if !got.UpdatedAt.Equal(firstAt) {
		t.Fatalf("expected updated_at %s, got %s", firstAt, got.UpdatedAt)
	}

	secondAt := firstAt.Add(5 * time.Minute)
	if err := db.UpsertNotificationState(ctx, 7, "discord-primary", "FAIL", secondAt); err != nil {
		t.Fatalf("second UpsertNotificationState returned error: %v", err)
	}

	got, err = db.GetNotificationState(ctx, 7, "discord-primary")
	if err != nil {
		t.Fatalf("GetNotificationState after update returned error: %v", err)
	}
	if got == nil {
		t.Fatal("expected state row after update, got nil")
	}
	if got.State != "FAIL" {
		t.Fatalf("expected FAIL state after update, got %q", got.State)
	}
	if !got.UpdatedAt.Equal(secondAt) {
		t.Fatalf("expected updated_at %s after update, got %s", secondAt, got.UpdatedAt)
	}

	var count int
	if err := db.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notification_state WHERE drive_id = ? AND notification_name = ?`,
		7, "discord-primary",
	).Scan(&count); err != nil {
		t.Fatalf("count query returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row for key after repeated upserts, got %d", count)
	}
}

func TestInsertSampleReusesDriveAcrossDevicePathChanges(t *testing.T) {
	db := openTestDuckDB(t)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn returned error: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	firstAt := time.Date(2026, 3, 22, 8, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(30 * time.Minute)

	firstInfo := smart.DriveInfo{
		Device: "/dev/sda",
		WWN:    "0x5000C500AABBCCDD",
	}
	secondInfo := smart.DriveInfo{
		Device: "/dev/disk/by-id/wwn-0x5000C500AABBCCDD",
		Model:  "Samsung SSD 870 EVO 1TB",
		Serial: "S6PXYZ123456",
		WWN:    "0x5000C500AABBCCDD",
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("first BeginTx returned error: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	driveID, err := upsertDrive(ctx, tx, firstInfo, firstAt)
	if err != nil {
		t.Fatalf("first upsertDrive returned error: %v", err)
	}
	firstSampleID, err := insertSmartSample(ctx, tx, driveID, smart.SmartSample{CollectedAt: firstAt, RawJSON: `{}`})
	if err != nil {
		t.Fatalf("first insertSmartSample returned error: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("first Commit returned error: %v", err)
	}

	tx, err = conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("second BeginTx returned error: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	driveID, err = upsertDrive(ctx, tx, secondInfo, secondAt)
	if err != nil {
		t.Fatalf("second upsertDrive returned error: %v", err)
	}
	secondSampleID, err := insertSmartSample(ctx, tx, driveID, smart.SmartSample{CollectedAt: secondAt, RawJSON: `{}`})
	if err != nil {
		t.Fatalf("second insertSmartSample returned error: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("second Commit returned error: %v", err)
	}

	var (
		driveCount    int
		storedDriveID int64
		device        string
		model         string
		serial        string
		wwn           string
		firstSeenAt   time.Time
		lastSeenAt    time.Time
	)
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM drives`).Scan(&driveCount); err != nil {
		t.Fatalf("drive count query returned error: %v", err)
	}
	if driveCount != 1 {
		t.Fatalf("expected 1 drive row after device path change, got %d", driveCount)
	}

	if err := db.db.QueryRowContext(ctx, `
		SELECT id, device, model, serial, wwn, first_seen_at, last_seen_at
		FROM drives
		WHERE identity_key = ?
	`, driveIdentityKey(firstInfo)).Scan(&storedDriveID, &device, &model, &serial, &wwn, &firstSeenAt, &lastSeenAt); err != nil {
		t.Fatalf("drive lookup returned error: %v", err)
	}
	if device != secondInfo.Device {
		t.Fatalf("device=%q want %q", device, secondInfo.Device)
	}
	if model != secondInfo.Model {
		t.Fatalf("model=%q want %q", model, secondInfo.Model)
	}
	if serial != secondInfo.Serial {
		t.Fatalf("serial=%q want %q", serial, secondInfo.Serial)
	}
	if wwn != secondInfo.WWN {
		t.Fatalf("wwn=%q want %q", wwn, secondInfo.WWN)
	}
	if !firstSeenAt.Equal(firstAt) {
		t.Fatalf("first_seen_at=%s want %s", firstSeenAt, firstAt)
	}
	if !lastSeenAt.Equal(secondAt) {
		t.Fatalf("last_seen_at=%s want %s", lastSeenAt, secondAt)
	}

	for _, sampleID := range []int64{firstSampleID, secondSampleID} {
		var sampleDriveID int64
		if err := db.db.QueryRowContext(ctx, `SELECT drive_id FROM smart_samples WHERE id = ?`, sampleID).Scan(&sampleDriveID); err != nil {
			t.Fatalf("sample drive lookup for %d returned error: %v", sampleID, err)
		}
		if sampleDriveID != storedDriveID {
			t.Fatalf("sample %d drive_id=%d want %d", sampleID, sampleDriveID, storedDriveID)
		}
	}
}

func openTestDuckDB(t *testing.T) *DuckDB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "storage-test.duckdb")
	db, err := OpenDuckDB(path)
	if err != nil {
		t.Fatalf("OpenDuckDB(%q) error: %v", path, err)
	}
	return db
}

func insertTestDrive(t *testing.T, db *DuckDB, id int64, device string, seenAt time.Time) {
	t.Helper()

	_, err := db.db.Exec(`
		INSERT INTO drives (id, device, identity_key, model, serial, wwn, first_seen_at, last_seen_at)
		VALUES (?, ?, ?, '', '', '', ?, ?)
	`, id, device, driveIdentityKey(smart.DriveInfo{Device: device}), seenAt, seenAt)
	if err != nil {
		t.Fatalf("insert test drive: %v", err)
	}
}
