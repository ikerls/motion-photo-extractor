package cli

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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

// Files of the same name in different directories would share their outputs
// in one output directory. Only the first may have them.
func TestProcessKeepsFilesWhoseOutputsCollide(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*config.Config)
		outputs   []string
	}{
		{
			name:      "delete original",
			configure: func(cfg *config.Config) { cfg.DeleteOrig = true },
			outputs:   []string{"IMG_photo.jpg", "IMG_video.mp4"},
		},
		{
			name:      "delete original and overwrite",
			configure: func(cfg *config.Config) { cfg.DeleteOrig, cfg.Force = true, true },
			outputs:   []string{"IMG_photo.jpg", "IMG_video.mp4"},
		},
		{
			name:      "rename original and overwrite",
			configure: func(cfg *config.Config) { cfg.RenameOrig, cfg.Force = true, true },
			outputs:   []string{"IMG.jpg", "IMG.mp4", "IMG_original.jpg"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			output := filepath.Join(tempDir, "out")
			first := filepath.Join(tempDir, "in", "a", "IMG.jpg")
			second := filepath.Join(tempDir, "in", "b", "IMG.jpg")
			for _, input := range []string{first, second} {
				if err := os.MkdirAll(filepath.Dir(input), 0755); err != nil {
					t.Fatalf("mkdir input dir: %v", err)
				}
				writeMotionPhotoFixture(t, input)
			}
			// Told apart from the first file's outputs by its size.
			appendToFile(t, second, []byte("more video"))

			cfg := testConfig(output, filepath.Join(tempDir, "in"))
			tc.configure(cfg)
			rep := discardReporter()
			if err := process(t.Context(), cfg, rep); err != errFilesFailed {
				t.Fatalf("process() error = %v, want errFilesFailed", err)
			}
			if rep.extracted != 1 || rep.failed != 1 {
				t.Fatalf("extracted = %d, failed = %d, want 1 and 1", rep.extracted, rep.failed)
			}

			assertFileDoesNotExist(t, first)
			assertFileContent(t, second, append(buildMotionPhotoFixture(), "more video"...))
			for _, name := range tc.outputs {
				assertFileExists(t, filepath.Join(output, name))
			}
			if got, want := fileSize(filepath.Join(output, tc.outputs[1])), int64(24); got != want {
				t.Fatalf("video size = %d, want the first file's %d", got, want)
			}
		})
	}
}

// A file that failed after its outputs were written, its original not being
// removable, has them all the same. A later file must not replace them.
func TestProcessKeepsOutputsOfAFileThatFailedAfterWritingThem(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory that files cannot be removed from")
	}
	tempDir := t.TempDir()
	output := filepath.Join(tempDir, "out")
	first := filepath.Join(tempDir, "in", "a", "IMG.jpg")
	second := filepath.Join(tempDir, "in", "b", "IMG.jpg")
	for _, input := range []string{first, second} {
		if err := os.MkdirAll(filepath.Dir(input), 0755); err != nil {
			t.Fatalf("mkdir input dir: %v", err)
		}
		writeMotionPhotoFixture(t, input)
	}
	// Told apart from the first file's outputs by its size.
	appendToFile(t, second, []byte("more video"))
	if err := os.Chmod(filepath.Dir(first), 0555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Dir(first), 0755) })

	cfg := testConfig(output, filepath.Join(tempDir, "in"))
	cfg.DeleteOrig, cfg.Force = true, true
	rep := discardReporter()
	if err := process(t.Context(), cfg, rep); err != errFilesFailed {
		t.Fatalf("process() error = %v, want errFilesFailed", err)
	}
	if rep.extracted != 0 || rep.failed != 2 {
		t.Fatalf("extracted = %d, failed = %d, want 0 and 2", rep.extracted, rep.failed)
	}

	assertFileExists(t, first)
	assertFileContent(t, second, append(buildMotionPhotoFixture(), "more video"...))
	assertFileExists(t, filepath.Join(output, "IMG_photo.jpg"))
	if got, want := fileSize(filepath.Join(output, "IMG_video.mp4")), int64(24); got != want {
		t.Fatalf("video size = %d, want the first file's %d", got, want)
	}
}

