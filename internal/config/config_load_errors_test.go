package config_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikerls/motion-photo-extractor/internal/config"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func TestLoadReturnsErrorForMissingExplicitConfigPath(t *testing.T) {
	err := runLoadAndReturnError(t, "--config", filepath.Join(t.TempDir(), "missing.yaml"), "--input", "photo.jpg")
	if err == nil {
		t.Fatal("Load() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Fatalf("Load() error = %q, want to contain %q", err.Error(), "failed to read config file")
	}
}

func TestLoadReturnsErrorForInvalidConfigContents(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "go-motion-photo.yaml")
	if err := os.WriteFile(configPath, []byte("output: [unterminated"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	err := runLoadAndReturnErrorInDir(t, tempDir, "--input", "photo.jpg")
	if err == nil {
		t.Fatal("Load() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Fatalf("Load() error = %q, want to contain %q", err.Error(), "failed to read config file")
	}
}

func runLoadAndReturnError(t *testing.T, args ...string) error {
	t.Helper()
	return runLoadAndReturnErrorInDir(t, t.TempDir(), args...)
}

func runLoadAndReturnErrorInDir(t *testing.T, dir string, args ...string) error {
	t.Helper()

	originalArgs := os.Args
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Setenv("HOME", dir)

	pflag.CommandLine = pflag.NewFlagSet("test", pflag.ContinueOnError)
	pflag.CommandLine.SetOutput(io.Discard)
	viper.Reset()
	os.Args = append([]string{"go-motion-photo"}, args...)

	t.Cleanup(func() {
		os.Args = originalArgs
		pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.ExitOnError)
		pflag.CommandLine.SetOutput(io.Discard)
		viper.Reset()
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("restore chdir: %v", err)
		}
	})

	_, loadErr := config.Load()
	return loadErr
}
