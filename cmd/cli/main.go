package main

import (
	"fmt"
	"github.com/charmbracelet/log"
	"github.com/ikerls/motion-photos-extractor/internal/config"
	"github.com/ikerls/motion-photos-extractor/internal/logger"
	"github.com/ikerls/motion-photos-extractor/pkg/extractor"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func processInputs(cfg *config.Config, e *extractor.Extractor) error {
	// Regex check
	if len(cfg.InputFile) >= 2 && strings.HasPrefix(cfg.InputFile, "/") && strings.HasSuffix(cfg.InputFile, "/") {
		patternStr := cfg.InputFile[1 : len(cfg.InputFile)-1]
		pattern, err := regexp.Compile(patternStr)
		if err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
		return processRegexPattern(pattern, cfg, e)
	}

	// Glob check
	if containsGlob(cfg.InputFile) {
		log.Infof("Processing glob pattern: %s\n", cfg.InputFile)
		matches, err := filepath.Glob(cfg.InputFile)
		if err != nil {
			return fmt.Errorf("invalid glob pattern: %w", err)
		}
		return processFiles(matches, cfg, e)
	}

	// Directory check
	if info, err := os.Stat(cfg.InputFile); err == nil && info.IsDir() {
		log.Infof("Processing directory: %s\n", cfg.InputFile)
		return processDirectory(cfg.InputFile, cfg, e)
	}

	log.Infof("Processing single file: %s\n", cfg.InputFile)
	return e.Process(cfg.InputFile, cfg.OutputDir, cfg.DeleteOrig, cfg.RenameOrig, cfg.ExtractPhoto, cfg.Force)
}

func processDirectory(dir string, cfg *config.Config, e *extractor.Extractor) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && isValidExtension(path) {
			if err := e.Process(path, cfg.OutputDir, cfg.DeleteOrig, cfg.RenameOrig, cfg.ExtractPhoto, cfg.Force); err != nil {
				log.Errorf("Error processing %s: %v\n", path, err)
			}
		}
		return nil
	})
}

func processFiles(files []string, cfg *config.Config, e *extractor.Extractor) error {
	log.Infof("Found %d files \n", len(files))
	for _, file := range files {
		if err := e.Process(file, cfg.OutputDir, cfg.DeleteOrig, cfg.RenameOrig, cfg.ExtractPhoto, cfg.Force); err != nil {
			log.Errorf("Error processing %s: %v\n", file, err)
		}
	}
	return nil
}

func processRegexPattern(pattern *regexp.Regexp, cfg *config.Config, e *extractor.Extractor) error {
	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	log.Infof("Found %d matches for regex pattern\n", len(pattern.FindAllString(cfg.InputFile, -1)))
	for _, entry := range entries {
		if !entry.IsDir() && pattern.MatchString(entry.Name()) && isValidExtension(entry.Name()) {
			fullPath := filepath.Join(dir, entry.Name())
			if err := e.Process(fullPath, cfg.OutputDir, cfg.DeleteOrig, cfg.RenameOrig, cfg.ExtractPhoto, cfg.Force); err != nil {
				log.Errorf("Error processing %s: %v\n", fullPath, err)
			}
		}
	}
	return nil
}

func containsGlob(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

func isValidExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".jpg" || ext == ".jpeg" || ext == ".heic"
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	logErr := logger.Setup(cfg.Log.File, cfg.Log.NoConsole, cfg.Log.Level)
	if logErr != nil {
		fmt.Printf("Failed to setup logger: %v\n", err)
		os.Exit(1)
	}

	if cfg.InputFile == "" {
		log.Error("Error: No input file specified")
		log.Infof("Use --help for more information\n")
		os.Exit(1)
	}
	e := extractor.New()
	if err := processInputs(cfg, e); err != nil {
		log.Errorf("%v\n", err)
		os.Exit(1)
	}
}
