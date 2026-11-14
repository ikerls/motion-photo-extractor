// Package logger builds the structured logger behind --log-file and the
// non-interactive console output.
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
)

type Options struct {
	// Console receives log output. Nil disables console logging.
	Console io.Writer
	// File is appended to when set. Its directory is created if missing.
	File string
	// Level is the lowest level that is logged.
	Level slog.Level
	// JSON writes one JSON object per line instead of text.
	JSON bool
}

// New builds a logger writing to the console and/or a file. The returned
// function closes the log file, if any, and must be called when done.
func New(opts Options) (*slog.Logger, func() error, error) {
	var handlers []slog.Handler
	closeFn := func() error { return nil }

	if opts.Console != nil {
		handlers = append(handlers, newHandler(opts.Console, opts))
	}

	if opts.File != "" {
		if err := os.MkdirAll(filepath.Dir(opts.File), 0755); err != nil {
			return nil, nil, fmt.Errorf("failed to create log directory: %w", err)
		}

		f, err := os.OpenFile(opts.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open log file: %w", err)
		}
		handlers = append(handlers, newHandler(f, opts))
		closeFn = f.Close
	}

	switch len(handlers) {
	case 0:
		return slog.New(slog.DiscardHandler), closeFn, nil
	case 1:
		return slog.New(handlers[0]), closeFn, nil
	default:
		return slog.New(slog.NewMultiHandler(handlers...)), closeFn, nil
	}
}

// newHandler gives each destination its own handler, so that a terminal gets
// colors while a file written alongside it stays plain.
func newHandler(w io.Writer, opts Options) slog.Handler {
	logOpts := log.Options{
		Level:           log.Level(opts.Level),
		ReportTimestamp: true,
		TimeFormat:      time.DateTime,
	}
	if opts.JSON {
		logOpts.Formatter = log.JSONFormatter
		logOpts.TimeFormat = time.RFC3339
	}

	handler := log.NewWithOptions(w, logOpts)
	handler.SetStyles(styles())
	return handler
}

// styles uses the terminal's own 16 colors, which stay readable on both dark
// and light backgrounds.
func styles() *log.Styles {
	styles := log.DefaultStyles()

	level := func(name, color string) lipgloss.Style {
		return lipgloss.NewStyle().SetString(name).Bold(true).Foreground(lipgloss.Color(color))
	}
	styles.Levels = map[log.Level]lipgloss.Style{
		log.DebugLevel: level("DEBUG", "8"),
		log.InfoLevel:  level("INFO ", "4"),
		log.WarnLevel:  level("WARN ", "3"),
		log.ErrorLevel: level("ERROR", "1"),
		log.FatalLevel: level("FATAL", "5"),
	}
	styles.Timestamp = lipgloss.NewStyle().Faint(true)
	styles.Key = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	return styles
}

// ParseLevel converts a --log-level value.
func ParseLevel(level string) (slog.Level, error) {
	switch level {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("invalid log level %q: use debug, info, warn or error", level)
	}
}
