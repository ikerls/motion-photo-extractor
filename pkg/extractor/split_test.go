package extractor

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
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

// completeMP4Data is a video whose boxes can be followed to its end.
var completeMP4Data = slices.Concat(
	minimalMP4Data,
	mp4Box("moov", []byte("movie header")),
	mp4Box("mdat", []byte("media data")),
)

// sefTrailer stands in for the directory Samsung ends its files with.
var sefTrailer = []byte("SEFH\x6b\x00\x00\x00\x01\x00\x00\x00directory\x18\x00\x00\x00SEFT")

func mp4Box(boxType string, payload []byte) []byte {
	box := binary.BigEndian.AppendUint32(nil, uint32(8+len(payload)))
	box = append(box, boxType...)
	return append(box, payload...)
}

func TestSplitLeavesOutWhatFollowsTheVideo(t *testing.T) {
	video := slices.Concat(completeMP4Data, sefTrailer)

	t.Run("marker", func(t *testing.T) {
		// The metadata is stale, so the marker is what locates the video.
		_, data := buildMotionPhotoFixture(video, len(video)+4, true)

		parts, err := Split(data)
		if err != nil {
			t.Fatalf("Split() error = %v", err)
		}
		if parts.Method != MethodMotionPhotoData || !bytes.Equal(parts.Video, completeMP4Data) {
			t.Fatalf("Method = %q, video = %q, want the MP4 alone", parts.Method, parts.Video)
		}
	})

	t.Run("metadata", func(t *testing.T) {
		_, data := buildMotionPhotoFixture(video, len(video), false)

		parts, err := Split(data)
		if err != nil {
			t.Fatalf("Split() error = %v", err)
		}
		if parts.Method != MethodMetadata || !bytes.Equal(parts.Video, completeMP4Data) {
			t.Fatalf("Method = %q, video = %q, want the MP4 alone", parts.Method, parts.Video)
		}
	})

	// Without a moov box there is no telling where the video ends.
	t.Run("keeps everything after an incomplete video", func(t *testing.T) {
		video := slices.Concat(minimalMP4Data, sefTrailer)
		_, data := buildMotionPhotoFixture(video, len(video), true)

		parts, err := Split(data)
		if err != nil {
			t.Fatalf("Split() error = %v", err)
		}
		if !bytes.Equal(parts.Video, video) {
			t.Fatalf("video = %q, want %q", parts.Video, video)
		}
	})
}

// The metadata of a HEIC motion photo points past the header of the mpvd box
// that holds the video. The header is not part of the image, and the box
// that follows the mpvd box is not part of the video.
func TestSplitHEICByMetadataLeavesOutTheMPVDBox(t *testing.T) {
	trailing := mp4Box("free", []byte("after the video"))
	xmp := fmt.Sprintf(`<x:xmpmeta><Container:Item Item:Semantic="MotionPhoto" Item:Length="%d"/></x:xmpmeta>`,
		len(completeMP4Data)+len(trailing))
	image := slices.Concat(mp4Box("ftyp", []byte("heic\x00\x00\x00\x00mif1heic")), mp4Box("mdat", []byte(xmp)))
	data := slices.Concat(image, mp4Box("mpvd", completeMP4Data), trailing)

	parts, err := Split(data)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	if parts.Method != MethodMetadata {
		t.Fatalf("Method = %q, want %q", parts.Method, MethodMetadata)
	}
	if !bytes.Equal(parts.Photo, image) {
		t.Fatalf("unexpected photo: got %q want %q", parts.Photo, image)
	}
	if !bytes.Equal(parts.Video, completeMP4Data) {
		t.Fatalf("unexpected video: got %q want %q", parts.Video, completeMP4Data)
	}
}

// A video may hold a marker and an MP4 of its own. The marker further up,
// whose video contains them, is the one to split at.
func TestSplitPrefersTheVideoThatContainsAnother(t *testing.T) {
	inner := slices.Concat(magicV1, completeMP4Data)
	outer := slices.Concat(minimalMP4Data, mp4Box("free", inner), mp4Box("moov", []byte("movie header")))
	wantJPEG, data := buildMotionPhotoFixture(outer, len(outer)+4, true)

	parts, err := Split(data)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	if !bytes.Equal(parts.Photo, wantJPEG) {
		t.Fatalf("unexpected photo: got %q want %q", parts.Photo, wantJPEG)
	}
	if !bytes.Equal(parts.Video, outer) {
		t.Fatalf("unexpected video: got %q want %q", parts.Video, outer)
	}

	// Two videos one after the other are not one inside the other: the last
	// one is still the one extracted.
	last := slices.Concat(completeMP4Data, mp4Box("free", []byte("the last one")))
	_, data = buildMotionPhotoFixture(slices.Concat(completeMP4Data, magicV1, last), len(last)+4, true)
	if parts, err = Split(data); err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	if !bytes.Equal(parts.Video, last) {
		t.Fatalf("unexpected video of consecutive ones: got %q want %q", parts.Video, last)
	}
}

