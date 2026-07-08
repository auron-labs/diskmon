//go:build cgo

package storage

import (
	"strings"

	"diskmon/internal/smart"
)

func driveIdentityKey(info smart.DriveInfo) string {
	if wwn := normalizeIdentityPart(info.WWN); wwn != "" {
		return "wwn:" + wwn
	}

	serial := normalizeIdentityPart(info.Serial)
	model := normalizeIdentityPart(info.Model)
	if serial != "" && model != "" {
		return "serial-model:" + serial + "|" + model
	}

	if device := normalizeIdentityPart(info.Device); device != "" {
		return "device:" + device
	}

	return ""
}

func normalizeIdentityPart(value string) string {
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(strings.Join(parts, " "))
}
