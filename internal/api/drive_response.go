package api

import (
	"encoding/json"

	"diskmon/internal/health"
)

func augmentDriveResponse(item any) any {
	payload, ok := asJSONMap(item)
	if !ok {
		return item
	}

	if existing, ok := payload["health_guidance"]; ok {
		switch v := existing.(type) {
		case []any:
			if len(v) > 0 {
				return payload
			}
		case []string:
			if len(v) > 0 {
				return payload
			}
		}
	}

	guidance := health.GuidanceForReasons(extractHealthReasons(payload["health_reasons"]))
	if len(guidance) > 0 {
		payload["health_guidance"] = guidance
	}

	return payload
}

func extractHealthReasons(value any) []string {
	switch v := value.(type) {
	case string:
		return health.ParseReasonList(v)
	case []string:
		return v
	case []any:
		reasons := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok || s == "" {
				continue
			}
			reasons = append(reasons, s)
		}
		return reasons
	default:
		return nil
	}
}

func asJSONMap(v any) (map[string]any, bool) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, false
	}

	return payload, true
}