func TestProcessExtractsAFileNamedTwiceOnce(t *testing.T) {
	tempDir := t.TempDir()
	output := filepath.Join(tempDir, "out")
	input := filepath.Join(tempDir, "in", "single.jpg")
	if err := os.MkdirAll(filepath.Dir(input), 0755); err != nil {
		t.Fatalf("mkdir input dir: %v", err)
	}
	writeMotionPhotoFixture(t, input)

	cfg := testConfig(output, filepath.Join(tempDir, "in"), input)
	cfg.DeleteOrig = true
	rep := discardReporter()
	if err := process(t.Context(), cfg, rep); err != nil {
		t.Fatalf("process() error = %v", err)
	}
	if rep.total != 1 || rep.extracted != 1 {
		t.Fatalf("total = %d, extracted = %d, want 1 and 1", rep.total, rep.extracted)
	}
	assertFileExists(t, filepath.Join(output, "single_video.mp4"))
}

// With --rename-orig the originals stay next to what was extracted from
// them. A second run must leave them alone instead of extracting them again.
func TestProcessDoesNotExtractRenamedOriginalsAgain(t *testing.T) {
	dir := t.TempDir()
	writeMotionPhotoFixture(t, filepath.Join(dir, "IMG.jpg"))
	// An original that nothing was extracted from is a file like any other.
	writeMotionPhotoFixture(t, filepath.Join(dir, "other_original.jpg"))

	cfg := testConfig(dir, dir)
	cfg.RenameOrig = true
	rep := discardReporter()
	if err := process(t.Context(), cfg, rep); err != nil {
		t.Fatalf("process() error = %v", err)
	}
	if rep.extracted != 2 {
		t.Fatalf("extracted = %d, want 2", rep.extracted)
	}
	want := []string{
		"IMG.jpg", "IMG.mp4", "IMG_original.jpg",
		"other_original.jpg", "other_original.mp4", "other_original_original.jpg",
	}
	assertDirEntries(t, dir, want...)

	for _, force := range []bool{false, true} {
		cfg.Force = force
		rep = discardReporter()
		if err := process(t.Context(), cfg, rep); err != nil {
			t.Fatalf("second process() error = %v", err)
		}
		if rep.extracted != 0 || rep.moved != 2 || rep.notMotion != 2 {
			t.Fatalf("extracted = %d, moved = %d, notMotion = %d, want 0, 2 and 2", rep.extracted, rep.moved, rep.notMotion)
		}
		assertDirEntries(t, dir, want...)
	}
}

// What tells of an earlier run is either component, whichever this run
// extracts, and the directory however it is reached.
func TestProcessRecognizesRenamedOriginalsOfADifferentRun(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeMotionPhotoFixture(t, filepath.Join(real, "a.jpg"))

	cfg := testConfig(real, real)
	cfg.RenameOrig, cfg.ExtractPhoto = true, false
	if err := process(t.Context(), cfg, discardReporter()); err != nil {
		t.Fatalf("process() error = %v", err)
	}
	assertDirEntries(t, real, "a.mp4", "a_original.jpg")

	// Only the video is there, and only the photo is asked for.
	cfg.ExtractPhoto, cfg.ExtractVideo = true, false
	rep := discardReporter()
	if err := process(t.Context(), cfg, rep); err != nil {
		t.Fatalf("second process() error = %v", err)
	}
	if rep.moved != 1 {
		t.Fatalf("moved = %d, want 1", rep.moved)
	}
	assertDirEntries(t, real, "a.mp4", "a_original.jpg")

	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}
	cfg = testConfig(real, alias)
	cfg.RenameOrig = true
	rep = discardReporter()
	if err := process(t.Context(), cfg, rep); err != nil {
		t.Fatalf("process() through a link error = %v", err)
	}
	if rep.moved != 1 {
		t.Fatalf("moved through a link = %d, want 1", rep.moved)
	}
	assertDirEntries(t, real, "a.mp4", "a_original.jpg")
}

// A motion photo of the run may also be reached through a link, which would
// lead to the photo of another file once that replaced it.
func TestProcessDoesNotOverwriteAnInputBehindALink(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.jpg")
	target := filepath.Join(dir, "a_photo.jpg")
	link := filepath.Join(dir, "b.jpg")
	writeMotionPhotoFixture(t, first)
	writeMotionPhotoFixture(t, target)
	if err := os.Symlink("a_photo.jpg", link); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}

	cfg := testConfig(dir, first, link)
	cfg.Force = true
	rep := discardReporter()
	if err := process(t.Context(), cfg, rep); err != errFilesFailed {
		t.Fatalf("process() error = %v, want errFilesFailed", err)
	}
	if rep.extracted != 1 || rep.failed != 1 {
		t.Fatalf("extracted = %d, failed = %d, want 1 and 1", rep.extracted, rep.failed)
	}
	assertFileContent(t, target, buildMotionPhotoFixture())
	assertFileExists(t, filepath.Join(dir, "b_video.mp4"))
}

