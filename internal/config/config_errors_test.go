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

// A variable that a flag overrides is not read, so its value cannot be what
// is wrong with the invocation.
func TestLoadIgnoresInvalidEnvironmentValueOverriddenByFlag(t *testing.T) {
	t.Setenv("GO_MOTION_PHOTO_FORCE", "maybe")

	if err := loadError(t, t.TempDir(), "--force=false", "photo.jpg"); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
}

func TestLoadRejectsUnknownConfigKeys(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
		want    string
	}{
		{name: "misspelled", file: "go-motion-photo.yaml", content: "outptu: ./elsewhere\n", want: `unknown key "outptu", did you mean "output"?`},
		{name: "in a section", file: "go-motion-photo.yaml", content: "log:\n  levle: debug\n", want: `unknown key "log.levle", did you mean "log.level"?`},
		{name: "unrelated", file: "go-motion-photo.yaml", content: "output: out\nverbosity: high\n", want: `unknown key "verbosity"`},
		{name: "JSON", file: "go-motion-photo.json", content: `{"delete_origg": true}`, want: `unknown key "delete_origg", did you mean "delete_orig"?`},
		{name: "dotted", file: "go-motion-photo.yaml", content: "log.no_console: true\n", want: `unknown key "log.no_console", write it as "no_console" under "log"`},
		{name: "not a string", file: "go-motion-photo.yaml", content: "log:\n  123: ignored\n", want: `unknown key "log.123"`},
		{name: "not a string either", file: "go-motion-photo.yaml", content: "log:\n  true: ignored\n", want: `unknown key "log.true"`},
		{name: "behind an alias", file: "go-motion-photo.yaml", content: "log:\n  file: &name levle\n  *name : debug\n", want: `unknown key "log.levle", did you mean "log.level"?`},
		{name: "merged in", file: "go-motion-photo.yaml", content: "defaults: &defaults\n  levle: debug\nlog:\n  <<: *defaults\n", want: `unknown key "defaults"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(tempDir, tc.file), []byte(tc.content), 0644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			err := loadError(t, tempDir, "photo.jpg")
			if err == nil || !strings.HasSuffix(err.Error(), tc.want) || !strings.Contains(err.Error(), "failed to read config file") {
				t.Fatalf("Load() error = %v, want it to end with %q", err, tc.want)
			}
		})
	}
}

// Keys may be written as YAML allows: merged in from another mapping, or as
// an alias of a value.
func TestLoadAcceptsMergedAndAliasedConfigKeys(t *testing.T) {
	configs := map[string]string{
		"merge":            "log:\n  <<: &defaults\n    level: debug\n",
		"merge of several": "log:\n  <<: [{level: debug}, {format: json}]\n",
		"aliased key":      "log:\n  file: &name level\n  *name : debug\n",
		"aliased section":  "output: &out out\nlog: &log\n  level: debug\n",
	}

	for name, content := range configs {
		t.Run(name, func(t *testing.T) {
			tempDir := t.TempDir()
			writeConfig(t, tempDir, content)
			t.Chdir(tempDir)
			t.Setenv("HOME", tempDir)

			cfg, err := config.Load([]string{"photo.jpg"})
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.Log.Level != "debug" {
				t.Fatalf("Log.Level = %q, want debug", cfg.Log.Level)
			}
		})
	}
}

// Aliases may lead back to where they come from, or to the same mapping any
// number of times. Neither keeps the config file from being turned down.
func TestLoadRejectsConfigWithRunawayAliases(t *testing.T) {
	var nested strings.Builder
	nested.WriteString("a0: &a0\n  level: debug\n")
	for i := 1; i <= 28; i++ {
		fmt.Fprintf(&nested, "a%d: &a%d\n  <<: [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	nested.WriteString("log:\n  <<: *a28\n")

	configs := map[string]string{
		"contains itself": "log: &a\n  <<: *a\n",
		"nested merges":   nested.String(),
	}

	for name, content := range configs {
		t.Run(name, func(t *testing.T) {
			tempDir := t.TempDir()
			writeConfig(t, tempDir, content)

			if err := loadError(t, tempDir, "photo.jpg"); err == nil || !strings.Contains(err.Error(), "failed to read config file") {
				t.Fatalf("Load() error = %v, want a config file error", err)
			}
		})
	}
}

func TestLoadSuggestsFlagForTypo(t *testing.T) {
	tests := []struct {
		arg  string
		want string
	}{
		{arg: "--ouput", want: "did you mean --output?"},
		{arg: "--delete-original", want: ""},
		{arg: "--nope", want: ""},
		{arg: "-x", want: ""},
	}

	for _, tc := range tests {
		err := loadError(t, t.TempDir(), tc.arg, "photo.jpg")
		if err == nil {
			t.Fatalf("Load(%s) error = nil, want non-nil", tc.arg)
		}
		if got := strings.Contains(err.Error(), "did you mean"); got != (tc.want != "") || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("Load(%s) error = %q, want suggestion %q", tc.arg, err.Error(), tc.want)
		}
	}
}

func TestLoadRejectsVerboseWithQuiet(t *testing.T) {
	if err := loadError(t, t.TempDir(), "-v", "-q", "photo.jpg"); err == nil {
		t.Fatal("Load() error = nil, want non-nil")
	}
}
