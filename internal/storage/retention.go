//go:build cgo

package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const defaultBucketSampleLimit = 1000

// TierMaintenance runs at most one bounded bucket in each direction. Repeat it
// to drain offline backlogs. Rollups and raw-delete intents commit atomically;
// DuckDB's FK limitation requires the journaled child/parent deletes to follow.
func (d *DuckDB) TierMaintenance(ctx context.Context, now time.Time, policy TierPolicy) (TierMaintenanceResult, error) {
	var result TierMaintenanceResult
	if policy.RawRetention < 0 || policy.HourlyRetention < 0 || policy.DailyRetention < 0 {
		return result, fmt.Errorf("tier retention durations cannot be negative")
	}
	limit := policy.MaxSamplesPerBucket
	if limit <= 0 {
		limit = defaultBucketSampleLimit
	}
	if !policy.DryRun {
		// A prior pass may have committed rollups and pending FK-safe deletes.
		// Replay those intents even when raw downsampling is now disabled.
		deleted, err := d.flushPendingSamples(ctx, limit)
		result.PendingSamplesDeleted += deleted
		if err != nil {
			return result, err
		}
		_, deleted, err = d.flushPruneJournal(ctx, limit)
		result.PendingSamplesDeleted += deleted
		if err != nil {
			return result, err
		}
	}
	var rawCutoff, hourlyCutoff, dailyCutoff time.Time
	if policy.RawRetention > 0 {
		rawCutoff = now.Add(-policy.RawRetention)
	}
	if policy.HourlyRetention > 0 {
		hourlyCutoff = now.Add(-policy.HourlyRetention)
	}
	if policy.DailyRetention > 0 {
		dailyCutoff = now.Add(-policy.DailyRetention)
	}
	var err error
	if policy.DryRun {
		result.PendingRowsRemaining, err = d.countPendingRows(ctx, limit)
		if err != nil {
			return result, err
		}
		if policy.RawRetention > 0 {
			result.RawSamplesRemaining, err = d.countRawEligible(ctx, rawCutoff, limit)
			if err != nil {
				return result, err
			}
		}
		if policy.HourlyRetention > 0 {
			if err = d.countRollup(ctx, "smart_hourly_rollups", hourlyCutoff, limit, &result.HourlyRowsRemaining); err != nil {
				return result, err
			}
		}
		if policy.DailyRetention > 0 {
			if err = d.countRollup(ctx, "smart_daily_rollups", dailyCutoff, limit, &result.DailyRowsRemaining); err != nil {
				return result, err
			}
		}
		return result, nil
	}
	if policy.RawRetention > 0 {
		if result.RawSamplesRolled, err = d.rollRawBucket(ctx, rawCutoff, limit); err != nil {
			return result, err
		}
		deleted, flushErr := d.flushPendingSamples(ctx, limit)
		result.PendingSamplesDeleted += deleted
		if flushErr != nil {
			return result, flushErr
		}
	}
	if policy.HourlyRetention > 0 {
		if result.HourlyRowsRolled, err = d.rollHourlyBucket(ctx, hourlyCutoff, limit); err != nil {
			return result, err
		}
	}
	if policy.DailyRetention > 0 {
		if result.DailyRowsDeleted, err = d.deleteDaily(ctx, dailyCutoff, limit); err != nil {
			return result, err
		}
	}
	if policy.RawRetention > 0 {
		if result.RawSamplesRemaining, err = d.countRawEligible(ctx, rawCutoff, limit); err != nil {
			return result, err
		}
	}
	if policy.HourlyRetention > 0 {
		if err = d.countRollup(ctx, "smart_hourly_rollups", hourlyCutoff, limit, &result.HourlyRowsRemaining); err != nil {
			return result, err
		}
	}
	if policy.DailyRetention > 0 {
		if err = d.countRollup(ctx, "smart_daily_rollups", dailyCutoff, limit, &result.DailyRowsRemaining); err != nil {
			return result, err
		}
	}
	if result.PendingRowsRemaining, err = d.countPendingRows(ctx, limit); err != nil {
		return result, err
	}
	return result, nil
}

