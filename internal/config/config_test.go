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

func TestLoadCombinesInputFlagAndPositionalArgs(t *testing.T) {
	cfg := loadConfigForTest(t, "first.jpg", "--input", "from-flag.jpg", "second.jpg")

	want := []string{"from-flag.jpg", "first.jpg", "second.jpg"}
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

func TestLoadReadsJSONConfig(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "compact", content: `{"output":"\/photos\/out","force":true,"log":{"level":"warn"}}`},
		{name: "indented with tabs", content: "{\n\t\"output\": \"/photos/out\",\n\t\"force\": true,\n\t\"log\": {\n\t\t\"level\": \"warn\"\n\t}\n}\n"},
		{name: "byte order mark", content: "\xef\xbb\xbf" + `{"output":"/photos/out","force":true,"log":{"level":"warn"}}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(tempDir, "go-motion-photo.json"), []byte(tc.content), 0644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			cfg := loadConfigForTestInDir(t, tempDir, "photo.jpg")

			if cfg.ConfigFile != "go-motion-photo.json" {
				t.Fatalf("ConfigFile = %q, want %q", cfg.ConfigFile, "go-motion-photo.json")
			}
			if cfg.OutputDir != "/photos/out" || !cfg.Force || cfg.Log.Level != "warn" {
				t.Fatalf("OutputDir = %q, Force = %v, Log.Level = %q, want /photos/out, true and warn", cfg.OutputDir, cfg.Force, cfg.Log.Level)
			}
			// Keys left out keep their defaults.
			if !cfg.ExtractVideo || cfg.Log.Format != "auto" {
				t.Fatalf("ExtractVideo = %v, Log.Format = %q, want true and auto", cfg.ExtractVideo, cfg.Log.Format)
			}
		})
	}
}

func TestLoadReadsExplicitJSONConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom.JSON")
	if err := os.WriteFile(configPath, []byte(`{"input": "from-file.jpg", "rename_orig": true}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := loadConfigForTest(t, "--config", configPath)

	if !slices.Equal(cfg.Inputs, []string{"from-file.jpg"}) || !cfg.RenameOrig {
		t.Fatalf("Inputs = %q, RenameOrig = %v, want from-file.jpg and true", cfg.Inputs, cfg.RenameOrig)
	}
}

// A config file in a format earlier versions read is not picked up. It is
// reported, unless a file that is read sits next to it.
func TestLoadReportsIgnoredConfigFiles(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "go-motion-photo")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	workDir := t.TempDir()
	for _, path := range []string{
		filepath.Join(workDir, "go-motion-photo.toml"),
		filepath.Join(configDir, "go-motion-photo.ini"),
	} {
		if err := os.WriteFile(path, []byte("output = \"from-legacy\"\n"), 0644); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}

	load := func() *config.Config {
		t.Helper()
		t.Chdir(workDir)
		t.Setenv("HOME", home)
		cfg, err := config.Load([]string{"photo.jpg"})
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		return cfg
	}

	cfg := load()
	want := []string{"go-motion-photo.toml", filepath.Join(configDir, "go-motion-photo.ini")}
	if cfg.ConfigFile != "" || cfg.OutputDir != "." || !slices.Equal(cfg.IgnoredConfigFiles, want) {
		t.Fatalf("ConfigFile = %q, OutputDir = %q, IgnoredConfigFiles = %q, want none, . and %q",
			cfg.ConfigFile, cfg.OutputDir, cfg.IgnoredConfigFiles, want)
	}

	if err := os.WriteFile(filepath.Join(configDir, "go-motion-photo.yaml"), []byte("output: from-home\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg = load()
	if cfg.OutputDir != "from-home" || !slices.Equal(cfg.IgnoredConfigFiles, want[:1]) {
		t.Fatalf("OutputDir = %q, IgnoredConfigFiles = %q, want from-home and %q", cfg.OutputDir, cfg.IgnoredConfigFiles, want[:1])
	}

	writeConfig(t, workDir, "output: from-work-dir\n")
	cfg = load()
	if cfg.OutputDir != "from-work-dir" || len(cfg.IgnoredConfigFiles) != 0 {
		t.Fatalf("OutputDir = %q, IgnoredConfigFiles = %q, want from-work-dir and none", cfg.OutputDir, cfg.IgnoredConfigFiles)
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

// Asking for the version must work even when the configuration is unusable.
func TestLoadReportsVersionDespiteBrokenConfig(t *testing.T) {
	tempDir := t.TempDir()
	writeConfig(t, tempDir, "output: [unterminated\n")

	if cfg := loadConfigForTestInDir(t, tempDir, "--version"); !cfg.ShowVersion {
		t.Fatal("ShowVersion = false, want true")
	}
	if cfg := loadConfigForTestInDir(t, tempDir, "--version", "--config", "missing.yaml"); !cfg.ShowVersion {
		t.Fatal("ShowVersion = false with a missing --config, want true")
	}
	if _, err := config.Load([]string{"photo.jpg"}); err == nil {
		t.Fatal("Load() without --version error = nil, want the config error")
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

func TestLoadReadsConfigFromHomeConfigDirectory(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "go-motion-photo")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "go-motion-photo.yml"), []byte("output: from-home\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Chdir(t.TempDir())
	t.Setenv("HOME", home)

	cfg, err := config.Load([]string{"photo.jpg"})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.OutputDir != "from-home" {
		t.Fatalf("OutputDir = %q, want %q", cfg.OutputDir, "from-home")
	}
}

func TestLoadParsesShortFlags(t *testing.T) {
	cfg := loadConfigForTest(t, "-i", "photo.jpg", "-o", "out", "-f")

	if !slices.Equal(cfg.Inputs, []string{"photo.jpg"}) {
		t.Fatalf("Inputs = %q, want %q", cfg.Inputs, []string{"photo.jpg"})
	}
	if cfg.OutputDir != "out" {
		t.Fatalf("OutputDir = %q, want %q", cfg.OutputDir, "out")
	}
	if !cfg.Force {
		t.Fatal("Force = false, want true")
	}
}

func TestLoadVerboseAndQuietSetLogLevel(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"-v", "photo.jpg"}, want: "debug"},
		{args: []string{"--quiet", "photo.jpg"}, want: "warn"},
		{args: []string{"--log-level", "error", "--verbose", "photo.jpg"}, want: "debug"},
	}

	for _, tc := range tests {
		cfg := loadConfigForTest(t, tc.args...)
		if cfg.Log.Level != tc.want {
			t.Fatalf("Load(%q): Log.Level = %q, want %q", tc.args, cfg.Log.Level, tc.want)
		}
	}
}

func TestLoadReadsLogFormat(t *testing.T) {
	if cfg := loadConfigForTest(t, "photo.jpg"); cfg.Log.Format != "auto" {
		t.Fatalf("Log.Format = %q, want %q", cfg.Log.Format, "auto")
	}

	t.Setenv("GO_MOTION_PHOTO_LOG_FORMAT", "json")
	if cfg := loadConfigForTest(t, "photo.jpg"); cfg.Log.Format != "json" {
		t.Fatalf("Log.Format = %q, want %q", cfg.Log.Format, "json")
	}
	if cfg := loadConfigForTest(t, "--log-format", "text", "photo.jpg"); cfg.Log.Format != "text" {
		t.Fatalf("Log.Format = %q, want %q", cfg.Log.Format, "text")
	}
}

func TestLoadReportsConfigFile(t *testing.T) {
	if cfg := loadConfigForTest(t, "photo.jpg"); cfg.ConfigFile != "" {
		t.Fatalf("ConfigFile = %q, want none", cfg.ConfigFile)
	}

	tempDir := t.TempDir()
	writeConfig(t, tempDir, "output: from-file\n")
	if cfg := loadConfigForTestInDir(t, tempDir, "photo.jpg"); cfg.ConfigFile != "go-motion-photo.yaml" {
		t.Fatalf("ConfigFile = %q, want %q", cfg.ConfigFile, "go-motion-photo.yaml")
	}
}
