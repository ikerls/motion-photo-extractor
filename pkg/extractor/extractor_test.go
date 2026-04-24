package extractor

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

var minimalMP4Data = []byte{
	0x00, 0x00, 0x00, 0x18,
	'f', 't', 'y', 'p',
	'm', 'p', '4', '2',
	0x00, 0x00, 0x00, 0x00,
	'i', 's', 'o', 'm',
	'm', 'p', '4', '2',
}

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
	mp4Data := append([]byte(nil), minimalMP4Data...)

	xmp := []byte(fmt.Sprintf(`<x:xmpmeta><rdf:RDF><rdf:Description `+
		`xmlns:GCamera="http://ns.google.com/photos/1.0/camera/" `+
		`xmlns:Container="http://ns.google.com/photos/1.0/container/" `+
		`xmlns:Item="http://ns.google.com/photos/1.0/container/item/" `+
		`GCamera:MotionPhoto="1" `+
		`GCamera:MotionPhotoOffset="%d">`+
		`<Container:Directory><rdf:Seq>`+
		`<rdf:li rdf:parseType="Resource"><Container:Item Item:Mime="image/jpeg" Item:Semantic="Primary" Item:Length="0" Item:Padding="5"/></rdf:li>`+
		`<rdf:li rdf:parseType="Resource"><Container:Item Item:Mime="video/mp4" Item:Semantic="MotionPhoto" Item:Length="%d" Item:Padding="0"/></rdf:li>`+
		`</rdf:Seq></Container:Directory></rdf:Description></rdf:RDF></x:xmpmeta>`, len(mp4Data), len(mp4Data)))

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

func TestSplitContentFallsBackToMarkerWhenMetadataCandidateIsStale(t *testing.T) {
	e := New()
	wantJPEG, motionPhoto := buildMotionPhotoFixture(minimalMP4Data, len(minimalMP4Data)+4, true)

	extractedJPEG, extractedMP4, err := e.splitContent(motionPhoto)
	if err != nil {
		t.Fatalf("splitContent() error = %v", err)
	}

	if !bytes.Equal(extractedJPEG, wantJPEG) {
		t.Fatalf("unexpected JPEG extraction: got %q want %q", extractedJPEG, wantJPEG)
	}

	if !bytes.Equal(extractedMP4, minimalMP4Data) {
		t.Fatalf("unexpected MP4 extraction: got %q want %q", extractedMP4, minimalMP4Data)
	}
}

func TestSplitContentRejectsStrayMPVDAfterJPEGEnd(t *testing.T) {
	e := New()
	_, _, err := e.splitContent(buildTrailingMPVDFalsePositive())
	if err == nil {
		t.Fatal("splitContent() error = nil, want non-nil")
	}
}

func TestSplitContentRejectsStrayMPVDInsideJPEGData(t *testing.T) {
	e := New()
	_, _, err := e.splitContent(buildEmbeddedMPVDFalsePositive())
	if err == nil {
		t.Fatal("splitContent() error = nil, want non-nil")
	}
}

func TestProcessSanitizesExtractedPhotoAndRejectsSecondPass(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	output := filepath.Join(tempDir, "out")
	writeMotionPhotoFixture(t, input)

	e := New()
	if err := e.Process(input, output, false, false, true, true, false); err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	photoPath := filepath.Join(output, "sample_photo.jpg")
	photoData, err := os.ReadFile(photoPath)
	if err != nil {
		t.Fatalf("read extracted photo: %v", err)
	}

	if bytes.Contains(photoData, []byte(`Item:Semantic="MotionPhoto"`)) {
		t.Fatal("extracted photo still contains motion photo semantic metadata")
	}
	if bytes.Contains(photoData, []byte(`GCamera:MotionPhoto="1"`)) {
		t.Fatal("extracted photo still advertises MotionPhoto=1")
	}
	if bytes.Contains(photoData, []byte(`GCamera:MotionPhotoOffset="24"`)) {
		t.Fatal("extracted photo still contains a non-zero motion photo offset")
	}

	secondOutput := filepath.Join(tempDir, "out-second-pass")
	err = e.Process(photoPath, secondOutput, false, false, true, true, false)
	if err == nil {
		t.Fatal("Process() error = nil on extracted photo, want non-nil")
	}

	assertFileDoesNotExist(t, filepath.Join(secondOutput, "sample_photo_photo.jpg"))
	assertFileDoesNotExist(t, filepath.Join(secondOutput, "sample_photo_video.mp4"))
}

func writeMotionPhotoFixture(t *testing.T, path string) {
	t.Helper()
	_, data := buildMotionPhotoFixture(minimalMP4Data, len(minimalMP4Data), true)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func buildMotionPhotoFixture(mp4Data []byte, metadataLength int, includeMarker bool) ([]byte, []byte) {
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
		`</rdf:Seq></Container:Directory></rdf:Description></rdf:RDF></x:xmpmeta>`, metadataLength, metadataLength))

	jpegData := append([]byte{0xFF, 0xD8}, xmp...)
	jpegData = append(jpegData, []byte("stray-mpvd-inside-jpeg")...)
	jpegData = append(jpegData, 0xFF, 0xD9)

	motionPhoto := append([]byte{}, jpegData...)
	if includeMarker {
		motionPhoto = append(motionPhoto, magicV1...)
	}
	motionPhoto = append(motionPhoto, mp4Data...)

	return jpegData, motionPhoto
}

func buildTrailingMPVDFalsePositive() []byte {
	jpegData := append([]byte{0xFF, 0xD8}, []byte("plain-jpeg-data")...)
	jpegData = append(jpegData, 0xFF, 0xD9)

	data := append([]byte{}, jpegData...)
	data = append(data, bytes.Repeat([]byte{0x00}, 32)...)
	data = append(data, []byte("mpvdnot-an-mp4-payload")...)
	return data
}

func buildEmbeddedMPVDFalsePositive() []byte {
	data := append([]byte{0xFF, 0xD8}, []byte("jpeg-body-with-mpvd-inside")...)
	data = append(data, 0xFF, 0xD9)
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
