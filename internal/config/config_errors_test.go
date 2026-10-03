package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikerls/motion-photo-extractor/internal/config"
)

func TestLoadReturnsErrorForMissingExplicitConfigPath(t *testing.T) {
	err := loadError(t, t.TempDir(), "--config", filepath.Join(t.TempDir(), "missing.yaml"), "--input", "photo.jpg")
	if err == nil {
		t.Fatal("Load() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Fatalf("Load() error = %q, want to contain %q", err.Error(), "failed to read config file")
	}
}

func TestLoadReturnsErrorForInvalidConfigContents(t *testing.T) {
	tempDir := t.TempDir()
	writeConfig(t, tempDir, "output: [unterminated")

	err := loadError(t, tempDir, "--input", "photo.jpg")
	if err == nil {
		t.Fatal("Load() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Fatalf("Load() error = %q, want to contain %q", err.Error(), "failed to read config file")
	}
}

func TestLoadReturnsErrorForUnknownFlag(t *testing.T) {
	if err := loadError(t, t.TempDir(), "--nope"); err == nil {
		t.Fatal("Load() error = nil, want non-nil")
	}
}

func loadError(t *testing.T, dir string, args ...string) error {
	t.Helper()

	t.Chdir(dir)
	t.Setenv("HOME", dir)

	_, err := config.Load(args)
	return err
}
