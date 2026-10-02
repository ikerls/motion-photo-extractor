package cli

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikerls/motion-photo-extractor/internal/config"
)

func TestProcessSingleFile(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "single.jpg")
	output := filepath.Join(tempDir, "out")
	writeMotionPhotoFixture(t, input)

	if err := process(t.Context(), testConfig(output, input), discardReporter()); err != nil {
		t.Fatalf("process() error = %v", err)
	}

	assertFileExists(t, filepath.Join(output, "single_photo.jpg"))
	assertFileExists(t, filepath.Join(output, "single_video.mp4"))
}

func TestProcessSingleFileWithoutVideoExtraction(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "single.jpg")
	output := filepath.Join(tempDir, "out")
	writeMotionPhotoFixture(t, input)

	cfg := testConfig(output, input)
	cfg.ExtractVideo = false

	if err := process(t.Context(), cfg, discardReporter()); err != nil {
		t.Fatalf("process() error = %v", err)
	}

	assertFileExists(t, filepath.Join(output, "single_photo.jpg"))
	assertFileDoesNotExist(t, filepath.Join(output, "single_video.mp4"))
}

func TestProcessSingleFileRejectsFalsePositive(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "false-positive.jpg")
	output := filepath.Join(tempDir, "out")
	writeFalsePositiveFixture(t, input)

	err := process(t.Context(), testConfig(output, input), discardReporter())
	if err == nil {
		t.Fatal("process() error = nil, want non-nil")
	}

	assertFileDoesNotExist(t, filepath.Join(output, "false-positive_photo.jpg"))
	assertFileDoesNotExist(t, filepath.Join(output, "false-positive_video.mp4"))
}

func TestProcessSeveralInputs(t *testing.T) {
	tempDir := t.TempDir()
	output := filepath.Join(tempDir, "out")
	first := filepath.Join(tempDir, "first.jpg")
	second := filepath.Join(tempDir, "second.jpg")
	writeMotionPhotoFixture(t, first)
	writeMotionPhotoFixture(t, second)

	if err := process(t.Context(), testConfig(output, first, second), discardReporter()); err != nil {
		t.Fatalf("process() error = %v", err)
	}

	assertFileExists(t, filepath.Join(output, "first_video.mp4"))
	assertFileExists(t, filepath.Join(output, "second_video.mp4"))
}

func TestProcessDirectoryProcessesSupportedFiles(t *testing.T) {
	tempDir := t.TempDir()
	inputDir := filepath.Join(tempDir, "in")
	output := filepath.Join(tempDir, "out")
	if err := os.MkdirAll(inputDir, 0755); err != nil {
		t.Fatalf("mkdir input dir: %v", err)
	}

	writeMotionPhotoFixture(t, filepath.Join(inputDir, "a.jpg"))
	writeMotionPhotoFixture(t, filepath.Join(inputDir, "b.heic"))
	if err := os.WriteFile(filepath.Join(inputDir, "skip.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write skip file: %v", err)
	}

	if err := process(t.Context(), testConfig(output, inputDir), discardReporter()); err != nil {
		t.Fatalf("process() error = %v", err)
	}

	assertFileExists(t, filepath.Join(output, "a_photo.jpg"))
	assertFileExists(t, filepath.Join(output, "a_video.mp4"))
	assertFileExists(t, filepath.Join(output, "b_photo.heic"))
	assertFileExists(t, filepath.Join(output, "b_video.mp4"))
	assertFileDoesNotExist(t, filepath.Join(output, "skip_video.mp4"))
}

func TestProcessDirectorySkipsFilesThatAreNotMotionPhotos(t *testing.T) {
	tempDir := t.TempDir()
	inputDir := filepath.Join(tempDir, "in")
	output := filepath.Join(tempDir, "out")
	if err := os.MkdirAll(inputDir, 0755); err != nil {
		t.Fatalf("mkdir input dir: %v", err)
	}

	writeMotionPhotoFixture(t, filepath.Join(inputDir, "motion.jpg"))
	writeFalsePositiveFixture(t, filepath.Join(inputDir, "plain.jpg"))

	if err := process(t.Context(), testConfig(output, inputDir), discardReporter()); err != nil {
		t.Fatalf("process() error = %v", err)
	}

	assertFileExists(t, filepath.Join(output, "motion_video.mp4"))
	assertFileDoesNotExist(t, filepath.Join(output, "plain_video.mp4"))
}

