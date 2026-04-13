package extractor

import (
	"bytes"
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

func TestSplitContentPrefersMetadataOverStrayMPVDAndTrimsPadding(t *testing.T) {
	e := New()

	mp4Data := []byte{
		0x00, 0x00, 0x00, 0x10,
		'f', 't', 'y', 'p',
		'm', 'p', '4', '2',
		'd', 'a', 't', 'a',
	}

	xmp := []byte(`<x:xmpmeta><Container:Directory>` +
		`<Container:Item Item:Mime="image/jpeg" Item:Semantic="Primary" Item:Padding="5"/>` +
		`<Container:Item Item:Mime="video/mp4" Item:Semantic="MotionPhoto" Item:Length="16" Item:Padding="0"/>` +
		`</Container:Directory></x:xmpmeta>`)

	jpegData := append([]byte{0xFF, 0xD8}, xmp...)
	jpegData = append(jpegData, []byte("stray-mpvd-inside-jpeg")...)
	jpegData = append(jpegData, 0xFF, 0xD9)

	padding := []byte("ABCDE")
	motionPhoto := append(append([]byte{}, jpegData...), padding...)
	motionPhoto = append(motionPhoto, mp4Data...)

	extractedJPEG, extractedMP4, err := e.splitContent(motionPhoto)
	if err != nil {
		t.Fatalf("splitContent() error = %v", err)
	}

	if !bytes.Equal(extractedJPEG, jpegData) {
		t.Fatalf("unexpected JPEG extraction: got %q want %q", extractedJPEG, jpegData)
	}

	if !bytes.Equal(extractedMP4, mp4Data) {
		t.Fatalf("unexpected MP4 extraction: got %q want %q", extractedMP4, mp4Data)
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
