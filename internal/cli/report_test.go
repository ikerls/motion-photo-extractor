package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runPretty runs the command with the human-readable console in dir and
// returns the exit status and what was written to stderr.
func runPretty(t *testing.T, dir string, args ...string) (int, string) {
	t.Helper()
	t.Chdir(dir)
	t.Setenv("HOME", dir)

	var stdout, stderr bytes.Buffer
	status := Run(append([]string{"--log-format", "pretty"}, args...), &stdout, &stderr, "test")
	return status, stderr.String()
}

func TestPrettySingleFileShowsOutputs(t *testing.T) {
	dir := t.TempDir()
	writeMotionPhotoFixture(t, filepath.Join(dir, "single.jpg"))

	status, got := runPretty(t, dir, "single.jpg", "--output", "out", "--rename-orig")
	want := strings.Join([]string{
		"✓ single.jpg",
		"  ├─ photo     out/single.jpg  ",
		"  ├─ video     out/single.mp4  24 B",
		"  └─ original  moved to out/single_original.jpg",
	}, "\n")
	if status != 0 || !containsLines(got, want) {
		t.Fatalf("status = %d, output:\n%s\nwant lines starting with:\n%s", status, got, want)
	}
}

func TestPrettySingleFileExplainsFailure(t *testing.T) {
	dir := t.TempDir()
	writeFalsePositiveFixture(t, filepath.Join(dir, "plain.jpg"))

	status, got := runPretty(t, dir, "plain.jpg")
	if status != 1 || !strings.HasPrefix(got, "✗ plain.jpg: not a motion photo") || !strings.Contains(got, "\n  Only Samsung motion photos") {
		t.Fatalf("status = %d, output:\n%s", status, got)
	}

	status, got = runPretty(t, dir, "missing.jpg")
	if want := "✗ missing.jpg: no such file or directory\n"; status != 1 || got != want {
		t.Fatalf("status = %d, output = %q, want %q", status, got, want)
	}
}

func TestPrettyBatchSummarizes(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "in"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeMotionPhotoFixture(t, filepath.Join(dir, "in", "a.jpg"))
	writeMotionPhotoFixture(t, filepath.Join(dir, "in", "b.jpg"))
	writeFalsePositiveFixture(t, filepath.Join(dir, "in", "plain.jpg"))

	status, got := runPretty(t, dir, "in", "missing.jpg", "--output", "out")
	for _, want := range []string{
		"› in  3 files\n",
		"✓ in/a.jpg      photo ",
		"✓ in/b.jpg      photo ",
		"✗ missing.jpg   no such file or directory\n",
		"\n\n✗ 2 extracted · 1 skipped · 1 failed  in ",
		"  2 photos and 2 videos, ",
		", written to out\n",
		"  1 file without a video skipped, list them with --verbose\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q", want)
		}
	}
	if status != 1 || strings.Contains(got, "plain.jpg") || strings.Contains(got, "\x1b") {
		t.Fatalf("status = %d, output:\n%s", status, got)
	}
}

func TestPrettyVerboseListsSkippedFiles(t *testing.T) {
	dir := t.TempDir()
	writeMotionPhotoFixture(t, filepath.Join(dir, "a.jpg"))
	writeFalsePositiveFixture(t, filepath.Join(dir, "plain.jpg"))

	status, got := runPretty(t, dir, ".", "--output", "out", "--verbose")
	if status != 0 || !strings.Contains(got, "– plain.jpg  not a motion photo") || !strings.Contains(got, "split by ") {
		t.Fatalf("status = %d, output:\n%s", status, got)
	}
	if strings.Contains(got, "list them with --verbose") {
		t.Fatalf("output suggests --verbose although it is set:\n%s", got)
	}
}

