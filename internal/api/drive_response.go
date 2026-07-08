package api

import (
	"diskmon/internal/health"
	"diskmon/internal/storage"
)

type driveResponse struct {
	*storage.DriveDetail
	HealthGuidance []string `json:"health_guidance,omitempty"`
}

func augmentDriveResponse(item *storage.DriveDetail) driveResponse {
	return driveResponse{
		DriveDetail:    item,
		HealthGuidance: health.GuidanceForReasons(health.ParseReasonList(item.HealthReasons)),
	}
}
