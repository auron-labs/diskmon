package util

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLogLevelReturnsErrorForUnsupportedLevel(t *testing.T) {
	_, err := ParseLogLevel("trace")
	if err == nil {
		t.Fatal("expected error for unsupported log level")
	}
	if got := err.Error(); got != "unsupported log level: trace" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestNewLoggerLevelVarCanEnableDebugLogging(t *testing.T) {
	var buf bytes.Buffer
	logger, levelVar, err := newLogger("info", &buf)
	if err != nil {
		t.Fatalf("newLogger returned error: %v", err)
	}

	logger.Debug("before-toggle")
	if strings.Contains(buf.String(), "before-toggle") {
		t.Fatal("debug log should not be emitted at info level")
	}

	levelVar.Set(slog.LevelDebug)
	logger.Debug("after-toggle")
	if !strings.Contains(buf.String(), "after-toggle") {
		t.Fatal("debug log should be emitted after lowering level to debug")
	}
}
