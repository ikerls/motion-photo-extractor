package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ikerls/motion-photo-extractor/internal/config"
)

func TestLoadAppliesDefaults(t *testing.T) {
	cfg := loadConfigForTest(t, "photo.jpg")

	if !slices.Equal(cfg.Inputs, []string{"photo.jpg"}) {
		t.Fatalf("Inputs = %q, want %q", cfg.Inputs, []string{"photo.jpg"})
	}
	if cfg.OutputDir != "." {
		t.Fatalf("OutputDir = %q, want %q", cfg.OutputDir, ".")
	}
	if cfg.DeleteOrig {
		t.Fatal("DeleteOrig = true, want false")
	}
	if cfg.RenameOrig {
		t.Fatal("RenameOrig = true, want false")
	}
	if !cfg.ExtractPhoto {
		t.Fatal("ExtractPhoto = false, want true")
	}
	if !cfg.ExtractVideo {
		t.Fatal("ExtractVideo = false, want true")
	}
	if cfg.Log.Level != "info" {
		t.Fatalf("Log.Level = %q, want %q", cfg.Log.Level, "info")
	}
	if cfg.Log.NoConsole {
		t.Fatal("Log.NoConsole = true, want false")
	}
}

func TestLoadUsesAllPositionalInputsWhenFlagIsMissing(t *testing.T) {
	cfg := loadConfigForTest(t, "movie.heic", "other.jpg")

	want := []string{"movie.heic", "other.jpg"}
	if !slices.Equal(cfg.Inputs, want) {
		t.Fatalf("Inputs = %q, want %q", cfg.Inputs, want)
	}
}

func TestLoadPrefersInputFlagOverPositionalArg(t *testing.T) {
	cfg := loadConfigForTest(t, "--input", "from-flag.jpg", "from-positional.jpg")

	want := []string{"from-flag.jpg"}
	if !slices.Equal(cfg.Inputs, want) {
		t.Fatalf("Inputs = %q, want %q", cfg.Inputs, want)
	}
}

func TestLoadPrefersPositionalArgOverConfigFileInput(t *testing.T) {
	tempDir := t.TempDir()
	writeConfig(t, tempDir, "input: from-config.jpg\n")

	cfg := loadConfigForTestInDir(t, tempDir, "from-positional.jpg")
	if want := []string{"from-positional.jpg"}; !slices.Equal(cfg.Inputs, want) {
		t.Fatalf("Inputs = %q, want %q", cfg.Inputs, want)
	}

	cfg = loadConfigForTestInDir(t, tempDir)
	if want := []string{"from-config.jpg"}; !slices.Equal(cfg.Inputs, want) {
		t.Fatalf("Inputs = %q, want %q", cfg.Inputs, want)
	}
}

func TestLoadParsesKebabCaseCLIFlags(t *testing.T) {
	cfg := loadConfigForTest(t,
		"--input", "photo.jpg",
		"--output", "./out",
		"--delete-orig",
		"--rename-orig",
		"--extract-photo=false",
		"--extract-video=false",
		"--log-file", "motion-photo.log",
		"--log-level", "debug",
		"--no-console-log",
		"--force",
	)

	if !slices.Equal(cfg.Inputs, []string{"photo.jpg"}) {
		t.Fatalf("Inputs = %q, want %q", cfg.Inputs, []string{"photo.jpg"})
	}
	if cfg.OutputDir != "./out" {
		t.Fatalf("OutputDir = %q, want %q", cfg.OutputDir, "./out")
	}
	if !cfg.DeleteOrig {
		t.Fatal("DeleteOrig = false, want true")
	}
	if !cfg.RenameOrig {
		t.Fatal("RenameOrig = false, want true")
	}
	if cfg.ExtractPhoto {
		t.Fatal("ExtractPhoto = true, want false")
	}
	if cfg.ExtractVideo {
		t.Fatal("ExtractVideo = true, want false")
	}
	if !cfg.Force {
		t.Fatal("Force = false, want true")
	}
	if cfg.Log.File != "motion-photo.log" {
		t.Fatalf("Log.File = %q, want %q", cfg.Log.File, "motion-photo.log")
	}
	if cfg.Log.Level != "debug" {
		t.Fatalf("Log.Level = %q, want %q", cfg.Log.Level, "debug")
	}
	if !cfg.Log.NoConsole {
		t.Fatal("Log.NoConsole = false, want true")
	}
}

