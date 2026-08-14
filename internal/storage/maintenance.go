//go:build cgo

package storage

import (
	"context"
	"strings"
	"time"
)

// MarkIncompleteSmartTestRuns marks non-terminal smart_test_runs as INCOMPLETE.
// Runs whose drive's device is in skipDevices are left untouched so that tests
// still genuinely running on the drive are not falsely marked incomplete.
func (d *DuckDB) MarkIncompleteSmartTestRuns(ctx context.Context, now time.Time, skipDevices []string) (int64, error) {
	prefix := "daemon restarted before final SMART self-test result was recorded at " + now.UTC().Format(time.RFC3339) + "; previous status: "

	excludeClause := ""
	args := []any{SmartTestStatusIncomplete, prefix, prefix}
	if len(skipDevices) > 0 {
		placeholders := make([]string, len(skipDevices))
		for i, device := range skipDevices {
			placeholders[i] = "?"
			args = append(args, device)
		}
		excludeClause = " AND d.id NOT IN (SELECT id FROM drives WHERE device IN (" + strings.Join(placeholders, ", ") + "))"
	}

	query := `
		UPDATE smart_test_runs r
		SET status = ?,
		    message = CASE
		        WHEN message IS NULL OR TRIM(message) = '' THEN ? || UPPER(COALESCE(NULLIF(TRIM(status), ''), 'UNKNOWN'))
		        ELSE message || chr(10) || ? || UPPER(COALESCE(NULLIF(TRIM(status), ''), 'UNKNOWN'))
		    END
		FROM drives d
		WHERE r.drive_id = d.id
		    AND UPPER(COALESCE(NULLIF(TRIM(r.status), ''), 'UNKNOWN')) NOT IN ('FAILED', 'PASSED', 'SUCCESS', 'COMPLETED', 'UNKNOWN', 'INCOMPLETE')
	` + excludeClause

	res, err := d.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