func TestProcessBatchReportsFailures(t *testing.T) {
	tempDir := t.TempDir()
	output := filepath.Join(tempDir, "out")
	good := filepath.Join(tempDir, "good.jpg")
	writeMotionPhotoFixture(t, good)

	err := process(t.Context(), testConfig(output, good, filepath.Join(tempDir, "missing.jpg")), discardReporter())
	if err == nil {
		t.Fatal("process() error = nil, want non-nil")
	}

	assertFileExists(t, filepath.Join(output, "good_video.mp4"))
}

func TestProcessGlobPattern(t *testing.T) {
	tempDir := t.TempDir()
	output := filepath.Join(tempDir, "out")
	writeMotionPhotoFixture(t, filepath.Join(tempDir, "g1.jpg"))
	writeMotionPhotoFixture(t, filepath.Join(tempDir, "g2.jpg"))
	if err := os.WriteFile(filepath.Join(tempDir, "g3.png"), []byte("not-supported"), 0644); err != nil {
		t.Fatalf("write png fixture: %v", err)
	}

	if err := process(t.Context(), testConfig(output, filepath.Join(tempDir, "g*")), discardReporter()); err != nil {
		t.Fatalf("process() error = %v", err)
	}

	assertFileExists(t, filepath.Join(output, "g1_photo.jpg"))
	assertFileExists(t, filepath.Join(output, "g1_video.mp4"))
	assertFileExists(t, filepath.Join(output, "g2_photo.jpg"))
	assertFileExists(t, filepath.Join(output, "g2_video.mp4"))
	assertFileDoesNotExist(t, filepath.Join(output, "g3_video.mp4"))
}

func TestProcessRegexPattern(t *testing.T) {
	tempDir := t.TempDir()
	output := filepath.Join(tempDir, "out")
	writeMotionPhotoFixture(t, filepath.Join(tempDir, "IMG_0001.jpg"))
	writeMotionPhotoFixture(t, filepath.Join(tempDir, "IMG_0002.jpg"))
	writeMotionPhotoFixture(t, filepath.Join(tempDir, "OTHER_0003.jpg"))

	t.Chdir(tempDir)
	if err := process(t.Context(), testConfig(output, `/IMG_\d{4}\.jpg/`), discardReporter()); err != nil {
		t.Fatalf("process() error = %v", err)
	}

	assertFileExists(t, filepath.Join(output, "IMG_0001_photo.jpg"))
	assertFileExists(t, filepath.Join(output, "IMG_0001_video.mp4"))
	assertFileExists(t, filepath.Join(output, "IMG_0002_photo.jpg"))
	assertFileExists(t, filepath.Join(output, "IMG_0002_video.mp4"))
	assertFileDoesNotExist(t, filepath.Join(output, "OTHER_0003_video.mp4"))
}

func TestProcessInvalidRegexReturnsError(t *testing.T) {
	err := process(t.Context(), testConfig(t.TempDir(), `/[unterminated/`), discardReporter())
	if err == nil {
		t.Fatal("process() error = nil, want non-nil")
	}
}

func TestProcessInvalidGlobReturnsError(t *testing.T) {
	err := process(t.Context(), testConfig(t.TempDir(), "["), discardReporter())
	if err == nil {
		t.Fatal("process() error = nil, want non-nil")
	}
}

func TestRunExitStatus(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)
	t.Setenv("HOME", tempDir)
	motion := filepath.Join(tempDir, "motion.jpg")
	plain := filepath.Join(tempDir, "plain.jpg")
	writeMotionPhotoFixture(t, motion)
	writeFalsePositiveFixture(t, plain)

	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "success", args: []string{"--output", filepath.Join(tempDir, "out"), motion}, want: 0},
		{name: "not a motion photo", args: []string{plain}, want: 1},
		{name: "no input", args: nil, want: 1},
		{name: "unknown flag", args: []string{"--nope"}, want: 1},
		{name: "invalid log level", args: []string{"--log-level", "loud", motion}, want: 1},
		{name: "help", args: []string{"--help"}, want: 0},
		{name: "version", args: []string{"--version"}, want: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := Run(tc.args, &stdout, &stderr, "test"); got != tc.want {
				t.Fatalf("Run(%q) = %d, want %d\nstdout: %s\nstderr: %s", tc.args, got, tc.want, &stdout, &stderr)
			}
		})
	}
}

