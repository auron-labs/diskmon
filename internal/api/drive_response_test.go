package api

import (
	"encoding/json"
	"reflect"
	"testing"

	"diskmon/internal/storage"
)

func TestAugmentDriveResponseAddsGuidance(t *testing.T) {
	item := &storage.DriveDetail{
		HealthReasons: "PENDING_SECTORS_NONZERO,UDMA_CRC_ERRORS_NONZERO",
		Health:        "RED",
	}

	payload, ok := augmentDriveResponse(item).(map[string]any)
	if !ok {
		t.Fatalf("expected map payload")
	}

	got, ok := payload["health_guidance"].([]string)
	if !ok {
		t.Fatalf("expected health_guidance array, got %#v", payload["health_guidance"])
	}

	want := []string{
		"Back up data now and run an extended SMART self-test. Replace the drive if pending sectors persist.",
		"Check and reseat the data or power connection, then monitor whether CRC errors continue increasing.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected guidance:\nwant: %#v\n got: %#v", want, got)
	}
	if payload["health"] != "RED" {
		t.Fatalf("expected existing fields to remain intact, got %#v", payload)
	}
}

func TestAugmentDriveResponseJSONRoundTripUsesStringArray(t *testing.T) {
	item := &storage.DriveDetail{
		HealthReasons: "PENDING_SECTORS_NONZERO",
		Health:        "RED",
	}

	data, err := json.Marshal(augmentDriveResponse(item))
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	guidance, ok := payload["health_guidance"].([]any)
	if !ok {
		t.Fatalf("expected decoded health_guidance array, got %#v", payload["health_guidance"])
	}
	if len(guidance) == 0 {
		t.Fatal("expected non-empty health_guidance")
	}
	for i, entry := range guidance {
		if _, ok := entry.(string); !ok {
			t.Fatalf("expected guidance[%d] to decode as string, got %#v", i, entry)
		}
	}
}

func TestAugmentDriveResponseLeavesUnknownPayloadUntouched(t *testing.T) {
	item := map[string]any{"health": "GREEN"}
	payload, ok := augmentDriveResponse(item).(map[string]any)
	if !ok {
		t.Fatalf("expected map payload")
	}
	if _, exists := payload["health_guidance"]; exists {
		t.Fatalf("did not expect health_guidance in %#v", payload)
	}
	if !reflect.DeepEqual(payload, item) {
		t.Fatalf("expected payload to remain unchanged")
	}
}

func TestAugmentDriveResponsePreservesExistingGuidance(t *testing.T) {
	item := map[string]any{
		"health":          "YELLOW",
		"health_reasons":  []any{"REALLOCATED_SECTORS_NONZERO"},
		"health_guidance": []string{"Use the stored guidance"},
	}

	payload, ok := augmentDriveResponse(item).(map[string]any)
	if !ok {
		t.Fatalf("expected map payload")
	}

	if !reflect.DeepEqual(payload["health_guidance"], item["health_guidance"]) {
		t.Fatalf("expected existing guidance to be preserved, got %#v", payload["health_guidance"])
	}
}
