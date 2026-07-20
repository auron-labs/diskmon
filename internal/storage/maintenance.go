//go:build cgo

package storage

import (
	"context"
	"time"
)

func (d *DuckDB) MarkIncompleteSmartTestRuns(ctx context.Context, now time.Time) (int64, error) {
	prefix := "daemon restarted before final SMART self-test result was recorded at " + now.UTC().Format(time.RFC3339) + "; previous status: "
	res, err := d.db.ExecContext(ctx, `
		UPDATE smart_test_runs
		SET status = ?,
		    message = CASE
		        WHEN message IS NULL OR TRIM(message) = '' THEN ? || UPPER(COALESCE(NULLIF(TRIM(status), ''), 'UNKNOWN'))
		        ELSE message || chr(10) || ? || UPPER(COALESCE(NULLIF(TRIM(status), ''), 'UNKNOWN'))
		    END
		WHERE UPPER(COALESCE(NULLIF(TRIM(status), ''), 'UNKNOWN')) NOT IN ('FAILED', 'PASSED', 'SUCCESS', 'COMPLETED', 'UNKNOWN', 'INCOMPLETE')
	`, SmartTestStatusIncomplete, prefix, prefix)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *DuckDB) PruneSamples(ctx context.Context, retention time.Duration, now time.Time) (int64, error) {
	if retention <= 0 {
		return 0, nil
	}
	cutoff := now.UTC().Add(-retention)
	res, err := d.db.ExecContext(ctx, `DELETE FROM smart_samples WHERE collected_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
