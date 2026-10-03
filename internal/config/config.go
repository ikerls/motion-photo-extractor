package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// ErrHelp is returned by Load when --help was requested.
var ErrHelp = pflag.ErrHelp

type Config struct {
	// Inputs holds the files, directories or patterns to process. It comes
	// from --input, the positional arguments, or the "input" config key, in
	// that order of preference.
	Inputs []string `mapstructure:"-"`

	OutputDir    string    `mapstructure:"output"`
	DeleteOrig   bool      `mapstructure:"delete_orig"`
	RenameOrig   bool      `mapstructure:"rename_orig"`
	ExtractPhoto bool      `mapstructure:"extract_photo"`
	ExtractVideo bool      `mapstructure:"extract_video"`
	Force        bool      `mapstructure:"force"`
	Log          LogConfig `mapstructure:"log"`

	// ShowVersion is set when --version was passed.
	ShowVersion bool `mapstructure:"-"`
}

type LogConfig struct {
	File      string `mapstructure:"file"`
	Level     string `mapstructure:"level"`
	NoConsole bool   `mapstructure:"no_console"`
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

// flagKeys maps each CLI flag to the config key it sets.
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
	fs := pflag.NewFlagSet(appName, pflag.ContinueOnError)
	fs.Usage = func() {}
	fs.String("input", "", "Input motion photo file or directory path (*.jpg, *.jpeg, *.heic)")
	fs.String("output", ".", "Directory to save extracted files")
	fs.Bool("delete-orig", false, "Delete original file after successful extraction")
	fs.Bool("rename-orig", false, "Rename original file and don't append _photo/_video to extracted files")
	fs.Bool("extract-photo", true, "Extract photo part")
	fs.Bool("extract-video", true, "Extract video part")
	fs.Bool("force", false, "Force overwrite existing files")
	fs.String("log-file", "", "Log to file")
	fs.String("log-level", "info", "Log level (debug, info, warn, error)")
	fs.Bool("no-console-log", false, "Disable console logging")
	configFile := fs.String("config", "", "Config file path (optional)")
	showVersion := fs.Bool("version", false, "Print version and exit")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	v := viper.New()
	for flagName, key := range flagKeys {
		if err := v.BindPFlag(key, fs.Lookup(flagName)); err != nil {
			return nil, fmt.Errorf("failed to bind CLI flags: %w", err)
		}
	}

	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if *configFile != "" {
		v.SetConfigFile(*configFile)
	} else {
		v.SetConfigName(appName)
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.config/" + appName)
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := errors.AsType[viper.ConfigFileNotFoundError](err); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	cfg := Config{ShowVersion: *showVersion}
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal configuration: %w", err)
	}

	switch input := v.GetString("input"); {
	case fs.Changed("input"):
		cfg.Inputs = []string{input}
	case fs.NArg() > 0:
		cfg.Inputs = fs.Args()
	case input != "":
		cfg.Inputs = []string{input}
	}

	return &cfg, nil
}
