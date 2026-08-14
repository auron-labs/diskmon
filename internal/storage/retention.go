//go:build cgo

package storage

import (
	"context"
	"fmt"
	"time"
)

// PruneSamples deletes smart_samples (and their cascading rows in
// smart_attributes and drive_health) older than the retention window. A
// retention of zero or less disables pruning and returns 0.
//
// The cutoff is computed as now - retention. Samples whose collected_at is
// older than the cutoff are removed. The latest sample per drive is always
// preserved so the dashboard never shows an empty state.
func (d *DuckDB) PruneSamples(ctx context.Context, retention time.Duration, now time.Time) (int64, error) {
	if retention <= 0 {
		return 0, nil
	}
	cutoff := now.Add(-retention)

	// Delete attributes and health rows for samples older than the cutoff,
	// preserving the latest sample per drive.
	res, err := d.db.ExecContext(ctx, `
		DELETE FROM smart_attributes
		WHERE sample_id IN (
			SELECT s.id FROM smart_samples s
			WHERE s.collected_at < ?
			  AND s.id NOT IN (
				SELECT MAX(id) FROM smart_samples GROUP BY drive_id
			  )
		)
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune smart_attributes: %w", err)
	}
	deletedAttrs, _ := res.RowsAffected()

	if _, err := d.db.ExecContext(ctx, `
		DELETE FROM drive_health
		WHERE sample_id IN (
			SELECT s.id FROM smart_samples s
			WHERE s.collected_at < ?
			  AND s.id NOT IN (
				SELECT MAX(id) FROM smart_samples GROUP BY drive_id
			  )
		)
	`, cutoff); err != nil {
		return 0, fmt.Errorf("prune drive_health: %w", err)
	}

	res, err = d.db.ExecContext(ctx, `
		DELETE FROM smart_samples
		WHERE collected_at < ?
		  AND id NOT IN (
			SELECT MAX(id) FROM smart_samples GROUP BY drive_id
		  )
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune smart_samples: %w", err)
	}
	deletedSamples, _ := res.RowsAffected()

	return deletedAttrs + deletedSamples, nil
}
