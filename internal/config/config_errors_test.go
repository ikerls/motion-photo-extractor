package config_test

import (
	"fmt"
	"os"
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

func TestLoadReturnsErrorForInvalidJSONConfig(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "go-motion-photo.json"), []byte(`{"output": `), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	err := loadError(t, tempDir, "--input", "photo.jpg")
	if err == nil {
		t.Fatal("Load() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Fatalf("Load() error = %q, want to contain %q", err.Error(), "failed to read config file")
	}
}

func TestLoadRejectsExplicitConfigInUnsupportedFormat(t *testing.T) {
	for _, name := range []string{"custom.toml", "custom.conf", "custom"} {
		configPath := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(configPath, []byte("output: from-file\n"), 0644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		err := loadError(t, t.TempDir(), "--config", configPath, "photo.jpg")
		if err == nil {
			t.Fatalf("Load() with %s error = nil, want non-nil", name)
		}
		want := fmt.Sprintf("unsupported config format %q: use a .yaml, .yml or .json file", filepath.Ext(name))
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Load() with %s error = %q, want to contain %q", name, err.Error(), want)
		}
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

func TestLoadReturnsErrorForInvalidEnvironmentValue(t *testing.T) {
	t.Setenv("GO_MOTION_PHOTO_FORCE", "maybe")

	err := loadError(t, t.TempDir(), "photo.jpg")
	if err == nil {
		t.Fatal("Load() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "GO_MOTION_PHOTO_FORCE") {
		t.Fatalf("Load() error = %q, want to name the variable", err.Error())
	}
}
