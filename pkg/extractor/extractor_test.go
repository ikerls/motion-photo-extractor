package extractor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProcessExtractsOnlyVideoWhenPhotoDisabled(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	output := filepath.Join(tempDir, "out")
	writeMotionPhotoFixture(t, input)

	e := New()
	err := e.Process(input, output, false, false, false, true, false)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	assertFileDoesNotExist(t, filepath.Join(output, "sample_photo.jpg"))
	assertFileExists(t, filepath.Join(output, "sample_video.mp4"))
}

func TestProcessExtractsOnlyPhotoWhenVideoDisabled(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	output := filepath.Join(tempDir, "out")
	writeMotionPhotoFixture(t, input)

	e := New()
	err := e.Process(input, output, false, false, true, false, false)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	assertFileExists(t, filepath.Join(output, "sample_photo.jpg"))
	assertFileDoesNotExist(t, filepath.Join(output, "sample_video.mp4"))
}

func TestProcessReturnsErrorWhenBothExtractionsDisabled(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	output := filepath.Join(tempDir, "out")
	writeMotionPhotoFixture(t, input)

	e := New()
	err := e.Process(input, output, false, false, false, false, false)
	if err == nil {
		t.Fatal("Process() error = nil, want non-nil")
	}
}

func writeMotionPhotoFixture(t *testing.T, path string) {
	t.Helper()
	data := []byte("jpeg-data-prefix" + "mpvd" + "mp4-data-payload")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
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
