// Package cli implements the go-motion-photo command.
package cli

import (
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/ikerls/motion-photo-extractor/internal/config"
	"github.com/ikerls/motion-photo-extractor/internal/logger"
	"github.com/ikerls/motion-photo-extractor/pkg/extractor"
)

// Run executes the command with args (without the program name) and returns
// the process exit status.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	cfg, err := config.Load(args)
	if errors.Is(err, config.ErrHelp) {
		fmt.Fprintln(stdout, config.Usage)
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\nUse --help for more information\n", err)
		return 1
	}

	if cfg.ShowVersion {
		fmt.Fprintf(stdout, "go-motion-photo %s\n", version)
		return 0
	}

	logOpts := logger.Options{File: cfg.Log.File, Level: cfg.Log.Level}
	if !cfg.Log.NoConsole {
		logOpts.Console = stderr
	}
	log, closeLog, err := logger.New(logOpts)
	if err != nil {
		fmt.Fprintf(stderr, "Failed to setup logger: %v\n", err)
		return 1
	}
	defer closeLog()

	if len(cfg.Inputs) == 0 {
		log.Error("No input file specified")
		log.Info("Use --help for more information")
		return 1
	}

	if err := process(cfg, log); err != nil {
		log.Error(err.Error())
		return 1
	}
	return 0
}

// process extracts every file designated by cfg.Inputs. It keeps going after
// a file fails and reports the failures in the returned error.
func process(cfg *config.Config, log *slog.Logger) error {
	var files []string
	explicit := true
	for _, input := range cfg.Inputs {
		found, isExplicit, err := resolveInput(input)
		if err != nil {
			return err
		}
		if !isExplicit {
			log.Info("Resolved input", "input", input, "files", len(found))
		}
		files = append(files, found...)
		explicit = explicit && isExplicit
	}

	opts := extractor.Options{
		OutputDir:      cfg.OutputDir,
		SkipPhoto:      !cfg.ExtractPhoto,
		SkipVideo:      !cfg.ExtractVideo,
		Overwrite:      cfg.Force,
		RenameOriginal: cfg.RenameOrig,
		DeleteOriginal: cfg.DeleteOrig,
	}

	// A single file named directly must be a motion photo. In a batch, files
	// that are not motion photos are expected and skipped.
	strict := explicit && len(files) == 1

	var extracted, skipped, failed int
	for _, file := range files {
		res, err := extractor.ExtractFile(file, opts)
		switch {
		case err == nil:
			extracted++
			report(log, file, res)
		case !strict && errors.Is(err, extractor.ErrNotMotionPhoto):
			skipped++
			log.Debug("Skipped, not a motion photo", "file", file, "reason", err)
		case strict:
			return fmt.Errorf("%s: %w", file, err)
		default:
			failed++
			log.Error("Extraction failed", "file", file, "err", err)
		}
	}

	if !strict {
		log.Info("Done", "extracted", extracted, "skipped", skipped, "failed", failed)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d files failed", failed, len(files))
	}
	return nil
}

func report(log *slog.Logger, file string, res extractor.Result) {
	for _, path := range res.Skipped {
		log.Warn("Output already exists, not overwritten (use --force)", "path", path)
	}

	attrs := []any{"file", file}
	if res.PhotoPath != "" {
		attrs = append(attrs, "photo", res.PhotoPath)
	}
	if res.VideoPath != "" {
		attrs = append(attrs, "video", res.VideoPath)
	}
	switch res.OriginalPath {
	case file:
	case "":
		attrs = append(attrs, "original", "deleted")
	default:
		attrs = append(attrs, "original", res.OriginalPath)
	}
	if res.PhotoPath == "" && res.VideoPath == "" {
		log.Info("Nothing written", attrs...)
	} else {
		log.Info("Extracted", attrs...)
	}
	log.Debug("Split point located", "file", file, "method", string(res.Method))
}