func (d *DuckDB) countPendingRows(ctx context.Context, limit int) (int64, error) {
	var rollup, prune int64
	for _, item := range []struct {
		table string
		out   *int64
	}{{"smart_rollup_pending_samples", &rollup}, {"smart_prune_pending_samples", &prune}} {
		if err := d.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM (SELECT sample_id FROM %s LIMIT ?) pending`, item.table), limit).Scan(item.out); err != nil {
			return 0, fmt.Errorf("count pending %s: %w", item.table, err)
		}
	}
	total := rollup + prune
	if total > int64(limit) {
		total = int64(limit)
	}
	return total, nil
}

func (d *DuckDB) countRawEligible(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id FROM drives ORDER BY id`)
	if err != nil {
		return 0, err
	}
	var drives []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		drives = append(drives, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	var total int64
	for _, drive := range drives {
		var n int64
		err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT s.id FROM smart_samples s WHERE s.drive_id=? AND s.collected_at<? AND NOT EXISTS(SELECT 1 FROM smart_rollup_pending_samples p WHERE p.sample_id=s.id) AND s.id<>(SELECT x.id FROM smart_samples x WHERE x.drive_id=s.drive_id ORDER BY x.collected_at DESC,x.id DESC LIMIT 1) LIMIT ?) x`, drive, cutoff, limit).Scan(&n)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}
func (d *DuckDB) countRollup(ctx context.Context, table string, cutoff time.Time, limit int, out *int64) error {
	err := d.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM (SELECT 1 FROM %s WHERE bucket_start<? LIMIT ?) x`, table), cutoff, limit).Scan(out)
	return err
}

// Rollup shape shared by both tiers. Nullable metrics stay NULL when absent.
type tierSample struct {
	id, drive                                          int64
	at                                                 time.Time
	temp, power, realloc, pending, uncorrectable, wear sql.NullInt64
	status                                             sql.NullString
	score                                              sql.NullInt64
	count, tempCount                                   int64
	tempMin, tempMax, tempAvg                          sql.NullFloat64
	reallocMax, pendingMax, uncMax, wearMin            sql.NullInt64
}
type tierRollup struct {
	drive                                                                                       int64
	start, end, lastAt                                                                          time.Time
	count                                                                                       int64
	tempMin, tempMax, tempSum                                                                   float64
	tempN                                                                                       int64
	tempLast                                                                                    sql.NullInt64
	power, reallocMax, reallocLast, pendingMax, pendingLast, uncMax, uncLast, wearMin, wearLast sql.NullInt64
	status                                                                                      sql.NullString
	score                                                                                       sql.NullInt64
	lastID                                                                                      int64
}

func (d *DuckDB) rollRawBucket(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	return d.rollBucket(ctx, `smart_samples`, `smart_hourly_rollups`, cutoff, limit, time.Hour, true)
}
func (d *DuckDB) rollHourlyBucket(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	return d.rollBucket(ctx, `smart_hourly_rollups`, `smart_daily_rollups`, cutoff, limit, 24*time.Hour, false)
}