func TestSplitReadsSingleQuotedMetadata(t *testing.T) {
	_, data := buildMotionPhotoFixture(minimalMP4Data, len(minimalMP4Data), false)
	data = bytes.ReplaceAll(data, []byte(`"`), []byte(`'`))

	parts, err := Split(data)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	if parts.Method != MethodMetadata || !bytes.Equal(parts.Video, minimalMP4Data) {
		t.Fatalf("Method = %q, video = %q", parts.Method, parts.Video)
	}

	SanitizePhoto(parts.Photo)
	for _, attr := range []string{`GCamera:MotionPhoto='1'`, `Item:Semantic='MotionPhoto'`, `GCamera:MotionPhotoOffset='24'`} {
		if bytes.Contains(parts.Photo, []byte(attr)) {
			t.Fatalf("sanitized photo still contains %s", attr)
		}
	}
	if _, err := Split(parts.Photo); !errors.Is(err, ErrNotMotionPhoto) {
		t.Fatalf("Split() of the sanitized photo error = %v, want ErrNotMotionPhoto", err)
	}
}

// The packet that describes the video need not be the first one.
func TestSplitReadsEveryXMPPacket(t *testing.T) {
	wantJPEG, data := buildMotionPhotoFixture(minimalMP4Data, len(minimalMP4Data), false)
	other := []byte(`<x:xmpmeta><rdf:Description dc:title="unrelated"/></x:xmpmeta>`)
	data = slices.Concat(data[:2], other, data[2:])

	parts, err := Split(data)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	if parts.Method != MethodMetadata || len(parts.Photo) != len(wantJPEG)+len(other) {
		t.Fatalf("Method = %q, photo = %q", parts.Method, parts.Photo)
	}

	SanitizePhoto(parts.Photo)
	if bytes.Contains(parts.Photo, []byte(`GCamera:MotionPhoto="1"`)) {
		t.Fatal("sanitized photo still advertises MotionPhoto=1")
	}
}

// Metadata is looked for where the format keeps it: anywhere in a HEIC, and
// in the segments before the image data of a JPEG, however large these are.
func TestSanitizePhotoFindsMetadataFarIntoTheFile(t *testing.T) {
	xmp := []byte(`<x:xmpmeta><rdf:Description GCamera:MotionPhoto="1" GCamera:MotionPhotoOffset="24"/></x:xmpmeta>`)
	enabled := []byte(`GCamera:MotionPhoto="1"`)

	t.Run("HEIC", func(t *testing.T) {
		photo := slices.Concat(
			mp4Box("ftyp", []byte("heic\x00\x00\x00\x00mif1heic")),
			mp4Box("mdat", slices.Concat(make([]byte, xmpSearchLimit), xmp)),
		)
		SanitizePhoto(photo)
		if bytes.Contains(photo, enabled) {
			t.Fatal("sanitized photo still advertises MotionPhoto=1")
		}
	})

	t.Run("JPEG", func(t *testing.T) {
		segment := func(marker byte, payload []byte) []byte {
			header := []byte{0xFF, marker, 0, 0}
			binary.BigEndian.PutUint16(header[2:], uint16(2+len(payload)))
			return append(header, payload...)
		}
		photo := []byte{0xFF, 0xD8}
		for range 10 {
			photo = append(photo, segment(0xE2, make([]byte, 60000))...)
		}
		photo = append(photo, segment(0xE1, xmp)...)
		photo = append(photo, segment(0xDA, nil)...)
		scan := len(photo)
		// Image data is not metadata, whatever it looks like.
		photo = append(photo, xmp...)
		photo = append(photo, 0xFF, 0xD9)

		SanitizePhoto(photo)
		if bytes.Contains(photo[:scan], enabled) {
			t.Fatal("sanitized photo still advertises MotionPhoto=1")
		}
		if !bytes.Contains(photo[scan:], enabled) {
			t.Fatal("image data was rewritten")
		}
	})
}

