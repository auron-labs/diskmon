package smart

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	runOut       map[string][]byte
	runErr       map[string]error
	selfTestOut  []byte
	selfTestErr  error
	selfTestLog  []byte
	selfTestLogE error
}

func (f *fakeRunner) Run(ctx context.Context, device string) ([]byte, error) {
	if err := f.runErr[device]; err != nil {
		return nil, err
	}
	return f.runOut[device], nil
}

func (f *fakeRunner) RunSelfTest(ctx context.Context, device string, testType string) ([]byte, error) {
	if f.selfTestErr != nil {
		return nil, f.selfTestErr
	}
	return f.selfTestOut, nil
}

func (f *fakeRunner) RunSelfTestLog(ctx context.Context, device string) ([]byte, error) {
	if f.selfTestLogE != nil {
		return nil, f.selfTestLogE
	}
	return f.selfTestLog, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestParseSelfTestResult(t *testing.T) {
	cases := []struct {
		name       string
		payload    string
		testType   string
		wantStatus string
		wantOK     bool
	}{
		{
			name: "in progress",
			payload: `{"ata_smart_self_test_log":{"standard":{"table":[
				{"type":{"string":"Short offline"},"status":{"string":"Self-test routine in progress"}}
			]}}}`,
			testType:   "short",
			wantStatus: "IN_PROGRESS",
			wantOK:     true,
		},
		{
			name: "passed",
			payload: `{"ata_smart_self_test_log":{"standard":{"table":[
				{"type":{"string":"Extended offline"},"status":{"string":"Completed without error"}}
			]}}}`,
			testType:   "long",
			wantStatus: "PASSED",
			wantOK:     true,
		},
		{
			name: "failed aborted",
			payload: `{"ata_smart_self_test_log":{"standard":{"table":[
				{"type":{"string":"Short offline"},"status":{"string":"Aborted by host"}}
			]}}}`,
			testType:   "short",
			wantStatus: "FAILED",
			wantOK:     true,
		},
		{
			name: "failed completed with error",
			payload: `{"ata_smart_self_test_log":{"standard":{"table":[
				{"type":{"string":"Short offline"},"status":{"string":"Completed: read failure"}}
			]}}}`,
			testType:   "short",
			wantStatus: "FAILED",
			wantOK:     true,
		},
		{
			name: "unknown",
			payload: `{"ata_smart_self_test_log":{"standard":{"table":[
				{"type":{"string":"Short offline"},"status":{"string":"vendor-specific"}}
			]}}}`,
			testType:   "short",
			wantStatus: "UNKNOWN",
			wantOK:     true,
		},
		{
			name:       "missing table",
			payload:    `{}`,
			testType:   "short",
			wantStatus: "",
			wantOK:     false,
		},
		{
			name: "no matching type",
			payload: `{"ata_smart_self_test_log":{"standard":{"table":[
				{"type":{"string":"Conveyance offline"},"status":{"string":"Completed without error"}}
			]}}}`,
			testType:   "short",
			wantStatus: "",
			wantOK:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, _, ok := parseSelfTestResult([]byte(tc.payload), tc.testType)
			if ok != tc.wantOK {
				t.Fatalf("ok mismatch: got %v want %v", ok, tc.wantOK)
			}
			if status != tc.wantStatus {
				t.Fatalf("status mismatch: got %q want %q", status, tc.wantStatus)
			}
		})
	}
}

func TestParseSelfTestWait(t *testing.T) {
	c := NewCollector(&fakeRunner{}, testLogger())
	if got := c.ParseSelfTestWait("Please wait 7 minutes for test to complete."); got != 7*time.Minute {
		t.Fatalf("expected 7m, got %v", got)
	}
	if got := c.ParseSelfTestWait("please wait -3 minutes"); got != 0 {
		t.Fatalf("expected 0 for invalid value, got %v", got)
	}
	if got := c.ParseSelfTestWait("no wait line"); got != 0 {
		t.Fatalf("expected 0 for missing value, got %v", got)
	}
}