func TestRunPrintsHelpAndVersionToStdout(t *testing.T) {
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	Run([]string{"--help"}, &stdout, &stderr, "1.2.3")
	if !strings.Contains(stdout.String(), "Usage: go-motion-photo") || stderr.Len() != 0 {
		t.Fatalf("--help: stdout = %q, stderr = %q", &stdout, &stderr)
	}

	stdout.Reset()
	Run([]string{"--version"}, &stdout, &stderr, "1.2.3")
	if got := stdout.String(); got != "go-motion-photo 1.2.3\n" {
		t.Fatalf("--version: stdout = %q", got)
	}
}

func testConfig(output string, inputs ...string) *config.Config {
	return &config.Config{
		Inputs:       inputs,
		OutputDir:    output,
		ExtractPhoto: true,
		ExtractVideo: true,
	}
}

func discardReporter() *reporter {
	return &reporter{log: slog.New(slog.DiscardHandler), out: newConsole(io.Discard, slog.LevelInfo)}
}

func writeMotionPhotoFixture(t *testing.T, path string) {
	t.Helper()
	data := buildMotionPhotoFixture()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func writeFalsePositiveFixture(t *testing.T, path string) {
	t.Helper()
	data := buildFalsePositiveFixture()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write false positive fixture %s: %v", path, err)
	}
}

func buildMotionPhotoFixture() []byte {
	mp4Data := []byte{
		0x00, 0x00, 0x00, 0x18,
		'f', 't', 'y', 'p',
		'm', 'p', '4', '2',
		0x00, 0x00, 0x00, 0x00,
		'i', 's', 'o', 'm',
		'm', 'p', '4', '2',
	}

	xmp := []byte(fmt.Sprintf(`<x:xmpmeta><rdf:RDF><rdf:Description `+
		`xmlns:GCamera="http://ns.google.com/photos/1.0/camera/" `+
		`xmlns:Container="http://ns.google.com/photos/1.0/container/" `+
		`xmlns:Item="http://ns.google.com/photos/1.0/container/item/" `+
		`GCamera:MotionPhoto="1" `+
		`GCamera:MotionPhotoVersion="1" `+
		`GCamera:MotionPhotoPresentationTimestampUs="123456" `+
		`GCamera:MotionPhotoOffset="%d">`+
		`<Container:Directory><rdf:Seq>`+
		`<rdf:li rdf:parseType="Resource"><Container:Item Item:Mime="image/jpeg" Item:Semantic="Primary" Item:Length="0" Item:Padding="5"/></rdf:li>`+
		`<rdf:li rdf:parseType="Resource"><Container:Item Item:Mime="video/mp4" Item:Semantic="MotionPhoto" Item:Length="%d" Item:Padding="0"/></rdf:li>`+
		`</rdf:Seq></Container:Directory></rdf:Description></rdf:RDF></x:xmpmeta>`, len(mp4Data), len(mp4Data)))

	jpegData := append([]byte{0xFF, 0xD8}, xmp...)
	jpegData = append(jpegData, []byte("stray-mpvd-inside-jpeg")...)
	jpegData = append(jpegData, 0xFF, 0xD9)

	data := append([]byte{}, jpegData...)
	data = append(data, []byte("MotionPhoto_Data")...)
	data = append(data, mp4Data...)
	return data
}

func buildFalsePositiveFixture() []byte {
	jpegData := append([]byte{0xFF, 0xD8}, []byte("plain-jpeg-data")...)
	jpegData = append(jpegData, 0xFF, 0xD9)

	data := append([]byte{}, jpegData...)
	data = append(data, bytes.Repeat([]byte{0x00}, 32)...)
	data = append(data, []byte("mpvdnot-an-mp4-payload")...)
	return data
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s to exist: %v", path, err)
	}
}

func assertFileDoesNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file %s not to exist, got err=%v", path, err)
	}
}
