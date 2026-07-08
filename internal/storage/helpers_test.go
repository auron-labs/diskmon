//go:build cgo

package storage

import (
	"context"
	"testing"
	"time"

	"diskmon/internal/health"
	"diskmon/internal/smart"
)

func TestClassifyAttribute(t *testing.T) {
	cases := []struct {
		name string
		in   AttributePoint
		want string
	}{
		{
			name: "no threshold green",
			in:   AttributePoint{Threshold: 0, Value: 1},
			want: "GREEN",
		},
		{
			name: "at threshold red",
			in:   AttributePoint{Threshold: 10, Value: 10},
			want: "RED",
		},
		{
			name: "below threshold red",
			in:   AttributePoint{Threshold: 10, Value: 9},
			want: "RED",
		},
		{
			name: "within warn margin yellow",
			in:   AttributePoint{Threshold: 100, Value: 108},
			want: "YELLOW",
		},
		{
			name: "small threshold min margin yellow",
			in:   AttributePoint{Threshold: 5, Value: 6},
			want: "YELLOW",
		},
		{
			name: "well above threshold green",
			in:   AttributePoint{Threshold: 100, Value: 130},
			want: "GREEN",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyAttribute(tc.in)
			if got != tc.want {
				t.Fatalf("classifyAttribute(%+v)=%s want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestNullHelpers(t *testing.T) {
	if got := nullInt(nil); got != nil {
		t.Fatalf("expected nil for nullInt(nil), got %#v", got)
	}
	if got := nullInt64(nil); got != nil {
		t.Fatalf("expected nil for nullInt64(nil), got %#v", got)
	}

	i := 7
	i64 := int64(9)
	if got := nullInt(&i); got != 7 {
		t.Fatalf("expected 7, got %#v", got)
	}
	if got := nullInt64(&i64); got != int64(9) {
		t.Fatalf("expected 9, got %#v", got)
	}
}

func TestDriveIdentityKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info smart.DriveInfo
		want string
	}{
		{
			name: "prefers normalized wwn",
			info: smart.DriveInfo{
				Device: "/dev/sda",
				Model:  "Model X",
				Serial: "SN123",
				WWN:    "  0xABcD  ",
			},
			want: "wwn:0xabcd",
		},
		{
			name: "falls back to normalized serial and model",
			info: smart.DriveInfo{
				Device: "/dev/sdb",
				Model:  "  Samsung   SSD  870 EVO  ",
				Serial: "  S3Z9   NX0T123456  ",
			},
			want: "serial-model:s3z9 nx0t123456|samsung ssd 870 evo",
		},
		{
			name: "falls back to normalized device",
			info: smart.DriveInfo{
				Device: "  /DEV/DISK3  ",
				Model:  "",
				Serial: "only-serial",
			},
			want: "device:/dev/disk3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := driveIdentityKey(tt.info); got != tt.want {
				t.Fatalf("driveIdentityKey(%+v)=%q want %q", tt.info, got, tt.want)
			}
		})
	}
}

func TestInsertSampleStoresNonEmptyIdentityKey(t *testing.T) {
	db := openTestDuckDB(t)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	collectedAt := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	info := smart.DriveInfo{
		Device: " /dev/sda ",
		Model:  "  Samsung SSD 870 EVO  ",
		Serial: "  S3Z9NX0T123456  ",
	}

	if _, err := db.InsertSample(ctx, info, smart.SmartSample{CollectedAt: collectedAt, RawJSON: `{}`}, health.Result{Status: health.StatusGreen, Score: 100}); err != nil {
		t.Fatalf("InsertSample returned error: %v", err)
	}

	var identityKey string
	if err := db.db.QueryRowContext(ctx, `SELECT identity_key FROM drives WHERE device = ?`, info.Device).Scan(&identityKey); err != nil {
		t.Fatalf("query identity_key returned error: %v", err)
	}
	if identityKey == "" {
		t.Fatal("expected non-empty identity_key")
	}
	if want := driveIdentityKey(info); identityKey != want {
		t.Fatalf("identity_key=%q want %q", identityKey, want)
	}
}