func TestPrettyReportsKeptOutputs(t *testing.T) {
	dir := t.TempDir()
	writeMotionPhotoFixture(t, filepath.Join(dir, "a.jpg"))
	writeMotionPhotoFixture(t, filepath.Join(dir, "b.jpg"))
	if status, got := runPretty(t, dir, "a.jpg", "b.jpg", "--output", "out"); status != 0 {
		t.Fatalf("first run: status = %d, output:\n%s", status, got)
	}

	// Existing outputs are warnings, so they show even when quiet.
	status, got := runPretty(t, dir, "a.jpg", "b.jpg", "--output", "out", "--quiet")
	want := "! a.jpg  photo exists, kept · video exists, kept\n" +
		"! b.jpg  photo exists, kept · video exists, kept\n" +
		"  4 existing outputs kept, overwrite with --force\n"
	if status != 0 || got != want {
		t.Fatalf("status = %d, output = %q, want %q", status, got, want)
	}

	status, got = runPretty(t, dir, "a.jpg", "b.jpg", "--output", "out", "--quiet", "--force")
	if status != 0 || got != "" {
		t.Fatalf("with --force: status = %d, output = %q, want none", status, got)
	}
}

func TestPrettyReportsOriginalsKeptDespiteDeleteOrig(t *testing.T) {
	dir := t.TempDir()
	writeMotionPhotoFixture(t, filepath.Join(dir, "a.jpg"))
	if err := os.Mkdir(filepath.Join(dir, "out"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "out", "a_video.mp4"), []byte("existing"), 0644); err != nil {
		t.Fatalf("write existing video: %v", err)
	}

	status, got := runPretty(t, dir, "a.jpg", "--output", "out", "--delete-orig")
	want := strings.Join([]string{
		"! a.jpg",
		"  ├─ photo     out/a_photo.jpg  ",
		"  ├─ video     out/a_video.mp4  already exists, kept",
		"  └─ original  kept, not every output was written",
	}, "\n")
	if status != 0 || !containsLines(got, want) {
		t.Fatalf("status = %d, output:\n%s\nwant lines starting with:\n%s", status, got, want)
	}
	assertFileExists(t, filepath.Join(dir, "a.jpg"))
}

func TestPrettyExplainsCollidingOutputs(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(dir, "in", sub), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		writeMotionPhotoFixture(t, filepath.Join(dir, "in", sub, "IMG.jpg"))
	}

	status, got := runPretty(t, dir, "in", "--output", "out", "--delete-orig")
	want := "✗ in/b/IMG.jpg  out/IMG_photo.jpg is already the output of in/a/IMG.jpg, extract this file to another directory\n"
	if status != 1 || !strings.Contains(got, want) || !strings.Contains(got, "1 extracted · 0 skipped · 1 failed") {
		t.Fatalf("status = %d, output:\n%s", status, got)
	}
	assertFileExists(t, filepath.Join(dir, "in", "b", "IMG.jpg"))
}

func TestPrettyWarnsAboutIgnoredConfigFile(t *testing.T) {
	dir := t.TempDir()
	writeMotionPhotoFixture(t, filepath.Join(dir, "a.jpg"))
	if err := os.WriteFile(filepath.Join(dir, "go-motion-photo.toml"), []byte("output = \"elsewhere\"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	status, got := runPretty(t, dir, "a.jpg", "--quiet")
	if want := "! go-motion-photo.toml  config file ignored, only YAML and JSON are supported\n"; status != 0 || got != want {
		t.Fatalf("status = %d, output = %q, want %q", status, got, want)
	}
	assertFileExists(t, filepath.Join(dir, "a_video.mp4"))
}

func TestPrettyWarnsWhenNothingMatches(t *testing.T) {
	status, got := runPretty(t, t.TempDir(), "*.heic")
	if want := "! *.heic  no supported files found\n"; status != 0 || got != want {
		t.Fatalf("status = %d, output = %q, want %q", status, got, want)
	}
}

func TestRunExplainsUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "no input", want: []string{"✗ no input specified\n", "  " + usageLine + "\n", "  " + helpHint + "\n"}},
		{name: "typo", args: []string{"--forse", "a.jpg"}, want: []string{"✗ unknown flag: --forse, did you mean --force?\n", helpHint}},
		{name: "log format", args: []string{"--log-format", "xml", "a.jpg"}, want: []string{`✗ invalid log format "xml"`}},
		{name: "nothing to extract", args: []string{"--log-format", "pretty", "--extract-photo=false", "--extract-video=false", "a.jpg"},
			want: []string{"✗ nothing to extract", "  Enable --extract-photo or --extract-video.\n"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("HOME", dir)

			var stdout, stderr bytes.Buffer
			status := Run(tc.args, &stdout, &stderr, "test")
			for _, want := range tc.want {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr = %q, want to contain %q", &stderr, want)
				}
			}
			if status != 1 || stdout.Len() != 0 {
				t.Fatalf("status = %d, stdout = %q", status, &stdout)
			}
		})
	}
}

