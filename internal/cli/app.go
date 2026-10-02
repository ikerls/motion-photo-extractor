// Package cli implements the go-motion-photo command.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/ikerls/motion-photo-extractor/internal/config"
	"github.com/ikerls/motion-photo-extractor/internal/logger"
	"github.com/ikerls/motion-photo-extractor/pkg/extractor"
)

// exitInterrupted is the conventional exit status after SIGINT.
const exitInterrupted = 130

var (
	// errFilesFailed and errInterrupted end a run whose outcome the reporter
	// has already described.
	errFilesFailed = errors.New("files failed")
	errInterrupted = errors.New("interrupted")
)

// Run executes the command with args (without the program name) and returns
// the process exit status.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	// Mistakes in the invocation are always shown, whatever the log options.
	usage := newConsole(stderr, slog.LevelInfo)

	cfg, err := config.Load(args)
	if errors.Is(err, config.ErrHelp) {
		printHelp(stdout)
		return 0
	}
	if err != nil {
		usage.failure(err, helpHint)
		return 1
	}

	if cfg.ShowVersion {
		fmt.Fprintf(stdout, "%s %s\n", appName, version)
		return 0
	}

	rep, closeLog, err := newReporter(cfg, stderr)
	if err != nil {
		usage.failure(err, helpHint)
		return 1
	}
	defer closeLog()

	if cfg.ConfigFile != "" {
		rep.usingConfig(cfg.ConfigFile)
	}
	if len(cfg.Inputs) == 0 {
		usage.failure(errors.New("no input specified"), usageLine, helpHint)
		return 1
	}

	// The first interrupt stops the run after the file being processed, so
	// that no half-written output is left behind. A second one kills it.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	context.AfterFunc(ctx, stop)

	switch err := process(ctx, cfg, rep); {
	case err == nil:
		return 0
	case errors.Is(err, errInterrupted):
		return exitInterrupted
	case errors.Is(err, errFilesFailed):
		return 1
	default:
		rep.fatal(err)
		return 1
	}
}

// newReporter sets up the output chosen by the log options: either the
// human-readable console or structured logs on stderr, plus the log file.
// The returned function closes the log file, if any.
func newReporter(cfg *config.Config, stderr io.Writer) (*reporter, func() error, error) {
	level, err := logger.ParseLevel(cfg.Log.Level)
	if err != nil {
		return nil, nil, err
	}

	var pretty bool
	switch cfg.Log.Format {
	case "auto":
		pretty = isTerminal(stderr)
	case "pretty":
		pretty = true
	case "text", "json":
	default:
		return nil, nil, fmt.Errorf("invalid log format %q: use auto, pretty, text or json", cfg.Log.Format)
	}

	out := newConsole(io.Discard, level)
	logOpts := logger.Options{File: cfg.Log.File, Level: level, JSON: cfg.Log.Format == "json"}
	switch {
	case cfg.Log.NoConsole:
	case pretty:
		out = newConsole(stderr, level)
	default:
		logOpts.Console = stderr
	}

	log, closeLog, err := logger.New(logOpts)
	if err != nil {
		return nil, nil, err
	}
	return &reporter{log: log, out: out}, closeLog, nil
}

// process extracts every file designated by cfg.Inputs. It keeps going after
// a file fails and returns errFilesFailed once all of them were tried.
func process(ctx context.Context, cfg *config.Config, rep *reporter) error {
	if !cfg.ExtractPhoto && !cfg.ExtractVideo {
		return extractor.ErrNothingToExtract
	}

	var files []string
	explicit := true
	for _, input := range cfg.Inputs {
		rep.scanning(input)
		found, isExplicit, err := resolveInput(input)
		if err != nil {
			return err
		}
		if !isExplicit {
			rep.resolved(input, len(found))
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

	rep.begin(files)
	start := time.Now()
	interrupted := false
	for i, file := range files {
		if ctx.Err() != nil {
			interrupted = true
			break
		}
		rep.processing(i, file)

		res, err := extractor.ExtractFile(file, opts)
		switch {
		case err == nil:
			rep.extractedFile(file, res)
		case !strict && errors.Is(err, extractor.ErrNotMotionPhoto):
			rep.skippedFile(file, err)
		default:
			rep.failedFile(file, err)
		}
	}
	rep.finish(cfg.OutputDir, time.Since(start), interrupted)

	switch {
	case interrupted:
		return errInterrupted
	case rep.failed > 0:
		return errFilesFailed
	default:
		return nil
	}
}