func (d *DuckDB) rollBucket(ctx context.Context, source, target string, cutoff time.Time, limit int, width time.Duration, raw bool) (int64, error) {
	var drive int64
	var start time.Time
	q := `SELECT drive_id, date_trunc('day',bucket_start) FROM smart_hourly_rollups WHERE bucket_start < ? ORDER BY bucket_start,drive_id LIMIT 1`
	var err error
	if raw {
		drive, start, err = d.oldestRawBucket(ctx, cutoff)
	} else {
		err = d.db.QueryRowContext(ctx, q, cutoff).Scan(&drive, &start)
	}
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("select rollup bucket: %w", err)
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var samples []tierSample
	if raw {
		q = `SELECT s.id,s.drive_id,s.collected_at,s.temperature,s.power_on_hours,s.reallocated_sectors,s.pending_sectors,s.uncorrectable_sectors,s.wear_level,h.status,h.score FROM smart_samples s LEFT JOIN drive_health h ON h.sample_id=s.id WHERE s.drive_id=? AND s.collected_at>=? AND s.collected_at<? AND s.collected_at < ? AND NOT EXISTS(SELECT 1 FROM smart_rollup_pending_samples p WHERE p.sample_id=s.id) AND s.id <> (SELECT id FROM smart_samples x WHERE x.drive_id=s.drive_id ORDER BY collected_at DESC,id DESC LIMIT 1) ORDER BY s.collected_at,s.id LIMIT ?`
	} else {
		q = `SELECT last_sample_id,drive_id,last_sample_at,temp_last,power_on_hours_last,reallocated_last,pending_last,uncorrectable_last,wear_last,health_status,health_score,sample_count,temp_count,temp_min,temp_max,temp_avg,reallocated_max,pending_max,uncorrectable_max,wear_min FROM smart_hourly_rollups WHERE drive_id=? AND bucket_start>=? AND bucket_start<? AND bucket_start < ? ORDER BY bucket_start LIMIT ?`
	}
	args := []any{drive, start, start.Add(time.Hour), cutoff, limit}
	if !raw {
		args = []any{drive, start, start.Add(24 * time.Hour), cutoff, limit}
	}
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var s tierSample
		if raw {
			err = rows.Scan(&s.id, &s.drive, &s.at, &s.temp, &s.power, &s.realloc, &s.pending, &s.uncorrectable, &s.wear, &s.status, &s.score)
		} else {
			err = rows.Scan(&s.id, &s.drive, &s.at, &s.temp, &s.power, &s.realloc, &s.pending, &s.uncorrectable, &s.wear, &s.status, &s.score, &s.count, &s.tempCount, &s.tempMin, &s.tempMax, &s.tempAvg, &s.reallocMax, &s.pendingMax, &s.uncMax, &s.wearMin)
		}
		if err != nil {
			rows.Close()
			return 0, err
		}
		samples = append(samples, s)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	if len(samples) == 0 {
		return 0, nil
	}
	roll, err := loadTierRollup(ctx, tx, target, drive, start)
	if err != nil {
		return 0, err
	}
	roll.drive, roll.start, roll.end = drive, start, start.Add(width)
	for _, s := range samples {
		if raw {
			roll.add(s)
		} else {
			roll.addRollup(s)
		}
	}
	if err := saveTierRollup(ctx, tx, target, roll); err != nil {
		return 0, err
	}
	ids := make([]any, len(samples))
	for i, s := range samples {
		ids[i] = s.id
	}
	if raw {
		for _, id := range ids {
			if _, err = tx.ExecContext(ctx, `INSERT INTO smart_rollup_pending_samples(sample_id) VALUES (?) ON CONFLICT DO NOTHING`, id); err != nil {
				return 0, err
			}
		}
	} else {
		if err = deleteByIDs(ctx, tx, "smart_hourly_rollups", "last_sample_id", ids); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit rollup bucket: %w", err)
	}
	return int64(len(samples)), nil
}

// Use the existing (drive_id,collected_at) index one drive at a time rather
// than forcing a global scan/index build across the potentially huge raw table.
func (d *DuckDB) oldestRawBucket(ctx context.Context, cutoff time.Time) (int64, time.Time, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id FROM drives ORDER BY id`)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("list drives for raw retention: %w", err)
	}
	var chosenDrive, chosenID int64
	var chosenAt time.Time
	for rows.Next() {
		var drive int64
		if err := rows.Scan(&drive); err != nil {
			rows.Close()
			return 0, time.Time{}, err
		}
		var id int64
		var at, bucket time.Time
		err := d.db.QueryRowContext(ctx, `SELECT s.id,s.collected_at,date_trunc('hour',s.collected_at) FROM smart_samples s WHERE s.drive_id=? AND s.collected_at<? AND NOT EXISTS(SELECT 1 FROM smart_rollup_pending_samples p WHERE p.sample_id=s.id) AND s.id<>(SELECT x.id FROM smart_samples x WHERE x.drive_id=s.drive_id ORDER BY x.collected_at DESC,x.id DESC LIMIT 1) ORDER BY s.collected_at,s.id LIMIT 1`, drive, cutoff).Scan(&id, &at, &bucket)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			rows.Close()
			return 0, time.Time{}, fmt.Errorf("find oldest raw sample for drive %d: %w", drive, err)
		}
		if chosenID == 0 || at.Before(chosenAt) || at.Equal(chosenAt) && id < chosenID {
			chosenDrive, chosenID, chosenAt = drive, id, bucket
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, time.Time{}, err
	}
	rows.Close()
	if chosenID == 0 {
		return 0, time.Time{}, sql.ErrNoRows
	}
	return chosenDrive, chosenAt, nil
}

func (d *DuckDB) flushPendingSamples(ctx context.Context, limit int) (int64, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT sample_id FROM smart_rollup_pending_samples ORDER BY sample_id LIMIT ?`, limit)
	if err != nil {
		return 0, fmt.Errorf("load pending sample deletions: %w", err)
	}
	var ids []any
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	if len(ids) == 0 {
		return 0, nil
	}
	// DuckDB rejects deleting referenced and referencing rows in one transaction.
	// The rollup and pending IDs committed together; replaying these autocommit
	// deletes after a restart is idempotent and cannot double-count samples.
	var deleted int64
	for _, child := range []struct{ table, col string }{{"smart_attributes", "sample_id"}, {"drive_health", "sample_id"}, {"smart_samples", "id"}} {
		res, err := d.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s IN (%s)", child.table, child.col, strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")), ids...)
		if err != nil {
			return deleted, fmt.Errorf("delete rolled-up %s: %w", child.table, err)
		}
		if child.table == "smart_samples" {
			deleted, err = res.RowsAffected()
			if err != nil {
				return 0, err
			}
		}
	}
	if err := deleteByIDsDB(ctx, d.db, "smart_rollup_pending_samples", "sample_id", ids); err != nil {
		return deleted, fmt.Errorf("clear pending sample deletions: %w", err)
	}
	return deleted, nil
}

