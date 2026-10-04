package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ikerls/motion-photo-extractor/pkg/extractor"
)

// maxNameWidth caps the column file names are aligned in, so that one long
// path does not push every line's details off screen.
const maxNameWidth = 40

// reporter tells the user what happens during a run. Every event goes to
// the structured log and to the console, each of which may be disabled.
type reporter struct {
	log *slog.Logger
	out *console

	// deleteOrig is set when originals are to be deleted, so that one that
	// was kept anyway is pointed out.
	deleteOrig bool

	total     int
	nameWidth int

	extracted   int
	unchanged   int // every output already existed
	notMotion   int
	unsupported int // files in a batch whose extension is not supported
	moved       int // originals that an earlier run moved aside
	failed      int
	unreadable  int // directories whose files were left out

	photos, videos int
	written        int64 // bytes
	kept           int   // existing outputs that were not overwritten
}

// single reports whether the run is about one file, which is then shown in
// full instead of as one line among many.
func (r *reporter) single() bool {
	return r.total == 1
}

func (r *reporter) usingConfig(path string) {
	r.log.Debug("Using config file", "path", path)
	r.out.print(slog.LevelDebug, r.out.dim.Render("Using config file "+path))
}

// ignoredConfig reports a config file that is in a format that is not read.
func (r *reporter) ignoredConfig(path string) {
	r.log.Warn("Config file ignored, only YAML and JSON are supported", "path", path)
	r.out.event(slog.LevelWarn, r.out.warn.Render(symbolWarn), path, "config file ignored, only YAML and JSON are supported")
}

// leftovers points out what an interrupted run left behind in dir.
func (r *reporter) leftovers(dir string, found []extractor.Leftover) {
	lines := []string{fmt.Sprintf("%s %s  %s", r.out.warn.Render(symbolWarn), dir,
		plural(len(found), "leftover")+" of an interrupted run, clean up with --recover")}
	for _, leftover := range found {
		r.log.Warn("Leftover of an interrupted run, clean up with --recover", "path", leftover.Path, "kind", string(leftover.Kind))
		lines = append(lines, "    "+r.out.dim.Render(leftover.Path))
	}
	r.out.print(slog.LevelWarn, lines...)
}

// recovered reports what was done about the leftovers of a directory.
func (r *reporter) recovered(report extractor.RecoveryReport) {
	for _, path := range report.Removed {
		r.log.Info("Removed leftover", "path", path)
		r.out.event(slog.LevelInfo, r.out.ok.Render(symbolOK), path, r.out.dim.Render("removed"))
	}
	for _, restored := range report.Restored {
		details := "restored from " + filepath.Base(restored.Backup)
		if restored.BackupRemains {
			details += ", which could not be removed"
		}
		r.log.Info("Restored from backup", "path", restored.Target, "backup", restored.Backup)
		r.out.event(slog.LevelInfo, r.out.ok.Render(symbolOK), restored.Target, details)
	}
	for _, retained := range report.Retained {
		r.log.Warn("Leftover kept", "path", retained.Path, "reason", retained.Reason)
		r.out.event(slog.LevelWarn, r.out.warn.Render(symbolWarn), retained.Path, "kept: "+retained.Reason)
	}
	for _, moved := range report.MovedAside {
		r.log.Warn("Original moved aside by an interrupted run", "path", moved.Original, "rename_to", moved.Input)
		r.out.event(slog.LevelWarn, r.out.warn.Render(symbolWarn), moved.Original,
			"moved aside by an interrupted run; rename it to "+filepath.Base(moved.Input)+" to extract it")
	}
}

// nothingToRecover reports that --recover found nothing to do in dirs.
func (r *reporter) nothingToRecover(dirs []string) {
	r.log.Info("No leftovers of an interrupted run", "dirs", strings.Join(dirs, ", "))
	r.out.event(slog.LevelInfo, r.out.dim.Render(symbolSkip), strings.Join(dirs, ", "), r.out.dim.Render("no leftovers of an interrupted run"))
}