// With --force an output replaces what is there. That must not be another
// motion photo of the run, which would be lost before it is extracted.
func TestProcessDoesNotOverwriteAnotherInput(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.jpg")
	second := filepath.Join(dir, "a_photo.jpg")
	writeMotionPhotoFixture(t, first)
	writeMotionPhotoFixture(t, second)

	cfg := testConfig(dir, first, second)
	cfg.Force = true
	rep := discardReporter()
	if err := process(t.Context(), cfg, rep); err != errFilesFailed {
		t.Fatalf("process() error = %v, want errFilesFailed", err)
	}
	if rep.extracted != 1 || rep.failed != 1 {
		t.Fatalf("extracted = %d, failed = %d, want 1 and 1", rep.extracted, rep.failed)
	}
	assertFileContent(t, first, buildMotionPhotoFixture())
	assertFileContent(t, second, buildMotionPhotoFixture())
	assertFileExists(t, filepath.Join(dir, "a_photo_video.mp4"))
	assertFileDoesNotExist(t, filepath.Join(dir, "a_video.mp4"))
}

// The outputs of an earlier run are found along with their original when a
// directory is extracted again. --force replaces them all the same.
func TestProcessOverwritesEarlierOutputsFoundAsInputs(t *testing.T) {
	dir := t.TempDir()
	writeMotionPhotoFixture(t, filepath.Join(dir, "a.jpg"))

	cfg := testConfig(dir, dir)
	if err := process(t.Context(), cfg, discardReporter()); err != nil {
		t.Fatalf("process() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a_video.mp4"), []byte("stale"), 0644); err != nil {
		t.Fatalf("write stale video: %v", err)
	}

	cfg.Force = true
	rep := discardReporter()
	if err := process(t.Context(), cfg, rep); err != nil {
		t.Fatalf("second process() error = %v", err)
	}
	if rep.extracted != 1 || rep.notMotion != 1 {
		t.Fatalf("extracted = %d, notMotion = %d, want 1 and 1", rep.extracted, rep.notMotion)
	}
	if got, want := fileSize(filepath.Join(dir, "a_video.mp4")), int64(24); got != want {
		t.Fatalf("video size = %d, want %d", got, want)
	}
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

// A config file that cannot be read must not keep the version from showing.
func TestRunPrintsVersionDespiteBrokenConfig(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, "go-motion-photo.yaml"), []byte("output: [unterminated"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	status := Run([]string{"--version"}, &stdout, &stderr, "1.2.3")
	if status != 0 || stdout.String() != "go-motion-photo 1.2.3\n" || stderr.Len() != 0 {
		t.Fatalf("status = %d, stdout = %q, stderr = %q", status, &stdout, &stderr)
	}

	if status := Run([]string{"a.jpg"}, &stdout, &stderr, "1.2.3"); status != 1 || !strings.Contains(stderr.String(), "failed to read config file") {
		t.Fatalf("without --version: status = %d, stderr = %q", status, &stderr)
	}
}

func TestRunProcessesInputFlagAndArguments(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	for _, name := range []string{"a.jpg", "b.jpg", "c.jpg"} {
		writeMotionPhotoFixture(t, filepath.Join(dir, name))
	}

	var stdout, stderr bytes.Buffer
	if status := Run([]string{"a.jpg", "--input", "b.jpg", "c.jpg", "--output", "out"}, &stdout, &stderr, "test"); status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, &stderr)
	}
	for _, name := range []string{"a_video.mp4", "b_video.mp4", "c_video.mp4"} {
		assertFileExists(t, filepath.Join(dir, "out", name))
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

func appendToFile(t *testing.T, path string, data []byte) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	if _, err := file.Write(data); err != nil {
		t.Fatalf("append to %s: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("content of %s = %q, want %q", path, got, want)
	}
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

func assertDirEntries(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("entries of %s = %q, want %q", dir, got, want)
	}
}
