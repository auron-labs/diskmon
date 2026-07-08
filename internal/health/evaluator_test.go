package health

import (
	"encoding/json"
	"os"
	"testing"

	"diskmon/internal/smart"
)

func TestHealthySeagateDrive(t *testing.T) {
	payload, err := os.ReadFile("testdata/seagate_healthy.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	_, sample, err := smart.ParseSmartJSON("/dev/sda", payload)
	if err != nil {
		t.Fatalf("parse smart json: %v", err)
	}

	ev := NewEvaluator(DefaultRules())
	result := ev.Evaluate(sample)

	if result.Status != StatusGreen {
		t.Errorf("expected GREEN, got %s (reasons: %v)", result.Status, result.Reasons)
	}
	if result.Score != 95 {
		t.Errorf("expected score 95, got %d", result.Score)
	}
	if len(result.Reasons) != 0 {
		t.Errorf("expected no reasons, got %v", result.Reasons)
	}

	// Verify parser extracted correct values
	if sample.ReallocatedSectors == nil || *sample.ReallocatedSectors != 0 {
		t.Errorf("expected reallocated sectors = 0, got %v", sample.ReallocatedSectors)
	}
	if sample.PendingSectors == nil || *sample.PendingSectors != 0 {
		t.Errorf("expected pending sectors = 0, got %v", sample.PendingSectors)
	}
	if sample.UncorrectableSectors == nil || *sample.UncorrectableSectors != 0 {
		t.Errorf("expected uncorrectable sectors = 0, got %v", sample.UncorrectableSectors)
	}
	if sample.Temperature == nil || *sample.Temperature != 38 {
		t.Errorf("expected temperature = 38, got %v", sample.Temperature)
	}
}

func TestSMARTOverallFailed(t *testing.T) {
	sample := smart.SmartSample{
		FailingNow:  true,
		Temperature: intPtr(40),
	}
	ev := NewEvaluator(DefaultRules())
	result := ev.Evaluate(sample)

	if result.Status != StatusRed {
		t.Errorf("expected RED, got %s", result.Status)
	}
	assertContains(t, result.Reasons, "SMART_OVERALL_FAILED")
}

func TestReallocatedSectorsNonzero(t *testing.T) {
	sample := smart.SmartSample{
		ReallocatedSectors: int64Ptr(1),
		Temperature:        intPtr(35),
	}
	ev := NewEvaluator(DefaultRules())
	result := ev.Evaluate(sample)

	if result.Status != StatusRed {
		t.Errorf("expected RED, got %s", result.Status)
	}
	assertContains(t, result.Reasons, "REALLOCATED_SECTORS_NONZERO")
}

func TestPendingSectorsNonzero(t *testing.T) {
	sample := smart.SmartSample{
		PendingSectors: int64Ptr(3),
		Temperature:    intPtr(35),
	}
	ev := NewEvaluator(DefaultRules())
	result := ev.Evaluate(sample)

	if result.Status != StatusRed {
		t.Errorf("expected RED, got %s", result.Status)
	}
	assertContains(t, result.Reasons, "PENDING_SECTORS_NONZERO")
}

func TestUncorrectableSectorsNonzero(t *testing.T) {
	sample := smart.SmartSample{
		UncorrectableSectors: int64Ptr(1),
		Temperature:          intPtr(35),
	}
	ev := NewEvaluator(DefaultRules())
	result := ev.Evaluate(sample)

	if result.Status != StatusRed {
		t.Errorf("expected RED, got %s", result.Status)
	}
	assertContains(t, result.Reasons, "UNCORRECTABLE_SECTORS_NONZERO")
}

func TestAttrWhenFailed(t *testing.T) {
	sample := smart.SmartSample{
		Temperature: intPtr(35),
		Attributes: []smart.SmartAttribute{
			{AttributeID: 5, Name: "Reallocated_Sector_Ct", WhenFailed: "now"},
		},
	}
	ev := NewEvaluator(DefaultRules())
	result := ev.Evaluate(sample)

	if result.Status != StatusRed {
		t.Errorf("expected RED, got %s", result.Status)
	}
	assertContains(t, result.Reasons, "ATTR_FAILED:Reallocated_Sector_Ct")
}

func TestTempCritical(t *testing.T) {
	sample := smart.SmartSample{
		Temperature: intPtr(55),
	}
	ev := NewEvaluator(DefaultRules())
	result := ev.Evaluate(sample)

	if result.Status != StatusRed {
		t.Errorf("expected RED for temp >= 55, got %s", result.Status)
	}
	assertContains(t, result.Reasons, "TEMP_HIGH_CRITICAL")
}

func TestTempWarning(t *testing.T) {
	sample := smart.SmartSample{
		Temperature: intPtr(50),
	}
	ev := NewEvaluator(DefaultRules())
	result := ev.Evaluate(sample)

	if result.Status != StatusYellow {
		t.Errorf("expected YELLOW for temp >= 50, got %s", result.Status)
	}
	assertContains(t, result.Reasons, "TEMP_HIGH_WARN")
}

func TestUDMACRCErrors(t *testing.T) {
	sample := smart.SmartSample{
		UDMACRCErrors: int64Ptr(5),
		Temperature:   intPtr(35),
	}
	ev := NewEvaluator(DefaultRules())
	result := ev.Evaluate(sample)

	if result.Status != StatusYellow {
		t.Errorf("expected YELLOW for UDMA CRC errors, got %s", result.Status)
	}
	assertContains(t, result.Reasons, "UDMA_CRC_ERRORS_NONZERO")
}

func TestWearLevelDegraded(t *testing.T) {
	sample := smart.SmartSample{
		WearLevel:   int64Ptr(80),
		Temperature: intPtr(35),
	}
	ev := NewEvaluator(DefaultRules())
	result := ev.Evaluate(sample)

	if result.Status != StatusYellow {
		t.Errorf("expected YELLOW for wear level >= 80, got %s", result.Status)
	}
	assertContains(t, result.Reasons, "WEAR_LEVEL_DEGRADED")
}

func TestHealthyDrive(t *testing.T) {
	sample := smart.SmartSample{
		Temperature:          intPtr(35),
		PowerOnHours:         int64Ptr(5000),
		ReallocatedSectors:   int64Ptr(0),
		PendingSectors:       int64Ptr(0),
		UncorrectableSectors: int64Ptr(0),
		UDMACRCErrors:        int64Ptr(0),
	}
	ev := NewEvaluator(DefaultRules())
	result := ev.Evaluate(sample)

	if result.Status != StatusGreen {
		t.Errorf("expected GREEN, got %s (reasons: %v)", result.Status, result.Reasons)
	}
	if result.Score != 95 {
		t.Errorf("expected score 95, got %d", result.Score)
	}
}

func TestInsufficientData(t *testing.T) {
	sample := smart.SmartSample{}
	ev := NewEvaluator(DefaultRules())
	result := ev.Evaluate(sample)

	if result.Status != StatusUnknown {
		t.Errorf("expected UNKNOWN, got %s", result.Status)
	}
}

func TestLargeRawReadErrorRateIgnored(t *testing.T) {
	// Seagate drives have huge Raw_Read_Error_Rate (ID 1) and Seek_Error_Rate (ID 7)
	// values that are vendor-encoded. These must NOT trigger any health downgrade.
	sample := smart.SmartSample{
		Temperature:          intPtr(38),
		PowerOnHours:         int64Ptr(5000),
		ReallocatedSectors:   int64Ptr(0),
		PendingSectors:       int64Ptr(0),
		UncorrectableSectors: int64Ptr(0),
		UDMACRCErrors:        int64Ptr(0),
		Attributes: []smart.SmartAttribute{
			{AttributeID: 1, Name: "Raw_Read_Error_Rate", Value: 73, Worst: 63, Threshold: 44, RawValue: 39252824, Raw: "39252824"},
			{AttributeID: 7, Name: "Seek_Error_Rate", Value: 82, Worst: 60, Threshold: 45, RawValue: 184910997, Raw: "184910997"},
		},
	}
	ev := NewEvaluator(DefaultRules())
	result := ev.Evaluate(sample)

	if result.Status != StatusGreen {
		t.Errorf("expected GREEN (large vendor raw values should be ignored), got %s (reasons: %v)", result.Status, result.Reasons)
	}
}

func TestNVMECriticalWarning(t *testing.T) {
	sample := smart.SmartSample{
		CriticalWarning: true,
		Temperature:     intPtr(35),
	}
	ev := NewEvaluator(DefaultRules())
	result := ev.Evaluate(sample)

	if result.Status != StatusRed {
		t.Errorf("expected RED, got %s", result.Status)
	}
	assertContains(t, result.Reasons, "NVME_CRITICAL_WARNING")
}

// TestParserSeagateAttributeIndexBug verifies the fix for the bug where
// the parser used array index (e.g., table[5]) instead of attribute ID
// lookup, causing incorrect values to be read for health-critical fields.
func TestParserSeagateAttributeIndexBug(t *testing.T) {
	// Minimal smartctl JSON structure where attribute at array index 5
	// has a large raw value (like Seek_Error_Rate on Seagate), but
	// the actual ID 5 (Reallocated_Sector_Ct) at array index 3 has raw=0.
	fixture := map[string]any{
		"smart_status": map[string]any{"passed": true},
		"temperature":  map[string]any{"current": 38},
		"ata_smart_attributes": map[string]any{
			"table": []any{
				map[string]any{"id": 1, "name": "Raw_Read_Error_Rate", "value": 73, "worst": 63, "thresh": 44, "raw": map[string]any{"value": 39252824, "string": "39252824"}},
				map[string]any{"id": 3, "name": "Spin_Up_Time", "value": 96, "worst": 96, "thresh": 0, "raw": map[string]any{"value": 0, "string": "0"}},
				map[string]any{"id": 4, "name": "Start_Stop_Count", "value": 100, "worst": 100, "thresh": 0, "raw": map[string]any{"value": 6, "string": "6"}},
				map[string]any{"id": 5, "name": "Reallocated_Sector_Ct", "value": 100, "worst": 100, "thresh": 36, "raw": map[string]any{"value": 0, "string": "0"}},
				map[string]any{"id": 9, "name": "Power_On_Hours", "value": 79, "worst": 79, "thresh": 0, "raw": map[string]any{"value": 5234, "string": "5234"}},
				map[string]any{"id": 7, "name": "Seek_Error_Rate", "value": 82, "worst": 60, "thresh": 45, "raw": map[string]any{"value": 184910997, "string": "184910997"}},
				map[string]any{"id": 197, "name": "Current_Pending_Sector", "value": 100, "worst": 100, "thresh": 0, "raw": map[string]any{"value": 0, "string": "0"}},
				map[string]any{"id": 198, "name": "Offline_Uncorrectable", "value": 100, "worst": 100, "thresh": 0, "raw": map[string]any{"value": 0, "string": "0"}},
			},
		},
	}
	payload, _ := json.Marshal(fixture)

	_, sample, err := smart.ParseSmartJSON("/dev/sda", payload)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// The old buggy code would have read table[5] (Seek_Error_Rate raw=184910997)
	// as reallocated sectors, causing RED. The fix uses ID-based lookup.
	if sample.ReallocatedSectors == nil {
		t.Fatal("expected reallocated sectors to be parsed")
	}
	if *sample.ReallocatedSectors != 0 {
		t.Errorf("expected reallocated sectors = 0 (from ID 5), got %d (likely read wrong array index)", *sample.ReallocatedSectors)
	}

	ev := NewEvaluator(DefaultRules())
	result := ev.Evaluate(sample)
	if result.Status != StatusGreen {
		t.Errorf("expected GREEN, got %s (reasons: %v)", result.Status, result.Reasons)
	}
}

func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }

func assertContains(t *testing.T, reasons []string, expected string) {
	t.Helper()
	for _, r := range reasons {
		if r == expected {
			return
		}
	}
	t.Errorf("expected reasons to contain %q, got %v", expected, reasons)
}
