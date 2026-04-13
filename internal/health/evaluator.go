package health

import "diskmon/internal/smart"

type Status string

const (
	StatusGreen   Status = "GREEN"
	StatusYellow  Status = "YELLOW"
	StatusRed     Status = "RED"
	StatusUnknown Status = "UNKNOWN"
)

type Result struct {
	Status   Status
	Score    int
	Reasons  []string
	Guidance []string
}

type Evaluator struct {
	rules Rules
}

func NewEvaluator(rules Rules) *Evaluator {
	return &Evaluator{rules: rules}
}

// Evaluate applies a strict signals-based health assessment.
//
// RED signals (any one triggers RED):
//  1. smart_status.passed == false (FailingNow)
//  2. Any attribute with when_failed != "" (attribute has failed)
//  3. Attribute ID 197 (Current_Pending_Sector) raw > 0
//  4. Attribute ID 198 (Offline_Uncorrectable) raw > 0
//  5. Temperature >= TemperatureRedC
//  6. NVMe critical warning present
//
// YELLOW signals (any one triggers YELLOW if no RED):
//  7. Attribute ID 5 (Reallocated_Sector_Ct) raw > 0
//  8. Attribute ID 199 (UDMA_CRC_Error_Count) raw > 0
//  9. Temperature >= TemperatureWarnC
//
// 10. Wear level >= WearLevelWarnPct
//
// GREEN: no signals triggered and sufficient data present
// UNKNOWN: parsing errors or insufficient data
func (e *Evaluator) Evaluate(s smart.SmartSample) Result {
	redReasons := make([]string, 0, 4)
	yellowReasons := make([]string, 0, 4)

	// --- RED signals ---

	// 1. Overall SMART status gate
	if s.FailingNow {
		redReasons = append(redReasons, "SMART_OVERALL_FAILED")
	}

	// 2. Attribute when_failed check
	for _, attr := range s.Attributes {
		if attr.WhenFailed != "" {
			redReasons = append(redReasons, "ATTR_FAILED:"+attr.Name)
		}
	}

	// 3. Pending sectors (ID 197) raw > 0
	if s.PendingSectors != nil && *s.PendingSectors > 0 {
		redReasons = append(redReasons, "PENDING_SECTORS_NONZERO")
	}

	// 4. Uncorrectable sectors (ID 198 / NVMe media_errors) raw > 0
	if s.UncorrectableSectors != nil && *s.UncorrectableSectors > 0 {
		redReasons = append(redReasons, "UNCORRECTABLE_SECTORS_NONZERO")
	}

	// 5. Temperature critical
	if s.Temperature != nil && *s.Temperature >= e.rules.TemperatureRedC {
		redReasons = append(redReasons, "TEMP_HIGH_CRITICAL")
	}

	// 6. NVMe critical warning
	if s.CriticalWarning {
		redReasons = append(redReasons, "NVME_CRITICAL_WARNING")
	}

	if len(redReasons) > 0 {
		return Result{Status: StatusRed, Score: 20, Reasons: redReasons, Guidance: GuidanceForReasons(redReasons)}
	}

	// --- YELLOW signals ---

	// 7. Reallocated sectors (ID 5) raw > 0
	if s.ReallocatedSectors != nil && *s.ReallocatedSectors > 0 {
		yellowReasons = append(yellowReasons, "REALLOCATED_SECTORS_NONZERO")
	}

	// 8. UDMA CRC errors (ID 199) raw > 0
	if s.UDMACRCErrors != nil && *s.UDMACRCErrors > 0 {
		yellowReasons = append(yellowReasons, "UDMA_CRC_ERRORS_NONZERO")
	}

	// 9. Temperature warning
	if s.Temperature != nil && *s.Temperature >= e.rules.TemperatureWarnC {
		yellowReasons = append(yellowReasons, "TEMP_HIGH_WARN")
	}

	// 10. Wear level degraded
	if s.WearLevel != nil && *s.WearLevel >= e.rules.WearLevelWarnPct {
		yellowReasons = append(yellowReasons, "WEAR_LEVEL_DEGRADED")
	}

	if len(yellowReasons) > 0 {
		return Result{Status: StatusYellow, Score: 65, Reasons: yellowReasons, Guidance: GuidanceForReasons(yellowReasons)}
	}

	// --- UNKNOWN check ---
	if s.Temperature == nil && s.PowerOnHours == nil && len(s.Attributes) == 0 {
		return Result{Status: StatusUnknown, Score: 0, Reasons: []string{"INSUFFICIENT_DATA"}, Guidance: GuidanceForReasons([]string{"INSUFFICIENT_DATA"})}
	}

	return Result{Status: StatusGreen, Score: 95, Reasons: nil}
}
