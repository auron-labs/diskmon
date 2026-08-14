//go:build cgo

package storage

import (
	"context"
	"database/sql"
)

func (d *DuckDB) ListDrives(ctx context.Context) ([]DriveSummary, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT d.id, d.device, d.model, d.serial,
		       COALESCE(h.status, 'UNKNOWN') AS health,
		       s.temperature, s.power_on_hours, d.last_seen_at
		FROM drives d
		LEFT JOIN LATERAL (
			SELECT sample_id, status FROM drive_health dh
			WHERE dh.drive_id = d.id
			ORDER BY sample_id DESC
			LIMIT 1
		) h ON true
		LEFT JOIN LATERAL (
			SELECT temperature, power_on_hours FROM smart_samples ss
			WHERE ss.drive_id = d.id
			ORDER BY id DESC
			LIMIT 1
		) s ON true
		ORDER BY d.device
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []DriveSummary{}
	for rows.Next() {
		var item DriveSummary
		if err := rows.Scan(&item.ID, &item.Device, &item.Model, &item.Serial, &item.Health, &item.Temperature, &item.PowerOnHrs, &item.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (d *DuckDB) GetDrive(ctx context.Context, id int64) (*DriveDetail, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT d.id, d.device, d.model, d.serial, d.wwn,
		       COALESCE(h.status, 'UNKNOWN'), COALESCE(h.score, 0), COALESCE(h.reasons, ''),
		       s.temperature, s.power_on_hours, s.reallocated_sectors, s.pending_sectors,
		       s.uncorrectable_sectors, s.wear_level, s.collected_at,
		       d.first_seen_at, d.last_seen_at
		FROM drives d
		LEFT JOIN LATERAL (
			SELECT sample_id, status, score, reasons
			FROM drive_health
			WHERE drive_id = d.id
			ORDER BY sample_id DESC
			LIMIT 1
		) h ON true
		LEFT JOIN LATERAL (
			SELECT id, temperature, power_on_hours, reallocated_sectors, pending_sectors, uncorrectable_sectors, wear_level, collected_at
			FROM smart_samples
			WHERE drive_id = d.id
			ORDER BY id DESC
			LIMIT 1
		) s ON true
		WHERE d.id = ?
	`, id)

	var item DriveDetail
	if err := row.Scan(
		&item.ID, &item.Device, &item.Model, &item.Serial, &item.WWN,
		&item.Health, &item.HealthScore, &item.HealthReasons,
		&item.Temperature, &item.PowerOnHours, &item.Reallocated, &item.Pending,
		&item.Uncorrectable, &item.WearLevel, &item.CollectedAt,
		&item.FirstSeen, &item.LastSeen,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (d *DuckDB) DriveHistory(ctx context.Context, id int64, limit int) ([]HistoryPoint, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT collected_at, temperature, power_on_hours, reallocated_sectors,
		       pending_sectors, uncorrectable_sectors, wear_level
		FROM smart_samples
		WHERE drive_id = ?
		ORDER BY collected_at DESC
		LIMIT ?
	`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []HistoryPoint{}
	for rows.Next() {
		var p HistoryPoint
		if err := rows.Scan(&p.CollectedAt, &p.Temperature, &p.PowerOnHours, &p.ReallocatedSectors, &p.PendingSectors, &p.UncorrectableSectors, &p.WearLevel); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d *DuckDB) DriveAttributes(ctx context.Context, id int64) ([]AttributePoint, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT a.attribute_id, a.name, a.value, a.worst, a.threshold, a.raw
		FROM smart_attributes a
		JOIN LATERAL (
			SELECT id FROM smart_samples
			WHERE drive_id = ?
			ORDER BY id DESC
			LIMIT 1
		) s ON a.sample_id = s.id
		ORDER BY a.attribute_id
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AttributePoint{}
	for rows.Next() {
		var item AttributePoint
		if err := rows.Scan(&item.AttributeID, &item.Name, &item.Value, &item.Worst, &item.Threshold, &item.Raw); err != nil {
			return nil, err
		}
		item.Status = classifyAttribute(item)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (d *DuckDB) DriveTestRuns(ctx context.Context, id int64, page int, pageSize int) (*SmartTestRunPage, error) {
	offset := (page - 1) * pageSize

	var total int
	if err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM smart_test_runs WHERE drive_id = ?`, id).Scan(&total); err != nil {
		return nil, err
	}

	rows, err := d.db.QueryContext(ctx, `
		SELECT id, test_type, scheduled_at, started_at, finished_at, status, COALESCE(message, '')
		FROM smart_test_runs
		WHERE drive_id = ?
		ORDER BY started_at DESC
		LIMIT ?
		OFFSET ?
	`, id, pageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SmartTestRun{}
	for rows.Next() {
		var item SmartTestRun
		var finishedAt sql.NullTime
		if err := rows.Scan(&item.ID, &item.TestType, &item.ScheduledAt, &item.StartedAt, &finishedAt, &item.Status, &item.Message); err != nil {
			return nil, err
		}
		if finishedAt.Valid {
			t := finishedAt.Time
			item.FinishedAt = &t
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &SmartTestRunPage{
		Items:    out,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	}, nil
}

func classifyAttribute(a AttributePoint) string {
	if a.Threshold <= 0 {
		return "GREEN"
	}
	if a.Value <= a.Threshold {
		return "RED"
	}
	// Only warn if value is within 10% of threshold (but at least 1 point)
	margin := a.Threshold / 10
	if margin < 1 {
		margin = 1
	}
	if a.Value <= a.Threshold+margin {
		return "YELLOW"
	}
	return "GREEN"
}
