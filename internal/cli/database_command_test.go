package cli

import (
	"strings"
	"testing"

	"diskmon/internal/config"
)

func TestDatabasePruneRejectsDryRunWithCompact(t *testing.T) {
	cmd := newDatabaseCmd(config.Default())
	cmd.SetArgs([]string{"prune", "--dry-run", "--compact"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--dry-run cannot be combined with --compact") {
		t.Fatalf("expected incompatible flags error, got %v", err)
	}
}

func TestDatabasePruneRejectsAllDisabledRetention(t *testing.T) {
	cfg := config.Default()
	cfg.RawRetention, cfg.HourlyRetention, cfg.DailyRetention = 0, 0, 0
	cmd := newDatabaseCmd(cfg)
	cmd.SetArgs([]string{"prune"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "all database retention tiers are disabled") {
		t.Fatalf("expected disabled retention error, got %v", err)
	}
}

func TestDatabasePruneAllowsIndividualDisabledTier(t *testing.T) {
	cfg := config.Default()
	cfg.RawRetention = 0
	if err := validatePruneRetention(cfg, false, false); err != nil {
		t.Fatalf("partial retention disable should be accepted: %v", err)
	}
}