func TestParseSelfTestResultWithPowerOnHours(t *testing.T) {
	payload := []byte(`{"ata_smart_self_test_log":{"standard":{"table":[
		{"type":{"string":"Short offline"},"status":{"string":"Completed without error"},"power_on_time":{"hours":100}}
	]}}}`)
	status, msg, poh, ok := parseSelfTestResultWithPowerOnHours(payload, "short")
	if !ok {
		t.Fatal("expected ok")
	}
	if status != "PASSED" {
		t.Fatalf("status=%q want PASSED", status)
	}
	if poh == nil || *poh != 100 {
		t.Fatalf("power_on_hours=%v want 100", poh)
	}
	if msg == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestParseSelfTestResultWithPowerOnHoursMissing(t *testing.T) {
	payload := []byte(`{"ata_smart_self_test_log":{"standard":{"table":[
		{"type":{"string":"Short offline"},"status":{"string":"Completed without error"}}
	]}}}`)
	_, _, poh, ok := parseSelfTestResultWithPowerOnHours(payload, "short")
	if !ok {
		t.Fatal("expected ok")
	}
	if poh != nil {
		t.Fatalf("power_on_hours=%v want nil", poh)
	}
}

func TestTestTypeMatches(t *testing.T) {
	if !testTypeMatches("short offline", "short") {
		t.Fatal("expected short match")
	}
	if !testTypeMatches("extended offline", "long") {
		t.Fatal("expected extended to match long")
	}
	if !testTypeMatches("long offline", "long") {
		t.Fatal("expected long match")
	}
	if testTypeMatches("conveyance", "short") {
		t.Fatal("expected non-short type to fail short match")
	}
	if testTypeMatches("short offline", "foo") {
		t.Fatal("expected unsupported type to fail")
	}
}

func TestSummarizeSelfTestStartOutput(t *testing.T) {
	raw := `
	=== START OF OFFLINE IMMEDIATE AND SELF-TEST SECTION ===
	Testing has begun.
	Please wait 2 minutes for test to complete.
	Test will complete after Fri Mar 6 10:42:00 2026
	`
	msg := summarizeSelfTestStartOutput(raw, "short")
	if !strings.Contains(msg, "SMART short self-test started.") {
		t.Fatalf("expected header in message, got %q", msg)
	}
	if !strings.Contains(strings.ToLower(msg), "testing has begun") {
		t.Fatalf("expected started line, got %q", msg)
	}
	if !strings.Contains(strings.ToLower(msg), "please wait 2 minutes") {
		t.Fatalf("expected wait line, got %q", msg)
	}
}

func TestCollectAllPartialFailure(t *testing.T) {
	runner := &fakeRunner{
		runOut: map[string][]byte{
			"/dev/sda": []byte(`{"temperature":{"current":35}}`),
		},
		runErr: map[string]error{
			"/dev/sdb": errors.New("smartctl failed"),
		},
	}
	c := NewCollector(runner, testLogger())
	results, err := c.CollectAll(context.Background(), []string{"/dev/sda", "/dev/sdb"})
	if err != nil {
		t.Fatalf("expected partial success, got err: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 successful result, got %d", len(results))
	}
	if results[0].Info.Device != "/dev/sda" {
		t.Fatalf("expected /dev/sda result, got %q", results[0].Info.Device)
	}
}

func TestCollectAllAllFailure(t *testing.T) {
	runner := &fakeRunner{
		runOut: map[string][]byte{},
		runErr: map[string]error{
			"/dev/sda": errors.New("failed"),
		},
	}
	c := NewCollector(runner, testLogger())
	_, err := c.CollectAll(context.Background(), []string{"/dev/sda"})
	if err == nil {
		t.Fatal("expected error when all devices fail")
	}
}

func TestRunSelfTestValidation(t *testing.T) {
	c := NewCollector(&fakeRunner{}, testLogger())
	if _, err := c.RunSelfTest(context.Background(), "/dev/sda", "bad"); err == nil {
		t.Fatal("expected invalid test type error")
	}
}

func TestRunSelfTestNormalizesType(t *testing.T) {
	runner := &fakeRunner{
		selfTestOut: []byte("Testing has begun.\nPlease wait 1 minutes.\n"),
	}
	c := NewCollector(runner, testLogger())
	msg, err := c.RunSelfTest(context.Background(), "/dev/sda", " Short ")
	if err != nil {
		t.Fatalf("run self test: %v", err)
	}
	if !strings.Contains(strings.ToLower(msg), "smart short self-test started") {
		t.Fatalf("expected normalized short output, got %q", msg)
	}
}
