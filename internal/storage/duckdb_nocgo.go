//go:build !cgo

package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"diskmon/internal/health"
	"diskmon/internal/smart"
)

var ErrCGODisabled = errors.New("duckdb storage requires cgo; rebuild with CGO_ENABLED=1 and a linux cross-compiler")

type DuckDB struct{}

func OpenDuckDB(path string) (*DuckDB, error) {
	return nil, ErrCGODisabled
}

func (d *DuckDB) Close() error {
	return ErrCGODisabled
}

func (d *DuckDB) Conn(ctx context.Context) (*sql.Conn, error) {
	return nil, ErrCGODisabled
}

func (d *DuckDB) Ready(ctx context.Context) error {
	return ErrCGODisabled
}

func (d *DuckDB) InsertSample(ctx context.Context, info smart.DriveInfo, sample smart.SmartSample, result health.Result) (int64, error) {
	return 0, ErrCGODisabled
}

func (d *DuckDB) InsertSmartTestRun(ctx context.Context, info smart.DriveInfo, run SmartTestRun) (int64, error) {
	return 0, ErrCGODisabled
}

func (d *DuckDB) ListDrives(ctx context.Context) ([]DriveSummary, error) {
	return nil, ErrCGODisabled
}

func (d *DuckDB) GetDrive(ctx context.Context, id int64) (*DriveDetail, error) {
	return nil, ErrCGODisabled
}

func (d *DuckDB) DriveHistory(ctx context.Context, id int64, limit int) ([]HistoryPoint, error) {
	return nil, ErrCGODisabled
}

func (d *DuckDB) DriveAttributes(ctx context.Context, id int64) ([]AttributePoint, error) {
	return nil, ErrCGODisabled
}

func (d *DuckDB) DriveTestRuns(ctx context.Context, id int64, page int, pageSize int) (*SmartTestRunPage, error) {
	return nil, ErrCGODisabled
}

func (d *DuckDB) MarkIncompleteSmartTestRuns(ctx context.Context, now time.Time) (int64, error) {
	return 0, ErrCGODisabled
}

func (d *DuckDB) GetNotificationState(ctx context.Context, driveID int64, notificationName string) (*NotificationState, error) {
	return nil, ErrCGODisabled
}

func (d *DuckDB) UpsertNotificationState(ctx context.Context, driveID int64, notificationName string, state string, updatedAt time.Time) error {
	return ErrCGODisabled
}
