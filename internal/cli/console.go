package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

const (
	symbolOK   = "✓"
	symbolWarn = "!"
	symbolFail = "✗"
	symbolSkip = "–"

	progressWidth    = 24
	progressInterval = 50 * time.Millisecond
)

// console is the human-readable output: one line per event and, on a
// terminal, a progress line that stays at the bottom while files are processed.
type console struct {
	w     io.Writer
	level slog.Level

	// fd is the terminal behind w, used for its width. Without a terminal
	// there is no progress line.
	fd       uintptr
	terminal bool

	// progress renders the line shown at the bottom, nil if there is none.
	progress func() string
	drawn    time.Time

	ok, warn, fail, dim, bold, accent lipgloss.Style
}

// newConsole returns a console that prints events at level or above to w.
// Colors are used when w is a terminal that supports them and NO_COLOR is
// not set.
func newConsole(w io.Writer, level slog.Level) *console {
	r := lipgloss.NewRenderer(w)
	c := &console{
		w:      w,
		level:  level,
		ok:     r.NewStyle().Foreground(lipgloss.Color("2")),
		warn:   r.NewStyle().Foreground(lipgloss.Color("3")),
		fail:   r.NewStyle().Foreground(lipgloss.Color("1")),
		dim:    r.NewStyle().Faint(true),
		bold:   r.NewStyle().Bold(true),
		accent: r.NewStyle().Foreground(lipgloss.Color("6")),
	}
	if isTerminal(w) {
		c.fd, c.terminal = w.(*os.File).Fd(), true
	}
	return c
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(f.Fd())
}

func (c *console) enabled(level slog.Level) bool {
	return level >= c.level
}

// print writes lines above the progress line.
func (c *console) print(level slog.Level, lines ...string) {
	if !c.enabled(level) {
		return
	}

	var b strings.Builder
	if c.progress != nil {
		b.WriteString(ansi.EraseEntireLine + "\r")
	}
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if c.progress != nil {
		b.WriteString(c.progress())
		c.drawn = time.Now()
	}
	io.WriteString(c.w, b.String())
}

// event prints a line made of a symbol, a subject and details about it.
func (c *console) event(level slog.Level, symbol, subject, details string) {
	line := symbol + " " + subject
	if details != "" {
		line += "  " + details
	}
	c.print(level, line)
}

// failure prints an error followed by hints on what to do about it.
func (c *console) failure(err error, hints ...string) {
	lines := []string{c.fail.Render(symbolFail) + " " + err.Error()}
	for _, hint := range hints {
		lines = append(lines, "  "+c.dim.Render(hint))
	}
	c.print(slog.LevelError, lines...)
}

// showProgress replaces the progress line with a bar for done out of total
// followed by label. Redraws are rate limited, since most files take well
// under a millisecond.
func (c *console) showProgress(done, total int, label string) {
	if !c.terminal || total == 0 {
		return
	}

	c.progress = func() string {
		filled := progressWidth * done / total
		bar := c.accent.Render(strings.Repeat("━", filled)) +
			c.dim.Render(strings.Repeat("─", progressWidth-filled))
		return c.fit(fmt.Sprintf("%s %s %s", bar, c.bold.Render(fmt.Sprintf("%d/%d", done, total)), c.dim.Render(label)))
	}
	if time.Since(c.drawn) >= progressInterval {
		c.drawProgress()
	}
}

// showStatus replaces the progress line with a message.
func (c *console) showStatus(message string) {
	if !c.terminal {
		return
	}
	c.progress = func() string { return c.fit(c.dim.Render(message)) }
	c.drawProgress()
}

func (c *console) drawProgress() {
	c.drawn = time.Now()
	io.WriteString(c.w, ansi.EraseEntireLine+"\r"+c.progress())
}

func (c *console) clearProgress() {
	if c.progress == nil {
		return
	}
	c.progress = nil
	io.WriteString(c.w, ansi.EraseEntireLine+"\r")
}

// fit shortens line to the width of the terminal. A progress line that wraps
// could not be erased again.
func (c *console) fit(line string) string {
	width, _, err := term.GetSize(c.fd)
	if err != nil || width < 2 {
		return line
	}
	return ansi.Truncate(line, width-1, "…")
}

// padRight pads s, which may contain escape sequences, to width columns.
func padRight(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-lipgloss.Width(s)))
}