func (r *reporter) scanning(input string) {
	r.out.showStatus("Scanning " + input + " …")
}

// unreadableDir reports a directory that could not be searched for files.
func (r *reporter) unreadableDir(dir string, err error) {
	r.unreadable++
	r.log.Error("Cannot read directory", "dir", dir, "err", err)
	r.out.event(slog.LevelError, r.out.fail.Render(symbolFail), dir, "cannot read directory: "+describe(err, dir))
}

// resolved reports how many files a directory or pattern expanded to.
func (r *reporter) resolved(input string, files int) {
	r.out.clearProgress()
	if files == 0 {
		r.log.Warn("No supported files found", "input", input)
		r.out.event(slog.LevelWarn, r.out.warn.Render(symbolWarn), input, "no supported files found")
		return
	}
	r.log.Info("Resolved input", "input", input, "files", files)
	r.out.event(slog.LevelInfo, r.out.accent.Render("›"), input, r.out.dim.Render(plural(files, "file")))
}

// begin is called once with every file that is about to be processed.
func (r *reporter) begin(files []string) {
	r.out.clearProgress()
	r.total = len(files)
	for _, file := range files {
		r.nameWidth = max(r.nameWidth, lipgloss.Width(file))
	}
	r.nameWidth = min(r.nameWidth, maxNameWidth)
}

func (r *reporter) processing(index int, file string) {
	if !r.single() {
		r.out.showProgress(index, r.total, file)
	}
}

// detail is one fact about an extracted file: short in a one-line report,
// label and value in a full one.
type detail struct {
	label, value, short string
}

func (r *reporter) extractedFile(file string, res extractor.Result) {
	for _, path := range res.Skipped {
		r.log.Warn("Output already exists, not overwritten (use --force)", "path", path)
	}

	attrs := []any{"file", file}
	var details []detail
	written := func(label, path string) {
		if path == "" {
			return
		}
		size := fileSize(path)
		r.written += size
		attrs = append(attrs, label, path)
		details = append(details, detail{
			label: label,
			value: path + "  " + r.out.dim.Render(formatSize(size)),
			short: label + " " + r.out.dim.Render(formatSize(size)),
		})
	}
	kept := func(label string) {
		for _, path := range res.Skipped {
			if (filepath.Ext(path) == ".mp4") != (label == "video") {
				continue
			}
			r.kept++
			details = append(details, detail{
				label: label,
				value: path + "  " + r.out.warn.Render("already exists, kept"),
				short: r.out.warn.Render(label + " exists, kept"),
			})
		}
	}

	written("photo", res.PhotoPath)
	kept("photo")
	written("video", res.VideoPath)
	kept("video")

	switch res.OriginalPath {
	case file:
		if r.deleteOrig {
			attrs = append(attrs, "original", "kept")
			details = append(details, detail{
				label: "original",
				value: r.out.warn.Render("kept, not every output was written"),
				short: r.out.warn.Render("original kept"),
			})
		}
	case "":
		attrs = append(attrs, "original", "deleted")
		details = append(details, detail{label: "original", value: "deleted", short: "original deleted"})
	default:
		attrs = append(attrs, "original", res.OriginalPath)
		details = append(details, detail{
			label: "original",
			value: "moved to " + res.OriginalPath,
			short: "original → " + filepath.Base(res.OriginalPath),
		})
	}

	if r.out.enabled(slog.LevelDebug) {
		details = append(details, detail{
			label: "split",
			value: r.out.dim.Render("found by " + string(res.Method)),
			short: r.out.dim.Render("split by " + string(res.Method)),
		})
	}

	if res.PhotoPath != "" {
		r.photos++
	}
	if res.VideoPath != "" {
		r.videos++
	}
	if res.PhotoPath == "" && res.VideoPath == "" {
		r.unchanged++
		r.log.Info("Nothing written", attrs...)
	} else {
		r.extracted++
		r.log.Info("Extracted", attrs...)
	}
	r.log.Debug("Split point located", "file", file, "method", string(res.Method))

	level, symbol := slog.LevelInfo, r.out.ok.Render(symbolOK)
	if len(res.Skipped) > 0 {
		level, symbol = slog.LevelWarn, r.out.warn.Render(symbolWarn)
	}
	if r.single() {
		r.out.print(level, r.tree(symbol+" "+r.out.bold.Render(file), details)...)
		return
	}

	short := make([]string, len(details))
	for i, d := range details {
		short[i] = d.short
	}
	r.out.event(level, symbol, r.name(file), strings.Join(short, r.out.dim.Render(" · ")))
}

