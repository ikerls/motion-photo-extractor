package cli

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

const (
	appName   = "go-motion-photo"
	usageLine = "Usage: " + appName + " [options] <file|directory|pattern>..."
	helpHint  = "Run " + appName + " --help for more information."
)

// helpSection is a titled table of terms and their descriptions, optionally
// followed by notes. A description may span several lines.
type helpSection struct {
	title string
	rows  [][2]string
	notes []string
}

var helpSections = []helpSection{
	{
		title: "Inputs",
		rows: [][2]string{
			{"photo.jpg", "A motion photo: .jpg, .jpeg or .heic"},
			{"./photos", "A directory, searched recursively"},
			{"'photos/*.jpg'", "A glob pattern"},
			{`'/IMG_\d{4}\.jpg/'`, "A regular expression between slashes, matched against\nthe file names in the current directory"},
		},
		notes: []string{
			"When more than one file is processed, those that are not motion photos or",
			"have another extension are skipped.",
		},
	},
	{
		title: "Options",
		rows: [][2]string{
			{"-i, --input <path>", "Input to process, same as passing it as an argument.\nMay be repeated"},
			{"-o, --output <dir>", `Directory to save extracted files (default: ".")`},
			{"-f, --force", "Overwrite existing output files"},
			{"    --rename-orig", "Give extracted files the original's name and move the\noriginal to <name>_original, instead of adding suffixes"},
			{"    --delete-orig", "Delete the original after a successful extraction"},
			{"    --extract-photo", "Extract the photo component (default: true)"},
			{"    --extract-video", "Extract the video component (default: true)"},
			{"    --recover", "Clean up what a run that was killed left in the output\ndirectory, before extracting. Needs no input"},
		},
		notes: []string{
			"A run that is killed outright may leave files named <output>.<8 hex digits>.part",
			"and .bak, which the next run points out. --recover removes the .part files and",
			"puts a .bak file back if its output is missing and it is the only one for it;",
			"any other is kept and reported. Not while another run writes to that directory.",
		},
	},
	{
		title: "Logging",
		rows: [][2]string{
			{"-v, --verbose", "Show skipped files and details, same as --log-level debug"},
			{"-q, --quiet", "Show only warnings and errors, same as --log-level warn"},
			{"    --log-level <level>", `debug, info, warn or error (default: "info")`},
			{"    --log-format <format>", "Console output: auto, pretty, text or json (default: \"auto\")\nauto is pretty on a terminal and text when redirected"},
			{"    --log-file <path>", "Also write timestamped logs to a file"},
			{"    --no-console-log", "Write nothing to the console"},
		},
		notes: []string{
			"Console output goes to standard error. Colors are disabled when NO_COLOR is set.",
		},
	},
	{
		title: "Configuration",
		rows: [][2]string{
			{"    --config <path>", "Config file, YAML or JSON. By default go-motion-photo.yaml,\n.yml or .json is read from the current directory or\n$HOME/.config/go-motion-photo"},
		},
		notes: []string{
			"Every config key can also be set through the environment,",
			"e.g. GO_MOTION_PHOTO_OUTPUT, GO_MOTION_PHOTO_LOG_LEVEL",
		},
	},
	{
		title: "Other",
		rows: [][2]string{
			{"-V, --version", "Print version and exit"},
			{"-h, --help", "Show this help"},
		},
		notes: []string{
			"The exit status is 1 if any file failed or a directory could not be read,",
			"and 130 if interrupted with Ctrl+C, kill or by closing the terminal, which",
			"stops the run after the file being processed.",
		},
	},
}

// helpExamples pairs what an invocation does with the invocation.
var helpExamples = [][2]string{
	{"Extract the photo and the video of one file", "photo.jpg"},
	{"Extract every motion photo of a directory somewhere else", "./photos -o ./extracted"},
	{"Process several files and patterns", `a.jpg b.heic 'holidays/*.jpg'`},
	{"Process files matching a regular expression", `'/IMG_\d{4}\.jpg/'`},
	{"Keep the original names, moving the originals aside", "photo.jpg --rename-orig"},
	{"Extract only the video, replacing earlier outputs", "photo.heic --extract-photo=false --force"},
}

func printHelp(w io.Writer) {
	c := newConsole(w, slog.LevelInfo)

	termWidth := 0
	for _, section := range helpSections {
		for _, row := range section.rows {
			termWidth = max(termWidth, len(row[0]))
		}
	}

	usage, rest, _ := strings.Cut(usageLine, " ")
	lines := []string{
		c.bold.Render(appName) + " extracts the photo and the video from Samsung motion photos.",
		"",
		c.bold.Render(usage) + " " + rest,
	}

	for _, section := range helpSections {
		lines = append(lines, "", c.bold.Render(section.title))
		for _, row := range section.rows {
			term := c.accent.Render(fmt.Sprintf("%-*s", termWidth, row[0]))
			for i, text := range strings.Split(row[1], "\n") {
				if i > 0 {
					term = strings.Repeat(" ", termWidth)
				}
				lines = append(lines, "  "+term+"  "+c.dimDefault(text))
			}
		}
		if len(section.notes) > 0 {
			lines = append(lines, "")
		}
		for _, note := range section.notes {
			lines = append(lines, "  "+c.dim.Render(note))
		}
	}

	lines = append(lines, "", c.bold.Render("Examples"))
	for _, example := range helpExamples {
		lines = append(lines, "  "+c.dim.Render("# "+example[0]), "  "+appName+" "+c.accent.Render(example[1]))
	}

	fmt.Fprintln(w, strings.Join(lines, "\n"))
}

// dimDefault fades the "(default: ...)" a description ends with.
func (c *console) dimDefault(text string) string {
	i := strings.LastIndex(text, " (default: ")
	if i < 0 {
		return text
	}
	return text[:i] + c.dim.Render(text[i:])
}