func deleteByIDsDB(ctx context.Context, db *sql.DB, table, column string, ids []any) error {
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	_, err := db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s IN (%s)", table, column, marks), ids...)
	return err
}

func deleteByIDs(ctx context.Context, tx *sql.Tx, table, column string, ids []any) error {
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	_, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s IN (%s)", table, column, marks), ids...)
	return err
}

func (r *tierRollup) add(s tierSample) {
	r.count++
	if s.temp.Valid {
		v := float64(s.temp.Int64)
		if r.tempN == 0 || v < r.tempMin {
			r.tempMin = v
		}
		if r.tempN == 0 || v > r.tempMax {
			r.tempMax = v
		}
		r.tempSum += v
		r.tempN++
		r.tempLast = s.temp
	}
	r.power = s.power
	updateMax(&r.reallocMax, s.realloc)
	r.reallocLast = s.realloc
	updateMax(&r.pendingMax, s.pending)
	r.pendingLast = s.pending
	updateMax(&r.uncMax, s.uncorrectable)
	r.uncLast = s.uncorrectable
	if s.wear.Valid && (!r.wearMin.Valid || s.wear.Int64 < r.wearMin.Int64) {
		r.wearMin = s.wear
	}
	r.wearLast = s.wear
	if s.status.Valid && (!r.status.Valid || healthRank(s.status.String) > healthRank(r.status.String)) {
		r.status = s.status
	}
	if s.score.Valid && (!r.score.Valid || s.score.Int64 < r.score.Int64) {
		r.score = s.score
	}
	if r.lastID == 0 || s.at.After(r.lastAt) || s.at.Equal(r.lastAt) && s.id > r.lastID {
		r.lastAt = s.at
		r.lastID = s.id
		r.power = s.power
		r.tempLast = s.temp
		r.reallocLast = s.realloc
		r.pendingLast = s.pending
		r.uncLast = s.uncorrectable
		r.wearLast = s.wear
	}
}

func (r *tierRollup) addRollup(s tierSample) {
	r.count += s.count
	if s.tempCount > 0 && s.tempAvg.Valid {
		if r.tempN == 0 || s.tempMin.Valid && s.tempMin.Float64 < r.tempMin {
			r.tempMin = s.tempMin.Float64
		}
		if r.tempN == 0 || s.tempMax.Valid && s.tempMax.Float64 > r.tempMax {
			r.tempMax = s.tempMax.Float64
		}
		r.tempN += s.tempCount
		r.tempSum += s.tempAvg.Float64 * float64(s.tempCount)
	}
	updateMax(&r.reallocMax, s.reallocMax)
	updateMax(&r.pendingMax, s.pendingMax)
	updateMax(&r.uncMax, s.uncMax)
	if s.wearMin.Valid && (!r.wearMin.Valid || s.wearMin.Int64 < r.wearMin.Int64) {
		r.wearMin = s.wearMin
	}
	if s.status.Valid && (!r.status.Valid || healthRank(s.status.String) > healthRank(r.status.String)) {
		r.status = s.status
	}
	if s.score.Valid && (!r.score.Valid || s.score.Int64 < r.score.Int64) {
		r.score = s.score
	}
	if r.lastID == 0 || s.at.After(r.lastAt) || s.at.Equal(r.lastAt) && s.id > r.lastID {
		r.lastAt = s.at
		r.lastID = s.id
		r.tempLast = s.temp
		r.power = s.power
		r.reallocLast = s.realloc
		r.pendingLast = s.pending
		r.uncLast = s.uncorrectable
		r.wearLast = s.wear
	}
}
func updateMax(dst *sql.NullInt64, v sql.NullInt64) {
	if v.Valid && (!dst.Valid || v.Int64 > dst.Int64) {
		*dst = v
	}
}
func healthRank(s string) int {
	switch s {
	case "RED":
		return 4
	case "YELLOW":
		return 3
	case "UNKNOWN":
		return 2
	case "GREEN":
		return 1
	}
	return 0
}

