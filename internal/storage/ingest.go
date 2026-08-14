//go:build cgo

package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"diskmon/internal/health"
	"diskmon/internal/smart"
)

func (d *DuckDB) InsertSample(ctx context.Context, info smart.DriveInfo, sample smart.SmartSample, result health.Result) (int64, int64, error) {
	conn, err := d.db.Conn(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer conn.Close()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	driveID, err := upsertDrive(ctx, tx, info, sample.CollectedAt)
	if err != nil {
		return 0, 0, err
	}

	sampleID, err := insertSmartSample(ctx, tx, driveID, sample)
	if err != nil {
		return 0, 0, err
	}

	if err := insertAttributes(ctx, tx, sampleID, sample.Attributes); err != nil {
		return 0, 0, err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO drive_health (drive_id, sample_id, status, score, reasons) VALUES (?, ?, ?, ?, ?)`,
		driveID, sampleID, result.Status, result.Score, strings.Join(result.Reasons, "; ")); err != nil {
		return 0, 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}

	return sampleID, driveID, nil
}

func (d *DuckDB) InsertSmartTestRun(ctx context.Context, info smart.DriveInfo, run SmartTestRun) (int64, error) {
	conn, err := d.db.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	seenAt := run.StartedAt
	if run.FinishedAt != nil {
		seenAt = *run.FinishedAt
	}
	driveID, err := upsertDrive(ctx, tx, info, seenAt)
	if err != nil {
		return 0, err
	}

	var runID int64
	if err := tx.QueryRowContext(ctx, `SELECT nextval('seq_smart_test_runs')`).Scan(&runID); err != nil {
		return 0, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO smart_test_runs (
			id, drive_id, test_type, scheduled_at, started_at, finished_at, status, message
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, runID, driveID, run.TestType, run.ScheduledAt, run.StartedAt, run.FinishedAt, run.Status, run.Message); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return runID, nil
}

func upsertDrive(ctx context.Context, tx *sql.Tx, info smart.DriveInfo, seenAt time.Time) (int64, error) {
	identityKey := driveIdentityKey(info)
	if identityKey == "" {
		return 0, fmt.Errorf("derive drive identity key: empty identity for device %q", info.Device)
	}

	var driveID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM drives WHERE identity_key = ?`, identityKey).Scan(&driveID)
	if err == nil {
		_, err = tx.ExecContext(ctx,
			`UPDATE drives SET
				device = ?,
				model = CASE WHEN ? <> '' THEN ? ELSE model END,
				serial = CASE WHEN ? <> '' THEN ? ELSE serial END,
				wwn = CASE WHEN ? <> '' THEN ? ELSE wwn END,
				last_seen_at = ?
			WHERE id = ?`,
			info.Device,
			info.Model, info.Model,
			info.Serial, info.Serial,
			info.WWN, info.WWN,
			seenAt, driveID,
		)
		return driveID, err
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	if err := tx.QueryRowContext(ctx, `SELECT nextval('seq_drives')`).Scan(&driveID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO drives (id, device, identity_key, model, serial, wwn, first_seen_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		driveID, info.Device, identityKey, info.Model, info.Serial, info.WWN, seenAt, seenAt); err != nil {
		return 0, err
	}

	return driveID, nil
}

func insertSmartSample(ctx context.Context, tx *sql.Tx, driveID int64, sample smart.SmartSample) (int64, error) {
	var sampleID int64
	if err := tx.QueryRowContext(ctx, `SELECT nextval('seq_samples')`).Scan(&sampleID); err != nil {
		return 0, err
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO smart_samples (
			id, drive_id, collected_at, temperature, power_on_hours,
			reallocated_sectors, pending_sectors, uncorrectable_sectors, wear_level, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		sampleID,
		driveID,
		sample.CollectedAt,
		nullInt(sample.Temperature),
		nullInt64(sample.PowerOnHours),
		nullInt64(sample.ReallocatedSectors),
		nullInt64(sample.PendingSectors),
		nullInt64(sample.UncorrectableSectors),
		nullInt64(sample.WearLevel),
		sample.RawJSON,
	)
	if err != nil {
		return 0, fmt.Errorf("insert smart sample: %w", err)
	}
	return sampleID, nil
}

func insertAttributes(ctx context.Context, tx *sql.Tx, sampleID int64, attrs []smart.SmartAttribute) error {
	for _, attr := range attrs {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO smart_attributes (sample_id, attribute_id, name, value, worst, threshold, raw) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			sampleID, attr.AttributeID, attr.Name, attr.Value, attr.Worst, attr.Threshold, attr.Raw)
		if err != nil {
			return err
		}
	}
	return nil
}

func nullInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}
