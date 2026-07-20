package smart

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type Runner interface {
	Run(ctx context.Context, device string) ([]byte, error)
	RunSelfTest(ctx context.Context, device string, testType string) ([]byte, error)
	RunSelfTestLog(ctx context.Context, device string) ([]byte, error)
}

type ExecRunner struct{}

var (
	selfTestWaitRe     = regexp.MustCompile(`(?i)\bplease wait\s+(\d+)\b`)
	selfTestStartedRe  = regexp.MustCompile(`(?im)^[^\n]*testing has begun[^\n]*$`)
	selfTestWaitLineRe = regexp.MustCompile(`(?im)^[^\n]*please wait[^\n]*$`)
	selfTestDoneLineRe = regexp.MustCompile(`(?im)^[^\n]*test will complete after[^\n]*$`)
)

func NewExecRunner() *ExecRunner {
	return &ExecRunner{}
}

func (r *ExecRunner) Run(ctx context.Context, device string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "smartctl", "-a", "-j", device)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("smartctl failed for %s: %w (%s)", device, err, string(out))
	}
	return out, nil
}

func (r *ExecRunner) RunSelfTest(ctx context.Context, device string, testType string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "smartctl", "-t", testType, device)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("smartctl %s test failed for %s: %w (%s)", testType, device, err, string(out))
	}
	return out, nil
}

func (r *ExecRunner) RunSelfTestLog(ctx context.Context, device string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "smartctl", "-l", "selftest", "-j", device)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("smartctl selftest log failed for %s: %w (%s)", device, err, string(out))
	}
	return out, nil
}

func (r *ExecRunner) Discover(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "smartctl", "--scan-open")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("smartctl scan failed: %w (%s)", err, string(out))
	}

	seen := map[string]struct{}{}
	devices := make([]string, 0)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		device := fields[0]
		if !strings.HasPrefix(device, "/dev/") {
			continue
		}
		if _, ok := seen[device]; ok {
			continue
		}
		seen[device] = struct{}{}
		devices = append(devices, device)
	}

	if len(devices) == 0 {
		return nil, fmt.Errorf("smartctl scan returned no devices")
	}
	return devices, nil
}

type Collector struct {
	runner Runner
	log    *slog.Logger
}

func NewCollector(runner Runner, logger *slog.Logger) *Collector {
	return &Collector{runner: runner, log: logger}
}

func (c *Collector) CollectAll(ctx context.Context, devices []string) ([]CollectResult, error) {
	results := make([]CollectResult, 0, len(devices))
	for _, d := range devices {
		res, err := c.CollectOne(ctx, d)
		if err != nil {
			c.log.Warn("smart collection failed", "device", d, "error", err)
			continue
		}
		results = append(results, res)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no successful collection results")
	}
	return results, nil
}

func (c *Collector) CollectOne(ctx context.Context, device string) (CollectResult, error) {
	raw, err := c.runner.Run(ctx, device)
	if err != nil {
		return CollectResult{}, err
	}
	info, sample, err := ParseSmartJSON(device, raw)
	if err != nil {
		return CollectResult{}, err
	}
	if info.Device == "" {
		info.Device = device
	}
	return CollectResult{Info: info, Sample: sample}, nil
}

func (c *Collector) RunSelfTest(ctx context.Context, device string, testType string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(testType))
	if normalized != "short" && normalized != "long" {
		return "", fmt.Errorf("unsupported SMART test type %q", testType)
	}

	out, err := c.runner.RunSelfTest(ctx, device, normalized)
	if err != nil {
		return "", err
	}
	return summarizeSelfTestStartOutput(string(out), normalized), nil
}

func (c *Collector) ParseSelfTestWait(output string) time.Duration {
	match := selfTestWaitRe.FindStringSubmatch(output)
	if len(match) != 2 {
		return 0
	}
	n, err := strconv.Atoi(match[1])
	if err != nil || n < 0 {
		return 0
	}
	return time.Duration(n) * time.Minute
}

func (c *Collector) ReadSelfTestResult(ctx context.Context, device string, testType string) (string, string) {
	raw, err := c.runner.RunSelfTestLog(ctx, device)
	if err != nil {
		return "UNKNOWN", err.Error()
	}
	status, msg, ok := parseSelfTestResult(raw, testType)
	if !ok {
		return "UNKNOWN", strings.TrimSpace(string(raw))
	}
	return status, msg
}

