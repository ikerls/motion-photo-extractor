package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ikerls/motion-photo-extractor/internal/config"
	"github.com/ikerls/motion-photo-extractor/pkg/extractor"
)

func TestProcessInputsSingleFile(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "single.jpg")
	output := filepath.Join(tempDir, "out")
	writeMotionPhotoFixture(t, input)

	cfg := testConfig(input, output)
	e := extractor.New()

	if err := processInputs(cfg, e); err != nil {
		t.Fatalf("processInputs() error = %v", err)
	}

	assertFileExists(t, filepath.Join(output, "single_photo.jpg"))
	assertFileExists(t, filepath.Join(output, "single_video.mp4"))
}

func TestProcessInputsSingleFileWithoutVideoExtraction(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "single.jpg")
	output := filepath.Join(tempDir, "out")
	writeMotionPhotoFixture(t, input)

	cfg := testConfig(input, output)
	cfg.ExtractVideo = false
	e := extractor.New()

	if err := processInputs(cfg, e); err != nil {
		t.Fatalf("processInputs() error = %v", err)
	}

	assertFileExists(t, filepath.Join(output, "single_photo.jpg"))
	assertFileDoesNotExist(t, filepath.Join(output, "single_video.mp4"))
}

func TestProcessInputsDirectoryProcessesSupportedFiles(t *testing.T) {
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

	cfg := testConfig(inputDir, output)
	e := extractor.New()

	if err := processInputs(cfg, e); err != nil {
		t.Fatalf("processInputs() error = %v", err)
	}

	assertFileExists(t, filepath.Join(output, "a_photo.jpg"))
	assertFileExists(t, filepath.Join(output, "a_video.mp4"))
	assertFileExists(t, filepath.Join(output, "b_photo.heic"))
	assertFileExists(t, filepath.Join(output, "b_video.mp4"))
	assertFileDoesNotExist(t, filepath.Join(output, "skip_video.mp4"))
}

func TestProcessInputsGlobPattern(t *testing.T) {
	tempDir := t.TempDir()
	output := filepath.Join(tempDir, "out")
	writeMotionPhotoFixture(t, filepath.Join(tempDir, "g1.jpg"))
	writeMotionPhotoFixture(t, filepath.Join(tempDir, "g2.jpg"))
	if err := os.WriteFile(filepath.Join(tempDir, "g3.png"), []byte("not-supported"), 0644); err != nil {
		t.Fatalf("write png fixture: %v", err)
	}

	cfg := testConfig(filepath.Join(tempDir, "*.jpg"), output)
	e := extractor.New()

	if err := processInputs(cfg, e); err != nil {
		t.Fatalf("processInputs() error = %v", err)
	}

	assertFileExists(t, filepath.Join(output, "g1_photo.jpg"))
	assertFileExists(t, filepath.Join(output, "g1_video.mp4"))
	assertFileExists(t, filepath.Join(output, "g2_photo.jpg"))
	assertFileExists(t, filepath.Join(output, "g2_video.mp4"))
	assertFileDoesNotExist(t, filepath.Join(output, "g3_video.mp4"))
}

func TestProcessInputsRegexPattern(t *testing.T) {
	tempDir := t.TempDir()
	output := filepath.Join(tempDir, "out")
	writeMotionPhotoFixture(t, filepath.Join(tempDir, "IMG_0001.jpg"))
	writeMotionPhotoFixture(t, filepath.Join(tempDir, "IMG_0002.jpg"))
	writeMotionPhotoFixture(t, filepath.Join(tempDir, "OTHER_0003.jpg"))

	cfg := testConfig(`/IMG_\d{4}\.jpg/`, output)
	e := extractor.New()

	withWorkingDirectory(t, tempDir, func() {
		if err := processInputs(cfg, e); err != nil {
			t.Fatalf("processInputs() error = %v", err)
		}
	})

	assertFileExists(t, filepath.Join(output, "IMG_0001_photo.jpg"))
	assertFileExists(t, filepath.Join(output, "IMG_0001_video.mp4"))
	assertFileExists(t, filepath.Join(output, "IMG_0002_photo.jpg"))
	assertFileExists(t, filepath.Join(output, "IMG_0002_video.mp4"))
	assertFileDoesNotExist(t, filepath.Join(output, "OTHER_0003_video.mp4"))
}

func TestProcessInputsInvalidRegexReturnsError(t *testing.T) {
	cfg := testConfig(`/[unterminated/`, t.TempDir())
	e := extractor.New()

	err := processInputs(cfg, e)
	if err == nil {
		t.Fatal("processInputs() error = nil, want non-nil")
	}
}

func TestProcessInputsInvalidGlobReturnsError(t *testing.T) {
	cfg := testConfig("[", t.TempDir())
	e := extractor.New()

	err := processInputs(cfg, e)
	if err == nil {
		t.Fatal("processInputs() error = nil, want non-nil")
	}
}

func testConfig(input, output string) *config.Config {
	return &config.Config{
		InputFile:    input,
		OutputDir:    output,
		ExtractPhoto: true,
		ExtractVideo: true,
	}
}

func writeMotionPhotoFixture(t *testing.T, path string) {
	t.Helper()
	data := []byte("jpeg-data-prefix" + "mpvd" + "mp4-data-payload")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func withWorkingDirectory(t *testing.T, dir string, fn func()) {
	t.Helper()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("restore chdir: %v", err)
		}
	})
	fn()
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
