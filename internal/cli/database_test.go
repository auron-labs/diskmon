//go:build cgo

package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"diskmon/internal/config"
	"diskmon/internal/health"
	"diskmon/internal/smart"
	"diskmon/internal/storage"
)

func TestCompactDatabaseAtomicallyReplacesAndLeavesSourceOnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diskmon.duckdb")
	db, err := storage.OpenDuckDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compactDatabase(context.Background(), path); err != nil {
		t.Fatalf("compactDatabase: %v", err)
	}
	if _, err := os.Stat(path + ".prune-backup"); !os.IsNotExist(err) {
		t.Fatalf("compaction should not create an internal backup, stat error: %v", err)
	}
	compacted, err := storage.OpenDuckDB(path)
	if err != nil {
		t.Fatalf("open compacted database: %v", err)
	}
	if err := compacted.Close(); err != nil {
		t.Fatal(err)
	}

	broken := filepath.Join(t.TempDir(), "broken.duckdb")
	content := []byte("not a duckdb database")
	if err := os.WriteFile(broken, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := compactDatabase(context.Background(), broken); err == nil {
		t.Fatal("expected compaction failure")
	}
	got, err := os.ReadFile(broken)
	if err != nil {
		t.Fatalf("source should remain after failure: %v", err)
	}
	if string(got) != string(content) {
		t.Fatal("source changed after failed compaction")
	}
	tempFiles, err := filepath.Glob(filepath.Join(filepath.Dir(broken), ".diskmon-compact-*.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tempFiles) != 0 {
		t.Fatalf("temporary compact files left after failure: %v", tempFiles)
	}
}

func TestDatabasePruneReportsAndDrainsPendingRowsWithCompactOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pending.duckdb")
	db, err := storage.OpenDuckDB(path)
	if err != nil {
		t.Fatal(err)
	}
	sampleID, _, err := db.InsertSample(context.Background(), smart.DriveInfo{Device: "/dev/test"}, smart.SmartSample{CollectedAt: time.Now().UTC(), RawJSON: `{}`}, health.Result{Status: health.StatusGreen, Score: 95})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), `INSERT INTO smart_rollup_pending_samples VALUES (?)`, sampleID); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Database = path
	cfg.RawRetention, cfg.HourlyRetention, cfg.DailyRetention = 0, 0, 0
	var dryOutput bytes.Buffer
	dryRun := newDatabaseCmd(cfg)
	dryRun.SetOut(&dryOutput)
	dryRun.SetArgs([]string{"prune", "--dry-run"})
	if err := dryRun.Execute(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(dryOutput.Bytes(), []byte("pending_remaining=1")) {
		t.Fatalf("dry run omitted pending row count: %s", dryOutput.String())
	}

	var compactOutput bytes.Buffer
	compact := newDatabaseCmd(cfg)
	compact.SetOut(&compactOutput)
	compact.SetArgs([]string{"prune", "--compact"})
	if err := compact.Execute(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(compactOutput.Bytes(), []byte("pending_deleted=1")) || !bytes.Contains(compactOutput.Bytes(), []byte("remaining=0")) {
		t.Fatalf("compact-only prune did not drain pending rows: %s", compactOutput.String())
	}
	if !bytes.Contains(compactOutput.Bytes(), []byte("Compacted database size:")) {
		t.Fatalf("expected compact size report: %s", compactOutput.String())
	}
}