// Where a video ends is only taken from its boxes if they can be followed
// through all of it. A box of a type that is not letters and digits is a box
// all the same, and nothing is cut off a video whose media data was not seen.
func TestSplitKeepsVideosThatCannotBeFollowedWhole(t *testing.T) {
	moov, mdat := mp4Box("moov", []byte("movie header")), mp4Box("mdat", []byte("media data"))

	tests := []struct {
		name  string
		video []byte
		want  int // bytes of the video kept, the trailer left out or not
	}{
		{
			name:  "custom box before the media data",
			video: slices.Concat(minimalMP4Data, moov, mp4Box("XMP_", []byte("custom")), mp4Box("free", nil), mdat),
			want:  len(minimalMP4Data) + len(moov) + 14 + 8 + len(mdat),
		},
		{
			name:  "something else before the media data",
			video: slices.Concat(minimalMP4Data, moov, []byte{0, 0, 0, 8, 0, 1, 2, 3}, mdat),
			want:  len(minimalMP4Data) + len(moov) + 8 + len(mdat) + len(sefTrailer),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			video := slices.Concat(tc.video, sefTrailer)
			_, data := buildMotionPhotoFixture(video, len(video), true)

			parts, err := Split(data)
			if err != nil {
				t.Fatalf("Split() error = %v", err)
			}
			if !bytes.Equal(parts.Video, video[:tc.want]) {
				t.Fatalf("video = %q, want %q", parts.Video, video[:tc.want])
			}
		})
	}
}

// An mpvd box only says where the video ends if the video fits it whole.
func TestSplitIgnoresAnMPVDSizeThatDoesNotHoldTheVideo(t *testing.T) {
	image := mp4Box("ftyp", []byte("heic\x00\x00\x00\x00mif1heic"))

	for _, size := range []uint32{8, 9, 32} {
		header := binary.BigEndian.AppendUint32(nil, size)
		data := slices.Concat(image, header, magicV2, completeMP4Data)

		parts, err := Split(data)
		if err != nil {
			t.Fatalf("Split() with mpvd size %d error = %v", size, err)
		}
		if !bytes.Equal(parts.Photo, image) || !bytes.Equal(parts.Video, completeMP4Data) {
			t.Fatalf("mpvd size %d: photo = %q, video = %q", size, parts.Photo, parts.Video)
		}
	}
}

// The header of an mpvd box may also give no size, the box running to the
// end, or give it in 64 bits. Neither kind is part of the image.
func TestSplitLeavesOutEveryKindOfMPVDHeader(t *testing.T) {
	xmp := fmt.Sprintf(`<x:xmpmeta><Container:Item Item:Semantic="MotionPhoto" Item:Length="%d"/></x:xmpmeta>`, len(completeMP4Data))
	image := slices.Concat(mp4Box("ftyp", []byte("heic\x00\x00\x00\x00mif1heic")), mp4Box("mdat", []byte(xmp)))

	headers := map[string][]byte{
		"no size":     slices.Concat([]byte{0, 0, 0, 0}, magicV2),
		"64-bit size": binary.BigEndian.AppendUint64(slices.Concat([]byte{0, 0, 0, 1}, magicV2), uint64(16+len(completeMP4Data))),
	}

	for name, header := range headers {
		t.Run(name, func(t *testing.T) {
			parts, err := Split(slices.Concat(image, header, completeMP4Data))
			if err != nil {
				t.Fatalf("Split() error = %v", err)
			}
			if !bytes.Equal(parts.Photo, image) || !bytes.Equal(parts.Video, completeMP4Data) {
				t.Fatalf("photo = %q, video = %q", parts.Photo, parts.Video)
			}
		})
	}
}

// The video that contains another need not be found by the same marker.
func TestSplitPrefersTheVideoInAnMPVDBoxThatContainsAnother(t *testing.T) {
	image := mp4Box("ftyp", []byte("heic\x00\x00\x00\x00mif1heic"))
	inner := slices.Concat(magicV1, completeMP4Data)
	outer := slices.Concat(minimalMP4Data, mp4Box("free", inner), mp4Box("moov", []byte("movie header")), mp4Box("mdat", nil))

	parts, err := Split(slices.Concat(image, mp4Box("mpvd", outer)))
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	if parts.Method != MethodMPVD || !bytes.Equal(parts.Photo, image) || !bytes.Equal(parts.Video, outer) {
		t.Fatalf("Method = %q, photo = %q, video = %q", parts.Method, parts.Photo, parts.Video)
	}
}

// A length that leads nowhere does not keep the one of a later packet from
// being tried.
func TestSplitTriesTheLengthOfEveryXMPPacket(t *testing.T) {
	wantJPEG, data := buildMotionPhotoFixture(minimalMP4Data, len(minimalMP4Data), false)
	stale := []byte(`<x:xmpmeta><rdf:Description GCamera:MotionPhotoOffset="7"/></x:xmpmeta>`)
	data = slices.Concat(data[:2], stale, data[2:])

	parts, err := Split(data)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	if parts.Method != MethodMetadata || len(parts.Photo) != len(wantJPEG)+len(stale) || !bytes.Equal(parts.Video, minimalMP4Data) {
		t.Fatalf("Method = %q, photo = %q, video = %q", parts.Method, parts.Photo, parts.Video)
	}
}