func loadTierRollup(ctx context.Context, tx *sql.Tx, table string, drive int64, start time.Time) (tierRollup, error) {
	var r tierRollup
	var min, max, avg sql.NullFloat64
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT sample_count,temp_count,temp_min,temp_max,temp_avg,temp_last,power_on_hours_last,reallocated_max,reallocated_last,pending_max,pending_last,uncorrectable_max,uncorrectable_last,wear_min,wear_last,health_status,health_score,last_sample_at,last_sample_id FROM %s WHERE drive_id=? AND bucket_start=?`, table), drive, start).Scan(&r.count, &r.tempN, &min, &max, &avg, &r.tempLast, &r.power, &r.reallocMax, &r.reallocLast, &r.pendingMax, &r.pendingLast, &r.uncMax, &r.uncLast, &r.wearMin, &r.wearLast, &r.status, &r.score, &r.lastAt, &r.lastID)
	if err == sql.ErrNoRows {
		return tierRollup{}, nil
	}
	if err != nil {
		return r, fmt.Errorf("load %s bucket: %w", table, err)
	}
	if min.Valid {
		r.tempMin = min.Float64
	}
	if max.Valid {
		r.tempMax = max.Float64
	}
	if avg.Valid {
		r.tempSum = avg.Float64 * float64(r.tempN)
	}
	return r, nil
}

func saveTierRollup(ctx context.Context, tx *sql.Tx, table string, r tierRollup) error {
	var avg any
	if r.tempN > 0 {
		avg = r.tempSum / float64(r.tempN)
	}
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (drive_id,bucket_start,bucket_end,sample_count,temp_count,temp_min,temp_max,temp_avg,temp_last,power_on_hours_last,reallocated_max,reallocated_last,pending_max,pending_last,uncorrectable_max,uncorrectable_last,wear_min,wear_last,health_status,health_score,last_sample_at,last_sample_id) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT (drive_id,bucket_start) DO UPDATE SET bucket_end=excluded.bucket_end,sample_count=excluded.sample_count,temp_count=excluded.temp_count,temp_min=excluded.temp_min,temp_max=excluded.temp_max,temp_avg=excluded.temp_avg,temp_last=excluded.temp_last,power_on_hours_last=excluded.power_on_hours_last,reallocated_max=excluded.reallocated_max,reallocated_last=excluded.reallocated_last,pending_max=excluded.pending_max,pending_last=excluded.pending_last,uncorrectable_max=excluded.uncorrectable_max,uncorrectable_last=excluded.uncorrectable_last,wear_min=excluded.wear_min,wear_last=excluded.wear_last,health_status=excluded.health_status,health_score=excluded.health_score,last_sample_at=excluded.last_sample_at,last_sample_id=excluded.last_sample_id`, table), r.drive, r.start, r.end, r.count, r.tempN, nullableFloat(r.tempMin, r.tempN), nullableFloat(r.tempMax, r.tempN), avg, r.tempLast, r.power, r.reallocMax, r.reallocLast, r.pendingMax, r.pendingLast, r.uncMax, r.uncLast, r.wearMin, r.wearLast, r.status, r.score, r.lastAt, r.lastID)
	if err != nil {
		return fmt.Errorf("save %s bucket: %w", table, err)
	}
	return nil
}
func nullableFloat(v float64, n int64) any {
	if n == 0 {
		return nil
	}
	return v
}

