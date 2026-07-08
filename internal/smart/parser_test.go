package smart

import (
	"encoding/json"
	"testing"
)

func TestParseSmartJSONRejectsInvalidJSON(t *testing.T) {
	_, _, err := ParseSmartJSON("/dev/sda", []byte("not-json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON payload")
	}
}

func TestParseSmartJSONTemperatureAndModelFallback(t *testing.T) {
	payload := []byte(`{
		"model_name": "   ",
		"model_family": "Fallback Family",
		"model_number": "Model Number",
		"temperature": {"current": 310},
		"power_on_time": {"hours": 123}
	}`)

	info, sample, err := ParseSmartJSON("/dev/sdb", payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if info.Model != "Fallback Family" {
		t.Fatalf("expected model fallback, got %q", info.Model)
	}
	if sample.Temperature == nil || *sample.Temperature != 36 {
		t.Fatalf("expected kelvin->celsius conversion to 36, got %v", sample.Temperature)
	}
	if sample.PowerOnHours == nil || *sample.PowerOnHours != 123 {
		t.Fatalf("expected power on hours 123, got %v", sample.PowerOnHours)
	}
}

func TestParseSmartJSONNVMeFallbacks(t *testing.T) {
	payload := []byte(`{
		"smart_status": {"passed": false},
		"nvme_smart_health_information_log": {
			"temperature": 305,
			"media_errors": 7,
			"percentage_used": 12,
			"critical_warning": 2
		}
	}`)

	_, sample, err := ParseSmartJSON("/dev/nvme0n1", payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sample.UncorrectableSectors == nil || *sample.UncorrectableSectors != 7 {
		t.Fatalf("expected media_errors fallback, got %v", sample.UncorrectableSectors)
	}
	if sample.WearLevel == nil || *sample.WearLevel != 12 {
		t.Fatalf("expected wear level 12, got %v", sample.WearLevel)
	}
	if !sample.CriticalWarning {
		t.Fatal("expected critical warning true")
	}
	if !sample.FailingNow {
		t.Fatal("expected failing_now true when smart_status.passed=false")
	}
}

func TestParseSmartJSONAttributeRawFallback(t *testing.T) {
	fixture := map[string]any{
		"ata_smart_attributes": map[string]any{
			"table": []any{
				map[string]any{
					"id":     5,
					"name":   "Reallocated_Sector_Ct",
					"value":  100,
					"worst":  100,
					"thresh": 36,
					"raw": map[string]any{
						"value":  42,
						"string": "",
					},
				},
			},
		},
	}
	payload, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	_, sample, err := ParseSmartJSON("/dev/sda", payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(sample.Attributes) != 1 {
		t.Fatalf("expected 1 attribute, got %d", len(sample.Attributes))
	}
	if sample.Attributes[0].Raw != "42" {
		t.Fatalf("expected raw string fallback to numeric value, got %q", sample.Attributes[0].Raw)
	}
	if sample.ReallocatedSectors == nil || *sample.ReallocatedSectors != 42 {
		t.Fatalf("expected reallocated sectors 42, got %v", sample.ReallocatedSectors)
	}
}