// The fragments of a video are not its media data: a box that ends after
// one, short of the data, does not say where the video ends.
func TestSplitKeepsFragmentedVideoBeyondAShortMPVDBox(t *testing.T) {
	image := mp4Box("ftyp", []byte("heic\x00\x00\x00\x00mif1heic"))
	start := slices.Concat(minimalMP4Data, mp4Box("moov", []byte("movie header")), mp4Box("moof", []byte("fragment")))
	video := slices.Concat(start, mp4Box("mdat", []byte("media data")), mp4Box("mfra", []byte("index")))

	header := binary.BigEndian.AppendUint32(nil, uint32(8+len(start)))
	parts, err := Split(slices.Concat(image, header, magicV2, video))
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	if !bytes.Equal(parts.Photo, image) || !bytes.Equal(parts.Video, video) {
		t.Fatalf("photo = %q, video = %q", parts.Photo, parts.Video)
	}
}

// What a metadata segment of the JPEG holds is not a video that contains
// the one after the image, whatever it is made to look like.
func TestSplitIgnoresMarkersInsideJPEGSegments(t *testing.T) {
	segment := func(marker byte, payload []byte) []byte {
		header := []byte{0xFF, marker, 0, 0}
		binary.BigEndian.PutUint16(header[2:], uint16(2+len(payload)))
		return append(header, payload...)
	}
	// An end marker, a video marker, and boxes that run to the end of the file.
	decoy := slices.Concat(jpegEOI, magicV2, minimalMP4Data, mp4Box("moov", nil), []byte{0, 0, 0, 0, 'f', 'r', 'e', 'e'})
	photo := slices.Concat(jpegSOI, segment(0xE2, decoy), segment(0xDA, nil), []byte("image data"), jpegEOI)

	parts, err := Split(slices.Concat(photo, magicV1, completeMP4Data))
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	if !bytes.Equal(parts.Photo, photo) || !bytes.Equal(parts.Video, completeMP4Data) {
		t.Fatalf("photo = %q, video = %q", parts.Photo, parts.Video)
	}
}

// An mpvd box whose size is given in 64 bits is found by its marker too,
// with its video beginning after that size.
func TestSplitFindsAnMPVDBoxWithA64BitSize(t *testing.T) {
	image := mp4Box("ftyp", []byte("heic\x00\x00\x00\x00mif1heic"))
	inner := slices.Concat(magicV1, completeMP4Data)
	outer := slices.Concat(minimalMP4Data, mp4Box("free", inner), mp4Box("moov", []byte("movie header")), mp4Box("mdat", nil))

	for name, video := range map[string][]byte{"plain": completeMP4Data, "containing another": outer} {
		t.Run(name, func(t *testing.T) {
			header := binary.BigEndian.AppendUint64(slices.Concat([]byte{0, 0, 0, 1}, magicV2), uint64(16+len(video)))
			parts, err := Split(slices.Concat(image, header, video))
			if err != nil {
				t.Fatalf("Split() error = %v", err)
			}
			if parts.Method != MethodMPVD || !bytes.Equal(parts.Photo, image) || !bytes.Equal(parts.Video, video) {
				t.Fatalf("Method = %q, photo = %q, video = %q", parts.Method, parts.Photo, parts.Video)
			}
		})
	}
}

// The bytes before an mpvd marker may look like the size field of a 64-bit
// header without there being one. The video then begins right after it.
func TestSplitFindsAVideoRightAfterAnMPVDMarker(t *testing.T) {
	photo := slices.Concat(jpegSOI, []byte("image data"), jpegEOI)

	parts, err := Split(slices.Concat(photo, []byte{0, 0, 0, 1}, magicV2, completeMP4Data))
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	if !bytes.Equal(parts.Photo, photo) || !bytes.Equal(parts.Video, completeMP4Data) {
		t.Fatalf("photo = %q, video = %q", parts.Photo, parts.Video)
	}
}

// A JPEG segment that claims to be longer than the file is not followed, and
// does not keep the video that contains another from being found after it.
func TestSplitPrefersTheContainingVideoAfterABrokenJPEGSegment(t *testing.T) {
	photo := slices.Concat(jpegSOI, []byte{0xFF, 0xE1, 0xFF, 0xFF}, []byte("image"), jpegEOI)
	inner := slices.Concat(magicV1, completeMP4Data)
	outer := slices.Concat(minimalMP4Data, mp4Box("free", inner), mp4Box("moov", []byte("movie header")), mp4Box("mdat", nil))

	parts, err := Split(slices.Concat(photo, magicV1, outer))
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	if !bytes.Equal(parts.Photo, photo) || !bytes.Equal(parts.Video, outer) {
		t.Fatalf("photo = %q, video = %q", parts.Photo, parts.Video)
	}
}
