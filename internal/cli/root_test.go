package cli

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"diskmon/internal/config"

	"github.com/spf13/cobra"
)

func TestNewRootCmdLogLevelFlagEnablesDebugLoggingInPreRun(t *testing.T) {
	configPath := writeRootTestConfig(t)
	logger, levelVar, buf := newRootTestLogger(slog.LevelInfo)
	cfg := config.Default()

	ran := false
	cmd := NewRootCmd(cfg, logger, levelVar)
	cmd.AddCommand(&cobra.Command{
		Use: "probe",
		RunE: func(cmd *cobra.Command, args []string) error {
			ran = true
			return nil
		},
	})
	cmd.SetArgs([]string{"--config", configPath, "--log-level", "debug", "probe"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !ran {
		t.Fatal("expected probe command to run")
	}
	if got := levelVar.Level(); got != slog.LevelDebug {
		t.Fatalf("expected level var %v, got %v", slog.LevelDebug, got)
	}
	if !strings.Contains(buf.String(), "config loaded") {
		t.Fatalf("expected pre-run debug log, got %q", buf.String())
	}
}

func TestNewRootCmdLogLevelFlagRejectsUnsupportedLevel(t *testing.T) {
	configPath := writeRootTestConfig(t)
	logger, levelVar, buf := newRootTestLogger(slog.LevelInfo)
	cfg := config.Default()

	ran := false
	cmd := NewRootCmd(cfg, logger, levelVar)
	cmd.AddCommand(&cobra.Command{
		Use: "probe",
		RunE: func(cmd *cobra.Command, args []string) error {
			ran = true
			return nil
		},
	})
	cmd.SetArgs([]string{"--config", configPath, "--log-level", "nope", "probe"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected Execute to return error")
	}
	if got := err.Error(); got != "unsupported log level: nope" {
		t.Fatalf("unexpected error: %q", got)
	}
	if ran {
		t.Fatal("probe command should not run when pre-run returns error")
	}
	if strings.Contains(buf.String(), "config loaded") {
		t.Fatalf("did not expect pre-run debug log, got %q", buf.String())
	}
}

func TestRootWebListenFlagOverridesConfigDefault(t *testing.T) {
	configPath := writeRootTestConfig(t)
	logger, levelVar, _ := newRootTestLogger(slog.LevelInfo)
	cfg := config.Default()

	ran := false
	cmd := NewRootCmd(cfg, logger, levelVar)
	cmd.AddCommand(&cobra.Command{
		Use: "probe",
		RunE: func(cmd *cobra.Command, args []string) error {
			ran = true
			return nil
		},
	})
	cmd.SetArgs([]string{"--config", configPath, "--web-listen", "0.0.0.0:8976", "probe"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !ran {
		t.Fatal("expected probe command to run")
	}
	if cfg.WebListen != "0.0.0.0:8976" {
		t.Fatalf("expected flag override to win, got %q", cfg.WebListen)
	}
}

func newRootTestLogger(initialLevel slog.Level) (*slog.Logger, *slog.LevelVar, *bytes.Buffer) {
	var buf bytes.Buffer
	levelVar := &slog.LevelVar{}
	levelVar.Set(initialLevel)
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: levelVar}))
	return logger, levelVar, &buf
}

func writeRootTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "diskmon.yaml")
	content := []byte("database: diskmon-test.duckdb\ncollector:\n  interval: 1m\nweb:\n  listen: 127.0.0.1:0\nlog:\n  level: INFO\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return path
}