// ReadSelfTestResultWithPowerOnHours returns the latest matching self-test log
// entry along with its power_on_time.hours value (when present). The
// power_on_hours value lets callers distinguish a freshly-completed test from
// a stale entry left over from a previous run.
func (c *Collector) ReadSelfTestResultWithPowerOnHours(ctx context.Context, device string, testType string) (string, string, *int64) {
	raw, err := c.runner.RunSelfTestLog(ctx, device)
	if err != nil {
		return "UNKNOWN", err.Error(), nil
	}
	status, msg, poh, ok := parseSelfTestResultWithPowerOnHours(raw, testType)
	if !ok {
		return "UNKNOWN", strings.TrimSpace(string(raw)), nil
	}
	return status, msg, poh
}

func parseSelfTestResult(payload []byte, testType string) (string, string, bool) {
	status, msg, _, ok := parseSelfTestResultWithPowerOnHours(payload, testType)
	return status, msg, ok
}

func parseSelfTestResultWithPowerOnHours(payload []byte, testType string) (string, string, *int64, bool) {
	table := gjson.GetBytes(payload, "ata_smart_self_test_log.standard.table")
	if !table.Exists() || !table.IsArray() {
		return "", "", nil, false
	}
	foundMatch := false
	fallbackState := ""
	fallbackMessage := ""
	var fallbackPowerOnHours *int64
	for _, row := range table.Array() {
		typeStr := strings.ToLower(strings.TrimSpace(row.Get("type.string").String()))
		if !testTypeMatches(typeStr, testType) {
			continue
		}
		foundMatch = true
		statusStr := strings.TrimSpace(row.Get("status.string").String())
		if statusStr == "" {
			statusStr = "unknown"
		}
		poh := powerOnHoursFromRow(row)
		lower := strings.ToLower(statusStr)
		switch {
		case strings.Contains(lower, "in progress"):
			return "IN_PROGRESS", statusStr, poh, true
		case strings.Contains(lower, "without error"):
			if fallbackState == "" {
				fallbackState = "PASSED"
				fallbackMessage = statusStr
				fallbackPowerOnHours = poh
			}
		case strings.Contains(lower, "aborted"):
			if fallbackState == "" {
				fallbackState = "FAILED"
				fallbackMessage = statusStr
				fallbackPowerOnHours = poh
			}
		case strings.Contains(lower, "completed"):
			if fallbackState == "" {
				fallbackState = "FAILED"
				fallbackMessage = statusStr
				fallbackPowerOnHours = poh
			}
		default:
			if fallbackState == "" {
				fallbackState = "UNKNOWN"
				fallbackMessage = statusStr
				fallbackPowerOnHours = poh
			}
		}
	}
	if fallbackState != "" {
		return fallbackState, fallbackMessage, fallbackPowerOnHours, true
	}
	if foundMatch {
		return "UNKNOWN", "unknown", nil, true
	}
	return "", "", nil, false
}

func powerOnHoursFromRow(row gjson.Result) *int64 {
	poh := row.Get("power_on_time.hours")
	if !poh.Exists() || poh.Type != gjson.Number {
		return nil
	}
	v := poh.Int()
	return &v
}

func testTypeMatches(typeStr string, wanted string) bool {
	typeStr = strings.ToLower(typeStr)
	wanted = strings.ToLower(strings.TrimSpace(wanted))
	switch wanted {
	case "short":
		return strings.Contains(typeStr, "short")
	case "long":
		return strings.Contains(typeStr, "extended") || strings.Contains(typeStr, "long")
	default:
		return false
	}
}

func summarizeSelfTestStartOutput(raw string, testType string) string {
	startedLine := strings.TrimSpace(selfTestStartedRe.FindString(raw))
	waitLine := strings.TrimSpace(selfTestWaitLineRe.FindString(raw))
	completeLine := strings.TrimSpace(selfTestDoneLineRe.FindString(raw))

	parts := []string{fmt.Sprintf("SMART %s self-test started.", testType)}
	if startedLine != "" {
		parts = append(parts, startedLine)
	}
	if waitLine != "" {
		parts = append(parts, waitLine)
	}
	if completeLine != "" {
		parts = append(parts, completeLine)
	}

	msg := strings.Join(parts, " ")
	msg = strings.Join(strings.Fields(msg), " ")
	return strings.TrimSpace(msg)
}

func DiscoverDevices(ctx context.Context) ([]string, error) {
	return NewExecRunner().Discover(ctx)
}
