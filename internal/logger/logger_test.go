package logger_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikerls/motion-photo-extractor/internal/logger"
)

func TestNewWritesToConsoleAndFile(t *testing.T) {
	var console bytes.Buffer
	file := filepath.Join(t.TempDir(), "logs", "run.log")

	log, closeLog, err := logger.New(logger.Options{Console: &console, File: file, Level: slog.LevelInfo})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	log.Debug("hidden")
	log.Info("Extracted", "file", "a.jpg")
	if err := closeLog(); err != nil {
		t.Fatalf("close error = %v", err)
	}

	written, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	for name, got := range map[string]string{"console": console.String(), "file": string(written)} {
		if !strings.Contains(got, "INFO  Extracted file=a.jpg") || strings.Contains(got, "hidden") {
			t.Errorf("%s = %q, want only the info line", name, got)
		}
		if strings.Contains(got, "\x1b") {
			t.Errorf("%s = %q, want no escape sequences", name, got)
		}
	}
}

func TestNewWritesJSON(t *testing.T) {
	var console bytes.Buffer

	log, _, err := logger.New(logger.Options{Console: &console, Level: slog.LevelDebug, JSON: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	log.Debug("Skipped", "file", "a.jpg")

	var line map[string]any
	if err := json.Unmarshal(console.Bytes(), &line); err != nil {
		t.Fatalf("output %q is not JSON: %v", &console, err)
	}
	if line["msg"] != "Skipped" || line["level"] != "debug" || line["file"] != "a.jpg" || line["time"] == nil {
		t.Fatalf("line = %v", line)
	}
}

func TestParseLevel(t *testing.T) {
	if level, err := logger.ParseLevel("warn"); err != nil || level != slog.LevelWarn {
		t.Fatalf("ParseLevel(warn) = %v, %v", level, err)
	}
	if _, err := logger.ParseLevel("loud"); err == nil {
		t.Fatal("ParseLevel(loud) error = nil, want non-nil")
	}
}
