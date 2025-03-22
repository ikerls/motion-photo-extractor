package logger

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"io"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
)

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

func Setup(logFile string, noConsole bool, logLevel string) error {
	var writers []io.Writer

	if !noConsole {
		writers = append(writers, os.Stdout)
	}

	if logFile != "" {
		if err := os.MkdirAll(filepath.Dir(logFile), 0755); err != nil {
			return fmt.Errorf("failed to create log directory: %w", err)
		}

		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}
		writers = append(writers, f)
	}

	var writer io.Writer
	if len(writers) > 1 {
		writer = io.MultiWriter(writers...)
	} else if len(writers) == 1 {
		writer = writers[0]
	} else {
		writer = io.Discard
	}

	level, err := parseLogLevel(logLevel)
	if err != nil {
		return err
	}

	logger := log.NewWithOptions(writer, log.Options{
		Level:           level,
		ReportTimestamp: true,
		ReportCaller:    false,
	})
	logger.SetStyles(getCustomStyles())

	log.SetDefault(logger)
	return nil
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
