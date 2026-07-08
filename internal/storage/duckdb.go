//go:build cgo

package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

	_ "github.com/marcboeker/go-duckdb"
)

//go:embed schema.sql
var schemaSQL string

type DuckDB struct {
	db *sql.DB
}

func OpenDuckDB(path string) (*DuckDB, error) {
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	defer conn.Close()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if err := migrateDuckDB(ctx, tx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate duckdb: %w", err)
	}
	if err := tx.Commit(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("commit duckdb migration: %w", err)
	}

	return &DuckDB{db: db}, nil
}

func (d *DuckDB) Close() error {
	return d.db.Close()
}

func (d *DuckDB) Conn(ctx context.Context) (*sql.Conn, error) {
	return d.db.Conn(ctx)
}

func (d *DuckDB) Ready(ctx context.Context) error {
	return d.db.PingContext(ctx)
}
