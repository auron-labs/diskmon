package health

import (
	"reflect"
	"testing"
)

func TestGuidanceForReasons(t *testing.T) {
	reasons := []string{
		"PENDING_SECTORS_NONZERO",
		"UDMA_CRC_ERRORS_NONZERO",
		"PENDING_SECTORS_NONZERO",
		"ATTR_FAILED:Reallocated_Sector_Ct",
	}

	want := []string{
		"Back up data now and run an extended SMART self-test. Replace the drive if pending sectors persist.",
		"Check and reseat the data or power connection, then monitor whether CRC errors continue increasing.",
		"Review the failing SMART attribute and replace the drive if the failure remains current.",
	}

	if got := GuidanceForReasons(reasons); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected guidance:\nwant: %#v\n got: %#v", want, got)
	}
}

func TestParseReasonList(t *testing.T) {
	raw := `["PENDING_SECTORS_NONZERO", "TEMP_HIGH_WARN"]`
	want := []string{"PENDING_SECTORS_NONZERO", "TEMP_HIGH_WARN"}

	if got := ParseReasonList(raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected reasons:\nwant: %#v\n got: %#v", want, got)
	}
}
