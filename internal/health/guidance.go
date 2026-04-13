package health

import "strings"

// GuidanceForReasons maps raw health reasons to user-facing next steps.
func GuidanceForReasons(reasons []string) []string {
	guidance := make([]string, 0, len(reasons))
	seen := make(map[string]struct{}, len(reasons))

	for _, reason := range reasons {
		message := guidanceForReason(reason)
		if message == "" {
			continue
		}
		if _, ok := seen[message]; ok {
			continue
		}
		seen[message] = struct{}{}
		guidance = append(guidance, message)
	}

	return guidance
}

func GuidanceFromRawReasons(raw string) []string {
	return GuidanceForReasons(ParseReasonList(raw))
}

func ParseReasonList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	replacer := strings.NewReplacer("\r\n", "\n", "\r", "\n", ";", ",", "|", ",", "\n", ",")
	parts := strings.Split(replacer.Replace(raw), ",")
	reasons := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		part = strings.Trim(part, `"'`)
		if part == "" {
			continue
		}
		reasons = append(reasons, part)
	}

	if len(reasons) == 0 {
		return nil
	}

	return reasons
}

func guidanceForReason(reason string) string {
	switch {
	case reason == "SMART_OVERALL_FAILED":
		return "Back up the drive immediately and schedule replacement."
	case strings.HasPrefix(reason, "ATTR_FAILED:"):
		return "Review the failing SMART attribute and replace the drive if the failure remains current."
	case reason == "REALLOCATED_SECTORS_NONZERO":
		return "Back up important data and plan drive replacement; reallocated sectors usually indicate media damage."
	case reason == "PENDING_SECTORS_NONZERO":
		return "Back up data now and run an extended SMART self-test. Replace the drive if pending sectors persist."
	case reason == "UNCORRECTABLE_SECTORS_NONZERO":
		return "Back up data immediately and replace the drive; unreadable sectors indicate elevated data-loss risk."
	case reason == "TEMP_HIGH_CRITICAL":
		return "Reduce drive temperature immediately by checking airflow, fans, dust, and sustained workload."
	case reason == "NVME_CRITICAL_WARNING":
		return "Back up data and inspect the NVMe critical warning details. Replace the drive if the warning remains."
	case reason == "UDMA_CRC_ERRORS_NONZERO":
		return "Check and reseat the data or power connection, then monitor whether CRC errors continue increasing."
	case reason == "TEMP_HIGH_WARN":
		return "Improve cooling and monitor temperatures to keep the drive below the warning threshold."
	case reason == "WEAR_LEVEL_DEGRADED":
		return "Plan SSD replacement and verify backups; flash wear is approaching the warning threshold."
	case reason == "INSUFFICIENT_DATA":
		return "Collect another SMART sample to confirm health; the current sample is incomplete."
	default:
		return ""
	}
}
