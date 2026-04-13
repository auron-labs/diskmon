package api

import (
	"reflect"
	"testing"
)

func TestAugmentDriveResponseAddsGuidance(t *testing.T) {
	item := struct {
		HealthReasons string `json:"health_reasons"`
		Health        string `json:"health"`
	}{
		HealthReasons: "PENDING_SECTORS_NONZERO,UDMA_CRC_ERRORS_NONZERO",
		Health:        "RED",
	}

	payload, ok := augmentDriveResponse(item).(map[string]any)
	if !ok {
		t.Fatalf("expected map payload")
	}

	got, ok := payload["health_guidance"].([]any)
	if !ok {
		t.Fatalf("expected health_guidance array, got %#v", payload["health_guidance"])
	}

	want := []any{
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
		"health_guidance": []any{"Use the stored guidance"},
	}

	payload, ok := augmentDriveResponse(item).(map[string]any)
	if !ok {
		t.Fatalf("expected map payload")
	}

	if !reflect.DeepEqual(payload["health_guidance"], item["health_guidance"]) {
		t.Fatalf("expected existing guidance to be preserved, got %#v", payload["health_guidance"])
	}
}
