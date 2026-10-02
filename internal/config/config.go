package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"go.yaml.in/yaml/v3"
)

// ErrHelp is returned by Load when --help was requested.
var ErrHelp = pflag.ErrHelp

type Config struct {
	// Inputs holds the files, directories or patterns to process. It comes
	// from --input, the positional arguments, or the "input" config key, in
	// that order of preference.
	Inputs []string `yaml:"-"`

	OutputDir    string    `yaml:"output"`
	DeleteOrig   bool      `yaml:"delete_orig"`
	RenameOrig   bool      `yaml:"rename_orig"`
	ExtractPhoto bool      `yaml:"extract_photo"`
	ExtractVideo bool      `yaml:"extract_video"`
	Force        bool      `yaml:"force"`
	Log          LogConfig `yaml:"log"`

	// ShowVersion is set when --version was passed.
	ShowVersion bool `yaml:"-"`
}

type LogConfig struct {
	File      string `yaml:"file"`
	Level     string `yaml:"level"`
	NoConsole bool   `yaml:"no_console"`
}

const (
	appName   = "go-motion-photo"
	envPrefix = "GO_MOTION_PHOTO"
)

const Usage = `Usage: go-motion-photo [options] <file|directory|pattern>...

Arguments:
  <file>...            One or more motion photo files, directories or patterns

Input Options:
  --input <path>       Path to a motion photo file or directory with supported files
                       Supported formats: .jpg, .jpeg, .heic
                       Glob patterns are expanded: 'photos/*.jpg'
                       For regex patterns, enclose pattern in forward slashes: /pattern/
                       (matched against file names in the current directory)

Output Options:
  --output <dir>       Directory to save extracted files (default: ".")
  --delete-orig        Delete original file after successful extraction
  --rename-orig        Rename original file instead of adding suffixes to extracted files
                       (Original gets _original suffix, extracted files use base name)
  --extract-photo      Extract the photo component (default: true)
  --extract-video      Extract the video component (default: true)
  --force              Force overwrite of existing output files

Logging Options:
  --log-file <path>    Path to log file (if not specified, logs to console only)
  --log-level <level>  Log level: debug, info, warn, error (default: "info")
  --no-console-log     Disable console logging (only log to file if specified)

Configuration:
  --config <path>      Path to configuration file
                       When not specified, searches for 'go-motion-photo.yaml' in:
                       - Current directory
                       - $HOME/.config/go-motion-photo
                       Every config key can also be set through the environment,
                       e.g. GO_MOTION_PHOTO_OUTPUT, GO_MOTION_PHOTO_LOG_LEVEL

Other:
  --version            Print version and exit
  --help               Show this help

When more than one file is processed, files that are not motion photos are skipped.
The exit status is 1 if any file failed.

Examples:
  go-motion-photo photo.jpg                                # Process single file
  go-motion-photo a.jpg b.jpg c.heic                       # Process several files
  go-motion-photo --input photo.jpg --output ./extracted   # Specify output location
  go-motion-photo --input ./photos                         # Process all supported files in directory
  go-motion-photo --input /IMG_\d{4}\.jpg/                 # Process files matching regex pattern
  go-motion-photo --input photo.jpg --rename-orig          # Keep original naming scheme
  go-motion-photo --input photo.jpg --extract-video=false  # Extract only photo component
  go-motion-photo --input photo.heic --force               # Process HEIC file and overwrite existing outputs`

// flagKeys maps each CLI flag to the config key it sets. The key also names
// the environment variable: log.level is read from GO_MOTION_PHOTO_LOG_LEVEL.
var flagKeys = map[string]string{
	"input":          "input",
	"output":         "output",
	"delete-orig":    "delete_orig",
	"rename-orig":    "rename_orig",
	"extract-photo":  "extract_photo",
	"extract-video":  "extract_video",
	"force":          "force",
	"log-file":       "log.file",
	"log-level":      "log.level",
	"no-console-log": "log.no_console",
}

// Load builds the configuration from args (without the program name), the
// environment and the config file. Precedence, highest first: CLI flags,
// environment variables, config file, defaults.
func Load(args []string) (*Config, error) {
	var file struct {
		Input  string `yaml:"input"`
		Config `yaml:",inline"`
	}
	cfg := &file.Config

	flags := pflag.NewFlagSet(appName, pflag.ContinueOnError)
	flags.Usage = func() {}
	flags.StringVar(&file.Input, "input", "", "Input motion photo file or directory path (*.jpg, *.jpeg, *.heic)")
	flags.StringVar(&cfg.OutputDir, "output", ".", "Directory to save extracted files")
	flags.BoolVar(&cfg.DeleteOrig, "delete-orig", false, "Delete original file after successful extraction")
	flags.BoolVar(&cfg.RenameOrig, "rename-orig", false, "Rename original file and don't append _photo/_video to extracted files")
	flags.BoolVar(&cfg.ExtractPhoto, "extract-photo", true, "Extract photo part")
	flags.BoolVar(&cfg.ExtractVideo, "extract-video", true, "Extract video part")
	flags.BoolVar(&cfg.Force, "force", false, "Force overwrite existing files")
	flags.StringVar(&cfg.Log.File, "log-file", "", "Log to file")
	flags.StringVar(&cfg.Log.Level, "log-level", "info", "Log level (debug, info, warn, error)")
	flags.BoolVar(&cfg.Log.NoConsole, "no-console-log", false, "Disable console logging")
	flags.BoolVar(&cfg.ShowVersion, "version", false, "Print version and exit")
	configFile := flags.String("config", "", "Config file path (optional)")

	if err := flags.Parse(args); err != nil {
		return nil, err
	}

	// cfg now holds the defaults overlaid with the command line. The config
	// file and the environment rank in between, so they are applied on top
	// and the command line values are then put back.
	fromCLI := make(map[string]string)
	flags.Visit(func(f *pflag.Flag) {
		fromCLI[f.Name] = f.Value.String()
	})

	data, err := readConfigFile(*configFile)
	if err == nil {
		err = yaml.Unmarshal(data, &file)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	for flagName, key := range flagKeys {
		name := envName(key)
		value := os.Getenv(name)
		if value == "" {
			continue
		}
		if err := flags.Set(flagName, value); err != nil {
			return nil, fmt.Errorf("invalid %s: %w", name, err)
		}
	}

	for flagName, value := range fromCLI {
		if err := flags.Set(flagName, value); err != nil {
			return nil, err
		}
	}

	_, inputFromCLI := fromCLI["input"]
	switch {
	case inputFromCLI:
		cfg.Inputs = []string{file.Input}
	case flags.NArg() > 0:
		cfg.Inputs = flags.Args()
	case file.Input != "":
		cfg.Inputs = []string{file.Input}
	}

	return cfg, nil
}

// readConfigFile returns the contents of the config file at path, or of the
// first one found in the default locations when path is empty. Not finding
// one in the default locations is not an error.
func readConfigFile(path string) ([]byte, error) {
	if path != "" {
		return os.ReadFile(path)
	}

	dirs := []string{"."}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".config", appName))
	}

	for _, dir := range dirs {
		for _, ext := range []string{".yaml", ".yml"} {
			data, err := os.ReadFile(filepath.Join(dir, appName+ext))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return data, err
		}
	}

	return nil, nil
}

func envName(key string) string {
	return envPrefix + "_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}
