//go:build cgo

package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDriveHistoryRangeUsesBoundsAndRollupTemperatures(t *testing.T) {
	db, err := OpenDuckDB(filepath.Join(t.TempDir(), "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	_, err = db.db.Exec(`INSERT INTO drives(id,device,identity_key,first_seen_at,last_seen_at) VALUES(7,'/dev/test','test',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		id  int
		at  time.Time
		tmp int
	}{{1, now.Add(-2 * time.Hour), 41}, {2, now.Add(-8 * 24 * time.Hour), 50}} {
		_, err := db.db.Exec(`INSERT INTO smart_samples(id,drive_id,collected_at,temperature) VALUES(?,7,?,?)`, sample.id, sample.at, sample.tmp)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.db.Exec(`INSERT INTO smart_hourly_rollups(drive_id,bucket_start,bucket_end,sample_count,temp_count,temp_min,temp_max,temp_avg,temp_last,last_sample_at,last_sample_id) VALUES(7,?,?,2,2,40.25,60.5,48.75,49,?,3)`, now.Add(-3*time.Hour), now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	result, err := db.DriveHistoryRange(context.Background(), 7, "24h")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Points) != 2 || result.Resolution != "mixed" {
		t.Fatalf("24h history points=%+v resolution=%q", result.Points, result.Resolution)
	}
	var rollup *RangeHistoryPoint
	for i := range result.Points {
		if result.Points[i].Resolution == "hourly" {
			rollup = &result.Points[i]
		}
	}
	if rollup == nil || rollup.Temperature == nil || *rollup.Temperature != 48.75 || rollup.TemperatureMax == nil || *rollup.TemperatureMax != 60.5 {
		t.Fatalf("expected rollup average and peak, got %+v", rollup)
	}
	week, err := db.DriveHistoryRange(context.Background(), 7, "7d")
	if err != nil {
		t.Fatal(err)
	}
	if len(week.Points) != 2 {
		t.Fatalf("7d range should exclude the 8-day sample; points=%+v", week.Points)
	}

	for i := 0; i < 1200; i++ {
		at := now.Add(-time.Duration(i+100) * time.Hour)
		peak := 45.0
		if i == 601 {
			peak = 99
		}
		_, err = db.db.Exec(`INSERT INTO smart_hourly_rollups(drive_id,bucket_start,bucket_end,sample_count,temp_count,temp_min,temp_max,temp_avg,temp_last,last_sample_at,last_sample_id) VALUES(7,?,?,1,1,40,?,?,45,?,?)`, at, at.Add(time.Hour), peak, 45.125, at, int64(i+10))
		if err != nil {
			t.Fatal(err)
		}
	}
	all, err := db.DriveHistoryRange(context.Background(), 7, "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Points) > maxHistoryPoints {
		t.Fatalf("all range returned %d points", len(all.Points))
	}
	foundPeak := false
	for _, point := range all.Points {
		if point.TemperatureMax != nil && *point.TemperatureMax == 99 {
			foundPeak = true
		}
	}
	if !foundPeak {
		t.Fatal("downsampling omitted the maximum temperature peak")
	}
}
