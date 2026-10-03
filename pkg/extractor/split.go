package extractor

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

var (
	magicV1 = []byte("MotionPhoto_Data")
	magicV2 = []byte("mpvd")
	jpegSOI = []byte{0xFF, 0xD8}
	jpegEOI = []byte{0xFF, 0xD9}
)

const (
	markerTailSearchSize = 16 << 20
	jpegTailSearchSize   = 256 << 10
)

// Method identifies how the boundary between photo and video was located.
type Method string

const (
	// MethodMetadata means the video length came from the XMP metadata.
	MethodMetadata Method = "metadata"
	// MethodMotionPhotoData means the legacy MotionPhoto_Data marker was used.
	MethodMotionPhotoData Method = "MotionPhoto_Data marker"
	// MethodMPVD means the mpvd marker was used.
	MethodMPVD Method = "mpvd marker"
)

// Parts is a motion photo split into its components.
type Parts struct {
	// Photo and Video alias the data passed to Split; they are not copies.
	Photo  []byte
	Video  []byte
	Method Method
}

// Split locates the still image and the embedded video inside a motion photo.
//
// The XMP metadata is tried first, then the MotionPhoto_Data and mpvd markers.
// A candidate is only accepted when it is followed by something that looks
// like an MP4 and, for JPEG input, preceded by a JPEG end marker. If no
// candidate qualifies the returned error wraps ErrNotMotionPhoto.
func Split(data []byte) (Parts, error) {
	s := splitter{data: data, seen: make(map[int]struct{}, 3)}

	b, err := s.locate()
	if err != nil {
		return Parts{}, err
	}

	if bytes.HasPrefix(data, jpegSOI) {
		// Drop any padding or marker bytes between the image and the video.
		if end := findJPEGEndBefore(data, b.videoStart); end != -1 {
			b.photoEnd = end
		}
	}

	return Parts{Photo: data[:b.photoEnd], Video: data[b.videoStart:], Method: b.method}, nil
}

// boundary is an accepted split point. photoEnd <= videoStart; the bytes in
// between (a marker, padding) belong to neither component.
type boundary struct {
	photoEnd   int
	videoStart int
	method     Method
}

type splitter struct {
	data   []byte
	seen   map[int]struct{}
	issues []string
}

func (s *splitter) locate() (boundary, error) {
	if videoLength, ok := findMotionPhotoVideoLength(s.data); ok {
		start := len(s.data) - videoLength
		if s.accept(start, MethodMetadata) {
			return boundary{photoEnd: start, videoStart: start, method: MethodMetadata}, nil
		}
	}

	if b, ok := s.findMarker(magicV1, MethodMotionPhotoData); ok {
		return b, nil
	}

	if b, ok := s.findMarker(magicV2, MethodMPVD); ok {
		b.photoEnd = boxStart(s.data, b.photoEnd)
		return b, nil
	}

	if len(s.issues) == 0 {
		return boundary{}, fmt.Errorf("%w: no motion photo metadata or marker found", ErrNotMotionPhoto)
	}

	return boundary{}, fmt.Errorf("%w: no valid embedded video found: %s", ErrNotMotionPhoto, strings.Join(s.issues, "; "))
}

// accept reports whether start is a valid, not yet rejected split candidate.
// The reason for a rejection is kept for the final error message.
func (s *splitter) accept(start int, method Method) bool {
	if _, ok := s.seen[start]; ok {
		return false
	}
	s.seen[start] = struct{}{}

	if err := validateSplitCandidate(s.data, start); err != nil {
		s.issues = append(s.issues, fmt.Sprintf("%s: %v", method, err))
		return false
	}

	return true
}

// findMarker looks for the last valid occurrence of magic, searching the tail
// of the file first since that is where the video normally lives.
func (s *splitter) findMarker(magic []byte, method Method) (boundary, bool) {
	if len(s.data) < len(magic) {
		return boundary{}, false
	}

	searchStart := max(len(s.data)-markerTailSearchSize, 0)
	rejected := len(s.issues)

	if b, ok := s.searchMarkerRegion(s.data[searchStart:], searchStart, magic, method); ok {
		return b, true
	}

	if searchStart > 0 {
		if b, ok := s.searchMarkerRegion(s.data[:searchStart+len(magic)-1], 0, magic, method); ok {
			return b, true
		}
	}

	// A short marker can match many times; only report the last rejection.
	if len(s.issues) > rejected+1 {
		s.issues = append(s.issues[:rejected], s.issues[len(s.issues)-1])
	}

	return boundary{}, false
}

func (s *splitter) searchMarkerRegion(region []byte, base int, magic []byte, method Method) (boundary, bool) {
	for len(region) >= len(magic) {
		markerIndex := bytes.LastIndex(region, magic)
		if markerIndex == -1 {
			break
		}

		markerStart := base + markerIndex
		start := markerStart + len(magic)
		if s.accept(start, method) {
			return boundary{photoEnd: markerStart, videoStart: start, method: method}, true
		}

		region = region[:markerIndex]
	}

	return boundary{}, false
}

// boxStart returns the offset of the ISO BMFF box whose type field begins at
// typeStart, or typeStart itself when the preceding bytes are not a plausible
// box size. HEIC motion photos carry the video inside an mpvd box, whose
// header is not part of the image.
func boxStart(data []byte, typeStart int) int {
	if typeStart < 4 {
		return typeStart
	}

	start := typeStart - 4
	size := uint64(binary.BigEndian.Uint32(data[start:typeStart]))
	if size < 8 || size > uint64(len(data)-start) {
		return typeStart
	}

	return start
}

func validateSplitCandidate(data []byte, start int) error {
	if start <= 0 || start >= len(data) {
		return fmt.Errorf("candidate start %d out of range", start)
	}

	if bytes.HasPrefix(data, jpegSOI) {
		if findJPEGEndBefore(data, start) == -1 {
			return fmt.Errorf("no JPEG end marker before candidate")
		}
	}

	if !looksLikeMP4(data[start:]) {
		return fmt.Errorf("candidate payload does not look like MP4")
	}

	return nil
}

func findJPEGEndBefore(data []byte, limit int) int {
	limit = min(limit, len(data))
	searchStart := max(limit-jpegTailSearchSize, 0)

	eoiIndex := bytes.LastIndex(data[searchStart:limit], jpegEOI)
	if eoiIndex == -1 {
		if searchStart == 0 {
			return -1
		}

		eoiIndex = bytes.LastIndex(data[:limit], jpegEOI)
		if eoiIndex == -1 {
			return -1
		}
		return eoiIndex + 2
	}

	return searchStart + eoiIndex + 2
}
