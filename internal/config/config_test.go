package config_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikerls/motion-photo-extractor/internal/config"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func TestLoadAppliesDefaults(t *testing.T) {
	cfg := loadConfigForTest(t, "photo.jpg")

	if cfg.InputFile != "photo.jpg" {
		t.Fatalf("InputFile = %q, want %q", cfg.InputFile, "photo.jpg")
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
	if cfg.Log.Level != "info" {
		t.Fatalf("Log.Level = %q, want %q", cfg.Log.Level, "info")
	}
	if cfg.Log.NoConsole {
		t.Fatal("Log.NoConsole = true, want false")
	}
}

func TestLoadUsesPositionalInputWhenFlagIsMissing(t *testing.T) {
	cfg := loadConfigForTest(t, "movie.heic")

	if cfg.InputFile != "movie.heic" {
		t.Fatalf("InputFile = %q, want %q", cfg.InputFile, "movie.heic")
	}
}

func TestLoadPrefersInputFlagOverPositionalArg(t *testing.T) {
	cfg := loadConfigForTest(t, "--input", "from-flag.jpg", "from-positional.jpg")

	if cfg.InputFile != "from-flag.jpg" {
		t.Fatalf("InputFile = %q, want %q", cfg.InputFile, "from-flag.jpg")
	}
}

func TestLoadParsesKebabCaseCLIFlags(t *testing.T) {
	cfg := loadConfigForTest(t,
		"--input", "photo.jpg",
		"--output", "./out",
		"--delete-orig",
		"--rename-orig",
		"--extract-photo=false",
		"--log-file", "motion-photo.log",
		"--log-level", "debug",
		"--no-console-log",
		"--force",
	)

	if cfg.InputFile != "photo.jpg" {
		t.Fatalf("InputFile = %q, want %q", cfg.InputFile, "photo.jpg")
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
	configPath := filepath.Join(tempDir, "go-motion-photo.yaml")
	configContent := []byte("delete_orig: false\nrename_orig: false\nextract_photo: true\nlog:\n  level: info\n  no_console: false\n")
	if err := os.WriteFile(configPath, configContent, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := loadConfigForTest(t,
		"--config", configPath,
		"--delete-orig",
		"--rename-orig",
		"--extract-photo=false",
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
	if cfg.Log.Level != "debug" {
		t.Fatalf("Log.Level = %q, want %q", cfg.Log.Level, "debug")
	}
	if !cfg.Log.NoConsole {
		t.Fatal("Log.NoConsole = false, want true")
	}
}

func TestLoadReadsConfigFromCurrentDirectory(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "go-motion-photo.yaml")
	configContent := []byte("output: from-config\nlog:\n  level: warn\n")
	if err := os.WriteFile(configPath, configContent, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := loadConfigForTestInDir(t, tempDir, "--input", "photo.jpg")

	if cfg.OutputDir != "from-config" {
		t.Fatalf("OutputDir = %q, want %q", cfg.OutputDir, "from-config")
	}
	if cfg.Log.Level != "warn" {
		t.Fatalf("Log.Level = %q, want %q", cfg.Log.Level, "warn")
	}
}

func loadConfigForTest(t *testing.T, args ...string) *config.Config {
	t.Helper()

	tempDir := t.TempDir()
	return loadConfigForTestInDir(t, tempDir, args...)
}

func loadConfigForTestInDir(t *testing.T, dir string, args ...string) *config.Config {
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

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	return cfg
}
