package main

import (
	"bytes"
	"fmt"
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

func TestProcessInputsSingleFileRejectsFalsePositive(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "false-positive.jpg")
	output := filepath.Join(tempDir, "out")
	writeFalsePositiveFixture(t, input)

	cfg := testConfig(input, output)
	e := extractor.New()

	err := processInputs(cfg, e)
	if err == nil {
		t.Fatal("processInputs() error = nil, want non-nil")
	}

	assertFileDoesNotExist(t, filepath.Join(output, "false-positive_photo.jpg"))
	assertFileDoesNotExist(t, filepath.Join(output, "false-positive_video.mp4"))
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