func (d *DuckDB) deleteDaily(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	res, err := d.db.ExecContext(ctx, `DELETE FROM smart_daily_rollups WHERE rowid IN (SELECT rowid FROM smart_daily_rollups WHERE bucket_start < ? ORDER BY bucket_start,drive_id LIMIT ?)`, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("delete expired daily rollups: %w", err)
	}
	return res.RowsAffected()
}

// PruneSamples retains its legacy hard-delete behavior and latest-sample guard.
func (d *DuckDB) PruneSamples(ctx context.Context, retention time.Duration, now time.Time) (int64, error) {
	if retention <= 0 {
		return 0, nil
	}
	cutoff := now.Add(-retention)
	deleted, _, err := d.flushPruneJournal(ctx, defaultBucketSampleLimit)
	if err != nil {
		return deleted, err
	}
	rows, err := d.db.QueryContext(ctx, `SELECT id FROM drives ORDER BY id`)
	if err != nil {
		return deleted, err
	}
	var drives []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return deleted, err
		}
		drives = append(drives, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return deleted, err
	}
	rows.Close()
	for _, drive := range drives {
		for {
			ids, err := d.prunableSampleIDs(ctx, drive, cutoff, defaultBucketSampleLimit)
			if err != nil {
				return deleted, err
			}
			if len(ids) == 0 {
				break
			}
			tx, err := d.db.BeginTx(ctx, nil)
			if err != nil {
				return deleted, err
			}
			for _, id := range ids {
				if _, err = tx.ExecContext(ctx, `INSERT INTO smart_prune_pending_samples(sample_id) VALUES (?) ON CONFLICT DO NOTHING`, id); err != nil {
					_ = tx.Rollback()
					return deleted, err
				}
			}
			if err = tx.Commit(); err != nil {
				return deleted, err
			}
			n, _, err := d.flushPruneJournal(ctx, defaultBucketSampleLimit)
			deleted += n
			if err != nil {
				return deleted, err
			}
		}
	}
	return deleted, nil
}

func (d *DuckDB) prunableSampleIDs(ctx context.Context, drive int64, cutoff time.Time, limit int) ([]int64, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT s.id FROM smart_samples s WHERE s.drive_id=? AND s.collected_at<? AND s.id<>(SELECT x.id FROM smart_samples x WHERE x.drive_id=s.drive_id ORDER BY x.collected_at DESC,x.id DESC LIMIT 1) AND NOT EXISTS(SELECT 1 FROM smart_prune_pending_samples p WHERE p.sample_id=s.id) ORDER BY s.collected_at,s.id LIMIT ?`, drive, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// The committed journal makes legacy hard-pruning recoverable while child and
// parent deletes remain separate autocommit statements for DuckDB FK safety.
func (d *DuckDB) flushPruneJournal(ctx context.Context, limit int) (int64, int64, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT sample_id FROM smart_prune_pending_samples ORDER BY sample_id LIMIT ?`, limit)
	if err != nil {
		return 0, 0, err
	}
	var ids []any
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, 0, err
	}
	rows.Close()
	if len(ids) == 0 {
		return 0, 0, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	var attrs int64
	if err := d.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM smart_attributes WHERE sample_id IN (%s)`, marks), ids...).Scan(&attrs); err != nil {
		return 0, 0, err
	}
	var deleted, deletedSamples int64
	for _, child := range []struct{ table, col string }{{"smart_attributes", "sample_id"}, {"drive_health", "sample_id"}, {"smart_samples", "id"}} {
		res, err := d.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s IN (%s)`, child.table, child.col, marks), ids...)
		if err != nil {
			return deleted, deletedSamples, fmt.Errorf("prune %s: %w", child.table, err)
		}
		if child.table != "drive_health" {
			n, e := res.RowsAffected()
			if e != nil {
				return deleted, deletedSamples, e
			}
			if child.table == "smart_samples" {
				deleted += n
				deletedSamples = n
			} else {
				deleted += n
			}
		}
	}
	if err := deleteByIDsDB(ctx, d.db, "smart_prune_pending_samples", "sample_id", ids); err != nil {
		return deleted, deletedSamples, err
	}
	return deleted, deletedSamples, nil
}