// tree lays details out as branches under a heading.
func (r *reporter) tree(heading string, details []detail) []string {
	labelWidth := 0
	for _, d := range details {
		labelWidth = max(labelWidth, len(d.label))
	}

	lines := []string{heading}
	for i, d := range details {
		branch := "├─"
		if i == len(details)-1 {
			branch = "└─"
		}
		lines = append(lines, fmt.Sprintf("  %s %s  %s", r.out.dim.Render(branch), padRight(d.label, labelWidth), d.value))
	}
	return lines
}

// skippedFile reports a file left alone because it is not a motion photo,
// not of a supported type or was extracted by an earlier run.
func (r *reporter) skippedFile(file string, reason error) {
	message := "Skipped, not a motion photo"
	switch {
	case errors.Is(reason, extractor.ErrUnsupportedExtension):
		r.unsupported++
		message = "Skipped, unsupported file extension"
	case errors.Is(reason, errAlreadyExtracted):
		r.moved++
		message = "Skipped, already extracted"
	default:
		r.notMotion++
	}

	// Skipped files are summed up at the end of a run, except for a single
	// file: this is then all that is said about it.
	level, details := slog.LevelDebug, reason.Error()
	if r.single() {
		level, details = slog.LevelInfo, "skipped, "+details
	}
	r.log.Log(context.Background(), level, message, "file", file, "reason", reason)
	r.out.event(level, r.out.dim.Render(symbolSkip), r.out.dim.Render(r.name(file)), r.out.dim.Render(details))
}

func (r *reporter) failedFile(file string, err error) {
	r.failed++
	r.log.Error("Extraction failed", "file", file, "err", err)

	if r.single() {
		r.out.failure(fmt.Errorf("%s: %s", file, describe(err, file)), hintsFor(err)...)
		return
	}
	r.out.event(slog.LevelError, r.out.fail.Render(symbolFail), r.name(file), describe(err, file))
}

// fatal reports an error that stops the whole run.
func (r *reporter) fatal(err error, hints ...string) {
	r.out.clearProgress()
	r.log.Error(err.Error())
	r.out.failure(err, append(hintsFor(err), hints...)...)
}

