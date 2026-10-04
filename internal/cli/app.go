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
	"path/filepath"
	"strings"
	"syscall"
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

	// errAlreadyExtracted is why an original that an earlier run moved aside
	// is left alone.
	errAlreadyExtracted = errors.New("already extracted")
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
	for _, path := range cfg.IgnoredConfigFiles {
		rep.ignoredConfig(path)
	}
	// Cleaning up an output directory needs no input.
	if len(cfg.Inputs) == 0 && !(cfg.Recover && cfg.OutputDir != "") {
		usage.failure(errors.New("no input specified"), usageLine, helpHint)
		return 1
	}

	ctx, stop := interruptContext()
	defer stop()
	listening(ctx)

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

// interruptSignals are the ways a run is asked to stop: Ctrl+C, a plain kill
// and the terminal going away.
var interruptSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

// listening is called once a run reacts to interrupts. Tests replace it to
// send one.
var listening = func(context.Context) {}

// interruptContext returns a context that is canceled by the first interrupt.
// The run then stops after the file being processed, so that no half-written
// output is left behind. A second interrupt kills it.
func interruptContext() (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), interruptSignals...)
	context.AfterFunc(ctx, stop)
	return ctx, stop
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
	return &reporter{log: log, out: out, deleteOrig: cfg.DeleteOrig}, closeLog, nil
}

