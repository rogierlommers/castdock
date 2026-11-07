package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewLoggerWritesHumanReadableText(t *testing.T) {
	var output bytes.Buffer
	logger := newLogger(&output, slog.LevelDebug)
	logger.Info("CastDock listening", "address", "127.0.0.1:8080")

	logLine := output.String()
	for _, expected := range []string{`level=INFO`, `msg="CastDock listening"`, `address=127.0.0.1:8080`} {
		if !strings.Contains(logLine, expected) {
			t.Fatalf("log output missing %q: %s", expected, logLine)
		}
	}
	if strings.HasPrefix(strings.TrimSpace(logLine), "{") {
		t.Fatalf("expected text output, got JSON: %s", logLine)
	}
}
