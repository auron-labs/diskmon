package api

import (
	"encoding/json"
	"reflect"
	"testing"

	"diskmon/internal/health"
	"diskmon/internal/storage"
)

func TestAugmentDriveResponseAddsGuidance(t *testing.T) {
	item := &storage.DriveDetail{
		HealthReasons: "PENDING_SECTORS_NONZERO,UDMA_CRC_ERRORS_NONZERO",
		Health:        "RED",
	}

	response := augmentDriveResponse(item)
	got := response.HealthGuidance

	want := []string{
		"Back up data now and run an extended SMART self-test. Replace the drive if pending sectors persist.",
		"Check and reseat the data or power connection, then monitor whether CRC errors continue increasing.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected guidance:\nwant: %#v\n got: %#v", want, got)
	}
	if response.Health != "RED" {
		t.Fatalf("expected existing fields to remain intact, got %#v", response)
	}
}

func TestAugmentDriveResponseOmitsGuidanceWhenEmpty(t *testing.T) {
	item := &storage.DriveDetail{Health: "GREEN"}

	data, err := json.Marshal(augmentDriveResponse(item))
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if _, ok := payload["health_guidance"]; ok {
		t.Fatalf("did not expect health_guidance in %#v", payload)
	}
}

func TestAugmentDriveResponseJSONPreservesExistingDriveFields(t *testing.T) {
	item := &storage.DriveDetail{
		ID:            42,
		Device:        "/dev/disk0",
		Model:         "Test Model",
		Serial:        "ABC123",
		WWN:           "wwn-1",
		HealthReasons: "PENDING_SECTORS_NONZERO",
		Health:        "RED",
		HealthScore:   10,
	}

	data, err := json.Marshal(augmentDriveResponse(item))
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	for key, want := range map[string]any{
		"id":             float64(42),
		"device":         "/dev/disk0",
		"model":          "Test Model",
		"serial":         "ABC123",
		"wwn":            "wwn-1",
		"health":         "RED",
		"health_score":   float64(10),
		"health_reasons": "PENDING_SECTORS_NONZERO",
	} {
		if got := payload[key]; got != want {
			t.Fatalf("expected %s=%#v, got %#v", key, want, got)
		}
	}

	guidance, ok := payload["health_guidance"].([]any)
	if !ok {
		t.Fatalf("expected decoded health_guidance array, got %#v", payload["health_guidance"])
	}
	wantGuidance := health.GuidanceForReasons([]string{"PENDING_SECTORS_NONZERO"})
	if len(guidance) != len(wantGuidance) {
		t.Fatalf("expected %d guidance entries, got %d", len(wantGuidance), len(guidance))
	}
	for i, entry := range guidance {
		got, ok := entry.(string)
		if !ok {
			t.Fatalf("expected guidance[%d] to decode as string, got %#v", i, entry)
		}
		if got != wantGuidance[i] {
			t.Fatalf("expected guidance[%d]=%q, got %q", i, wantGuidance[i], got)
		}
	}
}
