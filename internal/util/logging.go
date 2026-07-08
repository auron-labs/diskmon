package util

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

func ParseLogLevel(level string) (slog.Level, error) {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return slog.LevelDebug, nil
	case "INFO":
		return slog.LevelInfo, nil
	case "WARN":
		return slog.LevelWarn, nil
	case "ERROR":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported log level: %s", level)
	}
}

func NewLogger(level string) (*slog.Logger, *slog.LevelVar, error) {
	return newLogger(level, os.Stdout)
}

func newLogger(level string, w io.Writer) (*slog.Logger, *slog.LevelVar, error) {
	lv, err := ParseLogLevel(level)
	if err != nil {
		return nil, nil, err
	}

	levelVar := &slog.LevelVar{}
	levelVar.Set(lv)
	h := slog.NewTextHandler(w, &slog.HandlerOptions{Level: levelVar})
	return slog.New(h), levelVar, nil
}