// finish closes the report of a run over files, which went into outputDir.
func (r *reporter) finish(outputDir string, elapsed time.Duration, interrupted bool) {
	r.out.clearProgress()
	if r.total == 0 {
		return
	}

	notExtractable := r.notMotion + r.unsupported + r.moved
	processed := r.extracted + r.unchanged + notExtractable + r.failed
	skipped := r.unchanged + notExtractable
	if interrupted {
		r.log.Warn("Interrupted", "processed", processed, "files", r.total)
		r.out.event(slog.LevelWarn, r.out.warn.Render(symbolWarn), "Interrupted", fmt.Sprintf("%d of %d files processed", processed, r.total))
	}

	var lines []string
	if !r.single() {
		attrs := []any{"extracted", r.extracted, "skipped", skipped, "failed", r.failed}
		if r.unreadable > 0 {
			attrs = append(attrs, "unreadable_dirs", r.unreadable)
		}
		r.log.Info("Done", append(attrs, "bytes", r.written, "elapsed", elapsed.Round(time.Millisecond).String())...)

		symbol := r.out.dim.Render(symbolSkip)
		switch {
		case r.failed > 0 || r.unreadable > 0:
			symbol = r.out.fail.Render(symbolFail)
		case r.extracted > 0:
			symbol = r.out.ok.Render(symbolOK)
		}
		count := func(n int, what string, style lipgloss.Style) string {
			if n == 0 {
				style = r.out.dim
			}
			return style.Render(fmt.Sprintf("%d %s", n, what))
		}
		separator := r.out.dim.Render(" · ")
		lines = append(lines, "", symbol+" "+strings.Join([]string{
			count(r.extracted, "extracted", r.out.ok.Bold(true)),
			count(skipped, "skipped", r.out.warn),
			count(r.failed, "failed", r.out.fail.Bold(true)),
		}, separator)+r.out.dim.Render("  in "+formatDuration(elapsed)))

		if r.extracted > 0 {
			var outputs []string
			if r.photos > 0 {
				outputs = append(outputs, plural(r.photos, "photo"))
			}
			if r.videos > 0 {
				outputs = append(outputs, plural(r.videos, "video"))
			}
			destination := "next to the originals"
			if outputDir != "" {
				destination = "to " + outputDir
			}
			lines = append(lines, fmt.Sprintf("  %s, %s, written %s",
				strings.Join(outputs, " and "), formatSize(r.written), destination))
		}
		var reasons []string
		if r.notMotion > 0 {
			reasons = append(reasons, plural(r.notMotion, "file")+" without a video")
		}
		if r.unsupported > 0 {
			reasons = append(reasons, plural(r.unsupported, "file")+" of an unsupported type")
		}
		if r.moved > 0 {
			reasons = append(reasons, plural(r.moved, "original")+" of an earlier extraction")
		}
		if len(reasons) > 0 && !r.out.enabled(slog.LevelDebug) {
			lines = append(lines, "  "+r.out.dim.Render(strings.Join(reasons, " and ")+" skipped, list them with --verbose"))
		}
		if r.unreadable > 0 {
			dirs := "1 directory"
			if r.unreadable > 1 {
				dirs = fmt.Sprintf("%d directories", r.unreadable)
			}
			lines = append(lines, "  "+dirs+" could not be read")
		}
	}
	if len(lines) > 0 {
		r.out.print(slog.LevelInfo, lines...)
	}
	if r.kept > 0 {
		r.out.print(slog.LevelWarn, "  "+r.out.dim.Render(plural(r.kept, "existing output")+" kept, overwrite with --force"))
	}
}

// name pads file so that the details of consecutive lines line up.
func (r *reporter) name(file string) string {
	return padRight(file, r.nameWidth)
}

// describe is err's message without the path of file, which is printed next
// to it anyway.
func describe(err error, file string) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) && pathErr.Path == file {
		return strings.Replace(err.Error(), pathErr.Error(), pathErr.Err.Error(), 1)
	}
	return err.Error()
}

// hintsFor suggests what to do about err.
func hintsFor(err error) []string {
	switch {
	case errors.Is(err, extractor.ErrNotMotionPhoto):
		return []string{"Only Samsung motion photos, which embed a video, can be extracted."}
	case errors.Is(err, extractor.ErrUnsupportedExtension):
		return []string{"Supported formats: .jpg, .jpeg, .heic"}
	case errors.Is(err, extractor.ErrNothingToExtract):
		return []string{"Enable --extract-photo or --extract-video."}
	default:
		return nil
	}
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value, units := float64(bytes)/unit, "KMGT"
	for value >= unit && len(units) > 1 {
		value, units = value/unit, units[1:]
	}
	return fmt.Sprintf("%.1f %cB", value, units[0])
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return "<1ms"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return d.Round(time.Second).String()
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