func TestRunLogsStructuredOutputWhenNotOnATerminal(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	writeMotionPhotoFixture(t, filepath.Join(dir, "a.jpg"))

	var stdout, stderr bytes.Buffer
	if status := Run([]string{"a.jpg", "--output", "out", "--log-file", "run.log"}, &stdout, &stderr, "test"); status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, &stderr)
	}
	logged, err := os.ReadFile(filepath.Join(dir, "run.log"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	want := "INFO  Extracted file=a.jpg photo=out/a_photo.jpg video=out/a_video.mp4\n"
	if !strings.HasSuffix(stderr.String(), want) || stderr.String() != string(logged) {
		t.Fatalf("stderr = %q, log file = %q, want both to end with %q", &stderr, logged, want)
	}

	stderr.Reset()
	if status := Run([]string{"a.jpg", "--output", "out", "--force", "--log-format", "json"}, &stdout, &stderr, "test"); status != 0 {
		t.Fatalf("json: status = %d, stderr = %q", status, &stderr)
	}
	var line map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &line); err != nil || line["msg"] != "Extracted" || line["video"] != "out/a_video.mp4" {
		t.Fatalf("json: stderr = %q, err = %v", &stderr, err)
	}
}

func TestRunNoConsoleLogWritesOnlyTheLogFile(t *testing.T) {
	dir := t.TempDir()
	writeMotionPhotoFixture(t, filepath.Join(dir, "a.jpg"))

	status, got := runPretty(t, dir, "a.jpg", "--output", "out", "--no-console-log", "--log-file", "run.log")
	if status != 0 || got != "" {
		t.Fatalf("status = %d, stderr = %q, want none", status, got)
	}
	if logged, err := os.ReadFile(filepath.Join(dir, "run.log")); err != nil || !strings.Contains(string(logged), "Extracted file=a.jpg") {
		t.Fatalf("log file = %q, err = %v", logged, err)
	}
}

func TestProcessStopsWhenInterrupted(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "a.jpg")
	writeMotionPhotoFixture(t, input)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := process(ctx, testConfig(filepath.Join(dir, "out"), input), discardReporter()); !errors.Is(err, errInterrupted) {
		t.Fatalf("process() error = %v, want errInterrupted", err)
	}
	assertFileDoesNotExist(t, filepath.Join(dir, "out", "a_video.mp4"))
}

func TestFormatSize(t *testing.T) {
	tests := map[int64]string{
		0:                "0 B",
		1023:             "1023 B",
		1024:             "1.0 KB",
		1536:             "1.5 KB",
		5 * 1024 * 1024:  "5.0 MB",
		3 << 30:          "3.0 GB",
		2 << 40:          "2.0 TB",
		2048 * (1 << 40): "2048.0 TB",
	}
	for bytes, want := range tests {
		if got := formatSize(bytes); got != want {
			t.Errorf("formatSize(%d) = %q, want %q", bytes, got, want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := map[time.Duration]string{
		200 * time.Microsecond:  "<1ms",
		340 * time.Millisecond:  "340ms",
		1234 * time.Millisecond: "1.2s",
		125 * time.Second:       "2m5s",
	}
	for d, want := range tests {
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

// containsLines reports whether got has consecutive lines starting with the
// lines of want.
func containsLines(got, want string) bool {
	gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want, "\n")
	for start := range gotLines {
		if start+len(wantLines) > len(gotLines) {
			break
		}
		match := true
		for i, line := range wantLines {
			match = match && strings.HasPrefix(gotLines[start+i], line)
		}
		if match {
			return true
		}
	}
	return false
}
