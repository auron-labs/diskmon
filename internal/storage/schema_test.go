//go:build cgo

package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenDuckDBCreatesExpectedIndexes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema-indexes.duckdb")

	db, err := OpenDuckDB(path)
	if err != nil {
		t.Fatalf("OpenDuckDB(%q) error: %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	got := queryIndexNames(t, ctx, db.db, []string{
		"idx_drives_identity_key",
		"idx_smart_samples_drive_id_id",
		"idx_drive_health_drive_id_sample_id",
		"idx_smart_samples_drive_id_collected_at",
		"idx_smart_attributes_sample_id_attribute_id",
		"idx_smart_test_runs_drive_id_started_at",
	})

	want := []string{
		"idx_drive_health_drive_id_sample_id",
		"idx_drives_identity_key",
		"idx_smart_attributes_sample_id_attribute_id",
		"idx_smart_samples_drive_id_collected_at",
		"idx_smart_samples_drive_id_id",
		"idx_smart_test_runs_drive_id_started_at",
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d indexes, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index[%d]=%q want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}
