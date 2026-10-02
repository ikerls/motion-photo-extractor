package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
)

type Options struct {
	// Console receives log output. Nil disables console logging.
	Console io.Writer
	// File is appended to when set. Its directory is created if missing.
	File string
	// Level is one of debug, info, warn, error.
	Level string
}

func getCustomStyles() *log.Styles {
	styles := log.DefaultStyles()

	styles.Levels = map[log.Level]lipgloss.Style{
		log.DebugLevel: lipgloss.NewStyle().
			SetString("DEBUG").
			Padding(0, 1).
			Background(lipgloss.Color("8")).
			Foreground(lipgloss.Color("15")),
		log.InfoLevel: lipgloss.NewStyle().
			SetString("INFO").
			Padding(0, 1).
			Background(lipgloss.Color("39")).
			Foreground(lipgloss.Color("15")),
		log.WarnLevel: lipgloss.NewStyle().
			SetString("WARN").
			Padding(0, 1).
			Background(lipgloss.Color("220")).
			Foreground(lipgloss.Color("0")),
		log.ErrorLevel: lipgloss.NewStyle().
			SetString("ERROR").
			Padding(0, 1).
			Background(lipgloss.Color("196")).
			Foreground(lipgloss.Color("15")),
		log.FatalLevel: lipgloss.NewStyle().
			SetString("FATAL").
			Padding(0, 1).
			Background(lipgloss.Color("88")).
			Foreground(lipgloss.Color("15")),
	}

	styles.Timestamp = lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
	styles.Message = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))

	return styles
}

// New builds a logger writing to the console and/or a file. The returned
// function closes the log file, if any, and must be called when done.
func New(opts Options) (*slog.Logger, func() error, error) {
	level, err := parseLogLevel(opts.Level)
	if err != nil {
		return nil, nil, err
	}

	var writers []io.Writer
	closeFn := func() error { return nil }

	if opts.Console != nil {
		writers = append(writers, opts.Console)
	}

	if opts.File != "" {
		if err := os.MkdirAll(filepath.Dir(opts.File), 0755); err != nil {
			return nil, nil, fmt.Errorf("failed to create log directory: %w", err)
		}

		f, err := os.OpenFile(opts.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open log file: %w", err)
		}
		writers = append(writers, f)
		closeFn = f.Close
	}

	var writer io.Writer
	switch len(writers) {
	case 0:
		writer = io.Discard
	case 1:
		writer = writers[0]
	default:
		writer = io.MultiWriter(writers...)
	}

	handler := log.NewWithOptions(writer, log.Options{
		Level:           level,
		ReportTimestamp: true,
	})
	handler.SetStyles(getCustomStyles())

	return slog.New(handler), closeFn, nil
}

func parseLogLevel(level string) (log.Level, error) {
	switch level {
	case "debug":
		return log.DebugLevel, nil
	case "info":
		return log.InfoLevel, nil
	case "warn":
		return log.WarnLevel, nil
	case "error":
		return log.ErrorLevel, nil
	default:
		return log.InfoLevel, fmt.Errorf("invalid log level: %s", level)
	}
}