// process extracts every file designated by cfg.Inputs. It keeps going after
// a file fails or a directory cannot be read, and returns errFilesFailed
// once everything was tried.
func process(ctx context.Context, cfg *config.Config, rep *reporter) error {
	if !cfg.ExtractPhoto && !cfg.ExtractVideo {
		return extractor.ErrNothingToExtract
	}

	// What an interrupted run left behind is dealt with before the inputs
	// are looked for: one of them may be among it.
	if err := checkLeftovers(cfg, rep); err != nil {
		return err
	}

	var files []string
	explicit := true
	seen := make(map[string]bool)
	for _, input := range cfg.Inputs {
		rep.scanning(input)
		unreadable := rep.unreadable
		found, isExplicit, err := resolveInput(input, rep.unreadableDir)
		if err != nil {
			return err
		}
		// An input with no files needs no explaining once it was reported
		// as unreadable.
		if !isExplicit && (len(found) > 0 || rep.unreadable == unreadable) {
			rep.resolved(input, len(found))
		}
		// A file designated by several inputs is processed once.
		for _, file := range found {
			if key := pathKey(file); !seen[key] {
				seen[key] = true
				files = append(files, file)
			}
		}
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
	// that are not motion photos are expected and skipped, and so are those
	// of another type that a shell pattern such as * brought in.
	strict := explicit && len(files) == 1

	// Files of the same name in different directories share their outputs
	// when these go to one directory. The first one extracted owns them; the
	// others would overwrite its outputs or pass them off as their own.
	owners := make(map[string]string)

	// With --force an output replaces what is there, which must not be
	// another file of the run.
	inputs := make(map[string]bool)
	if opts.Overwrite {
		for _, file := range files {
			inputs[entryKey(file)] = true
			// Replacing the file that a link leads to takes it from the link.
			if resolved, err := filepath.EvalSymlinks(file); err == nil {
				inputs[entryKey(resolved)] = true
			}
		}
	}

	rep.begin(files)
	start := time.Now()
	interrupted := false
	for i, file := range files {
		if ctx.Err() != nil {
			interrupted = true
			break
		}
		rep.processing(i, file)

		if err := alreadyExtracted(file, opts); err != nil {
			rep.skippedFile(file, err)
			continue
		}

		// A file that cannot be extracted has no outputs to collide with;
		// extraction reports what is wrong with it.
		targets := extractor.Targets(file, opts)
		err := checkOwners(owners, targets)
		if err == nil && opts.Overwrite {
			// The last target of a renamed original is where it is moved to,
			// which extraction never replaces with something else.
			outputs := targets
			if opts.RenameOriginal {
				outputs = targets[:len(targets)-1]
			}
			err = checkInputs(inputs, file, outputs)
		}
		if err != nil && extractor.SupportedExtension(file) {
			rep.failedFile(file, err)
			continue
		}

		res, err := extractor.ExtractFile(file, opts)
		// A file also owns its outputs when it failed after they were
		// written, which is when a result comes along with the error.
		if err == nil || res.OriginalPath != "" {
			for _, target := range targets {
				owners[pathKey(target)] = file
			}
		}
		switch {
		case err == nil:
			rep.extractedFile(file, res)
		case !strict && (errors.Is(err, extractor.ErrNotMotionPhoto) || errors.Is(err, extractor.ErrUnsupportedExtension)):
			rep.skippedFile(file, err)
		default:
			rep.failedFile(file, err)
		}
	}
	// An interrupt while the last file was processed stopped nothing, but
	// is still how the run ended.
	interrupted = interrupted || ctx.Err() != nil
	rep.finish(cfg.OutputDir, time.Since(start), interrupted)

	switch {
	case interrupted:
		return errInterrupted
	case rep.failed > 0 || rep.unreadable > 0:
		return errFilesFailed
	default:
		return nil
	}
}

// checkLeftovers looks for what an interrupted run left behind in the
// directories that this one writes to. It is cleaned up with --recover and
// only pointed out without. An error stops the run before it extracts
// anything.
func checkLeftovers(cfg *config.Config, rep *reporter) error {
	dirs := outputDirs(cfg.OutputDir, cfg.Inputs)

	if !cfg.Recover {
		for _, dir := range dirs {
			// A directory that cannot be read is reported when its files
			// are looked for.
			if found, err := extractor.FindLeftovers(dir); err == nil && len(found) > 0 {
				rep.leftovers(dir, found)
			}
		}
		return nil
	}

	nothing := true
	for _, dir := range dirs {
		report, err := extractor.Recover(dir)
		rep.recovered(report)
		if err != nil {
			return fmt.Errorf("recover %s: %w", dir, err)
		}
		nothing = nothing && report.Empty()
	}
	if nothing && len(dirs) > 0 {
		rep.nothingToRecover(dirs)
	}
	return nil
}

// checkOwners returns an error if one of targets is an output of a file
// extracted earlier in the run.
func checkOwners(owners map[string]string, targets []string) error {
	for _, target := range targets {
		if owner, ok := owners[pathKey(target)]; ok {
			return fmt.Errorf("%s is already the output of %s, extract this file to another directory", target, owner)
		}
	}
	return nil
}

// checkInputs returns an error if one of outputs, those of file, would
// replace another file of the run that is a motion photo itself. One that is
// not, an output of an earlier run found along with its original, may be
// replaced.
func checkInputs(inputs map[string]bool, file string, outputs []string) error {
	for _, target := range outputs {
		if key := entryKey(target); key == entryKey(file) || !inputs[key] {
			continue
		}
		data, err := os.ReadFile(target)
		if err != nil {
			continue
		}
		if _, err := extractor.Split(data); err == nil {
			return fmt.Errorf("%s would be overwritten, which is a motion photo to extract itself; extract this file to another directory", target)
		}
	}
	return nil
}

// entryKey identifies the directory entry at path, whichever way its
// directory is reached. Unlike pathKey it tells a file that exists from
// another one, but not one that is yet to be written.
func entryKey(path string) string {
	key := pathKey(path)
	if dir, err := filepath.EvalSymlinks(filepath.Dir(key)); err == nil {
		return filepath.Join(dir, filepath.Base(key))
	}
	return key
}

// alreadyExtracted returns an error if file is an original that an earlier
// run with --rename-orig moved aside. Extracting it again would move it
// aside once more, as IMG_original_original.jpg, on every run.
func alreadyExtracted(file string, opts extractor.Options) error {
	if !opts.RenameOriginal {
		return nil
	}
	ext := filepath.Ext(file)
	name, ok := strings.CutSuffix(strings.TrimSuffix(filepath.Base(file), ext), "_original")
	if !ok || name == "" {
		return nil
	}

	// The targets of the file it would be the original of end with that
	// original, which is file only if it is where originals are moved to.
	// Either component tells of an earlier run, whichever this one extracts.
	opts.SkipPhoto, opts.SkipVideo = false, false
	targets := extractor.Targets(filepath.Join(filepath.Dir(file), name+ext), opts)
	outputs, original := targets[:len(targets)-1], targets[len(targets)-1]
	if entryKey(original) != entryKey(file) {
		return nil
	}
	for _, output := range outputs {
		if _, err := os.Lstat(output); err == nil {
			return fmt.Errorf("%w: it is the original of %s", errAlreadyExtracted, output)
		}
	}
	return nil
}

// pathKey identifies the file at path however the path is spelled.
func pathKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}
