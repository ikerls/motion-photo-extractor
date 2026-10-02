package extractor

import (
	"bytes"
	"errors"
	"fmt"
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

func TestSplitPrefersMetadataOverStrayMPVDAndTrimsPadding(t *testing.T) {
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

	parts, err := Split(motionPhoto)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	if parts.Method != MethodMetadata {
		t.Fatalf("Method = %q, want %q", parts.Method, MethodMetadata)
	}
	if !bytes.Equal(parts.Photo, jpegData) {
		t.Fatalf("unexpected JPEG extraction: got %q want %q", parts.Photo, jpegData)
	}
	if !bytes.Equal(parts.Video, mp4Data) {
		t.Fatalf("unexpected MP4 extraction: got %q want %q", parts.Video, mp4Data)
	}
}

func TestSplitFallsBackToMarkerWhenMetadataCandidateIsStale(t *testing.T) {
	wantJPEG, motionPhoto := buildMotionPhotoFixture(minimalMP4Data, len(minimalMP4Data)+4, true)

	parts, err := Split(motionPhoto)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}

	if parts.Method != MethodMotionPhotoData {
		t.Fatalf("Method = %q, want %q", parts.Method, MethodMotionPhotoData)
	}
	if !bytes.Equal(parts.Photo, wantJPEG) {
		t.Fatalf("unexpected JPEG extraction: got %q want %q", parts.Photo, wantJPEG)
	}
	if !bytes.Equal(parts.Video, minimalMP4Data) {
		t.Fatalf("unexpected MP4 extraction: got %q want %q", parts.Video, minimalMP4Data)
	}
}

func TestSplitRejectsStrayMPVDAfterJPEGEnd(t *testing.T) {
	_, err := Split(buildTrailingMPVDFalsePositive())
	if !errors.Is(err, ErrNotMotionPhoto) {
		t.Fatalf("Split() error = %v, want ErrNotMotionPhoto", err)
	}
}

func TestSplitRejectsStrayMPVDInsideJPEGData(t *testing.T) {
	_, err := Split(buildEmbeddedMPVDFalsePositive())
	if !errors.Is(err, ErrNotMotionPhoto) {
		t.Fatalf("Split() error = %v, want ErrNotMotionPhoto", err)
	}
}

func TestSplitRejectsDataWithoutMetadataOrMarker(t *testing.T) {
	_, err := Split([]byte{0xFF, 0xD8, 'x', 0xFF, 0xD9})
	if !errors.Is(err, ErrNotMotionPhoto) {
		t.Fatalf("Split() error = %v, want ErrNotMotionPhoto", err)
	}
}

// HEIC image data is not JPEG, so a 0xFFD9 byte pair inside it means nothing
// and must not be taken for the end of the photo.
func TestSplitKeepsWholeImageWhenInputIsNotJPEG(t *testing.T) {
	image := []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c'}
	image = append(image, 0xAA, 0xFF, 0xD9, 0xBB, 0xCC, 0xDD)

	t.Run("mpvd box", func(t *testing.T) {
		boxSize := byte(8 + len(minimalMP4Data))
		data := append([]byte{}, image...)
		data = append(data, 0x00, 0x00, 0x00, boxSize, 'm', 'p', 'v', 'd')
		data = append(data, minimalMP4Data...)

		parts, err := Split(data)
		if err != nil {
			t.Fatalf("Split() error = %v", err)
		}
		if !bytes.Equal(parts.Photo, image) {
			t.Fatalf("unexpected photo: got %q want %q", parts.Photo, image)
		}
		if !bytes.Equal(parts.Video, minimalMP4Data) {
			t.Fatalf("unexpected video: got %q want %q", parts.Video, minimalMP4Data)
		}
	})

	t.Run("MotionPhoto_Data marker", func(t *testing.T) {
		data := append([]byte{}, image...)
		data = append(data, magicV1...)
		data = append(data, minimalMP4Data...)

		parts, err := Split(data)
		if err != nil {
			t.Fatalf("Split() error = %v", err)
		}
		if !bytes.Equal(parts.Photo, image) {
			t.Fatalf("unexpected photo: got %q want %q", parts.Photo, image)
		}
	})
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
