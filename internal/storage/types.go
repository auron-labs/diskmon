package storage

import (
	"strings"
	"time"
)

const (
	SmartTestStatusStarted    = "STARTED"
	SmartTestStatusInProgress = "IN_PROGRESS"
	SmartTestStatusPassed     = "PASSED"
	SmartTestStatusFailed     = "FAILED"
	SmartTestStatusSuccess    = "SUCCESS"
	SmartTestStatusCompleted  = "COMPLETED"
	SmartTestStatusUnknown    = "UNKNOWN"
	SmartTestStatusIncomplete = "INCOMPLETE"
)

func IsTerminalSmartTestStatus(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case SmartTestStatusFailed,
		SmartTestStatusPassed,
		SmartTestStatusSuccess,
		SmartTestStatusCompleted,
		SmartTestStatusUnknown,
		SmartTestStatusIncomplete:
		return true
	default:
		return false
	}
}

type DriveSummary struct {
	ID          int64      `json:"id"`
	Device      string     `json:"device"`
	Model       string     `json:"model"`
	Serial      string     `json:"serial"`
	Health      string     `json:"health"`
	Temperature *int       `json:"temperature"`
	PowerOnHrs  *int64     `json:"power_on_hours"`
	LastSeen    *time.Time `json:"last_seen"`
}

type DriveDetail struct {
	ID            int64      `json:"id"`
	Device        string     `json:"device"`
	Model         string     `json:"model"`
	Serial        string     `json:"serial"`
	WWN           string     `json:"wwn"`
	Health        string     `json:"health"`
	HealthScore   int        `json:"health_score"`
	HealthReasons string     `json:"health_reasons"`
	Temperature   *int       `json:"temperature"`
	PowerOnHours  *int64     `json:"power_on_hours"`
	Reallocated   *int64     `json:"reallocated_sectors"`
	Pending       *int64     `json:"pending_sectors"`
	Uncorrectable *int64     `json:"uncorrectable_sectors"`
	WearLevel     *int64     `json:"wear_level"`
	CollectedAt   *time.Time `json:"collected_at"`
	FirstSeen     *time.Time `json:"first_seen"`
	LastSeen      *time.Time `json:"last_seen"`
}

type HistoryPoint struct {
	CollectedAt          time.Time `json:"collected_at"`
	Temperature          *int      `json:"temperature"`
	PowerOnHours         *int64    `json:"power_on_hours"`
	ReallocatedSectors   *int64    `json:"reallocated_sectors"`
	PendingSectors       *int64    `json:"pending_sectors"`
	UncorrectableSectors *int64    `json:"uncorrectable_sectors"`
	WearLevel            *int64    `json:"wear_level"`
}

type AttributePoint struct {
	AttributeID int    `json:"attribute_id"`
	Name        string `json:"name"`
	Value       int    `json:"value"`
	Worst       int    `json:"worst"`
	Threshold   int    `json:"threshold"`
	Raw         string `json:"raw"`
	Status      string `json:"status"`
}

type SmartTestRun struct {
	ID          int64      `json:"id"`
	TestType    string     `json:"test_type"`
	ScheduledAt time.Time  `json:"scheduled_at"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	Status      string     `json:"status"`
	Message     string     `json:"message"`
}

type SmartTestRunPage struct {
	Items    []SmartTestRun `json:"items"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Total    int            `json:"total"`
}

type NotificationState struct {
	DriveID          int64     `json:"drive_id"`
	NotificationName string    `json:"notification_name"`
	State            string    `json:"state"`
	UpdatedAt        time.Time `json:"updated_at"`
}
