package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Config struct {
	InputFile    string `mapstructure:"input"`
	OutputDir    string `mapstructure:"output"`
	DeleteOrig   bool   `mapstructure:"delete_orig"`
	RenameOrig   bool   `mapstructure:"rename_orig"`
	ExtractPhoto bool   `mapstructure:"extract_photo"`
	Force        bool   `mapstructure:"force"`
	Log          struct {
		File      string `mapstructure:"file"`
		Level     string `mapstructure:"level"`
		NoConsole bool   `mapstructure:"no_console"`
	} `mapstructure:"log"`
}

const usage = `Usage: go-motion-photo [--input <file|directory|/regex/>] [options]

Arguments:
  <file>               Motion photo file (same as --input)

Input Options:
  --input <path>       Path to a motion photo file or directory with supported files
                       Supported formats: .jpg, .jpeg, .heic
                       For regex patterns, enclose pattern in forward slashes: /pattern/

Output Options:
  --output <dir>       Directory to save extracted files (default: ".")
  --delete-orig        Delete original file after successful extraction
  --rename-orig        Rename original file instead of adding suffixes to extracted files
                       (Original gets _original suffix, extracted files use base name)
  --extract-photo      Extract the photo component (default: true)
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

Examples:
  go-motion-photo photo.jpg                                # Process single file
  go-motion-photo --input photo.jpg --output ./extracted   # Specify output location
  go-motion-photo --input ./photos                         # Process all supported files in directory
  go-motion-photo --input /IMG_\d{4}\.jpg/                 # Process files matching regex pattern
  go-motion-photo --input photo.jpg --rename-orig          # Keep original naming scheme
  go-motion-photo --input photo.heic --force               # Process HEIC file and overwrite existing outputs`

func Load() (*Config, error) {
	pflag.String("input", "", "Input motion photo file or directory path (*.jpg, *.jpeg, *.heic)")
	pflag.String("output", ".", "Directory to save extracted files")
	pflag.Bool("delete-orig", false, "Delete original file after successful extraction")
	pflag.Bool("rename-orig", false, "Rename original file and don't append _photo/_video to extracted files")
	pflag.Bool("extract-photo", true, "Extract photo part")
	pflag.String("config", "", "Config file path (optional)")
	pflag.Bool("force", false, "Force overwrite existing files")
	pflag.String("log-file", "", "Log to file")
	pflag.Bool("no-console-log", false, "Disable console logging")
	pflag.String("log-level", "info", "Log level (debug, info, warn, error)")

	pflag.Usage = func() {
		fmt.Println(usage)
	}
	pflag.Parse()

	viper.BindPFlags(pflag.CommandLine)

	// Handle positional arguments
	args := pflag.Args()
	if viper.GetString("input") == "" && len(args) > 0 {
		viper.Set("input", args[0])
	}

	if configFile := viper.GetString("config"); configFile != "" {
		viper.SetConfigFile(configFile)
	} else {
		viper.SetConfigName("go-motion-photo")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.config/go-motion-photo")
	}

	viper.SetDefault("output", ".")
	viper.SetDefault("extract_photo", true)
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.no_console", false)

	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	if err := viper.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFoundError) {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal configuration: %w", err)
	}

	return &cfg, nil
}