func TestLoadAllowsKebabCaseCLIToOverrideConfigFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "custom.yaml")
	configContent := []byte("delete_orig: false\nrename_orig: false\nextract_photo: true\nextract_video: true\nlog:\n  level: info\n  no_console: false\n")
	if err := os.WriteFile(configPath, configContent, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := loadConfigForTest(t,
		"--config", configPath,
		"--delete-orig",
		"--rename-orig",
		"--extract-photo=false",
		"--extract-video=false",
		"--log-level", "debug",
		"--no-console-log",
	)

	if !cfg.DeleteOrig {
		t.Fatal("DeleteOrig = false, want true")
	}
	if !cfg.RenameOrig {
		t.Fatal("RenameOrig = false, want true")
	}
	if cfg.ExtractPhoto {
		t.Fatal("ExtractPhoto = true, want false")
	}
	if cfg.ExtractVideo {
		t.Fatal("ExtractVideo = true, want false")
	}
	if cfg.Log.Level != "debug" {
		t.Fatalf("Log.Level = %q, want %q", cfg.Log.Level, "debug")
	}
	if !cfg.Log.NoConsole {
		t.Fatal("Log.NoConsole = false, want true")
	}
}

func TestLoadReadsConfigFromCurrentDirectory(t *testing.T) {
	tempDir := t.TempDir()
	writeConfig(t, tempDir, "output: from-config\nextract_video: false\nforce: true\nlog:\n  level: warn\n")

	cfg := loadConfigForTestInDir(t, tempDir, "--input", "photo.jpg")

	if cfg.OutputDir != "from-config" {
		t.Fatalf("OutputDir = %q, want %q", cfg.OutputDir, "from-config")
	}
	if cfg.ExtractVideo {
		t.Fatal("ExtractVideo = true, want false")
	}
	if !cfg.Force {
		t.Fatal("Force = false, want true")
	}
	if cfg.Log.Level != "warn" {
		t.Fatalf("Log.Level = %q, want %q", cfg.Log.Level, "warn")
	}
}

func TestLoadReadsEnvironmentBetweenConfigFileAndFlags(t *testing.T) {
	tempDir := t.TempDir()
	writeConfig(t, tempDir, "output: from-config\nlog:\n  level: warn\n")
	t.Setenv("GO_MOTION_PHOTO_OUTPUT", "from-env")
	t.Setenv("GO_MOTION_PHOTO_LOG_LEVEL", "error")
	t.Setenv("GO_MOTION_PHOTO_DELETE_ORIG", "true")

	cfg := loadConfigForTestInDir(t, tempDir, "--log-level", "debug", "photo.jpg")

	if cfg.OutputDir != "from-env" {
		t.Fatalf("OutputDir = %q, want %q", cfg.OutputDir, "from-env")
	}
	if !cfg.DeleteOrig {
		t.Fatal("DeleteOrig = false, want true")
	}
	if cfg.Log.Level != "debug" {
		t.Fatalf("Log.Level = %q, want %q", cfg.Log.Level, "debug")
	}
}

func TestLoadReportsHelpAndVersion(t *testing.T) {
	t.Chdir(t.TempDir())

	if _, err := config.Load([]string{"--help"}); !errors.Is(err, config.ErrHelp) {
		t.Fatalf("Load(--help) error = %v, want ErrHelp", err)
	}

	cfg := loadConfigForTest(t, "--version")
	if !cfg.ShowVersion {
		t.Fatal("ShowVersion = false, want true")
	}
}

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	path := filepath.Join(dir, "go-motion-photo.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func loadConfigForTest(t *testing.T, args ...string) *config.Config {
	t.Helper()
	return loadConfigForTestInDir(t, t.TempDir(), args...)
}

// loadConfigForTestInDir runs Load with dir as both working directory and
// home, the two places a config file is searched for.
func loadConfigForTestInDir(t *testing.T, dir string, args ...string) *config.Config {
	t.Helper()

	t.Chdir(dir)
	t.Setenv("HOME", dir)

	cfg, err := config.Load(args)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	return cfg
}
