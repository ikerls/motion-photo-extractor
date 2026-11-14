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
	// What lies between them or follows the video, such as the directory
	// Samsung ends its files with, is in neither.
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

	videoEnd := b.videoStart + mp4Length(data[b.videoStart:])

	// HEIC motion photos carry the video inside an mpvd box. Its header is
	// not part of the image. What follows the box is not part of the video,
	// if the box is one that holds the video whole.
	if start, end, ok := mpvdBox(data, b.videoStart); ok {
		b.photoEnd = min(b.photoEnd, start)
		if walk := walkMP4(data[b.videoStart:end]); walk.whole() && b.videoStart+walk.length == end {
			videoEnd = end
		}
	}

	if bytes.HasPrefix(data, jpegSOI) {
		// Drop any padding or marker bytes between the image and the video.
		if end := findJPEGEndBefore(data, b.videoStart); end != -1 {
			b.photoEnd = end
		}
	}

	return Parts{Photo: data[:b.photoEnd], Video: data[b.videoStart:videoEnd], Method: b.method}, nil
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
	for _, videoLength := range findMotionPhotoVideoLengths(s.data) {
		start := len(s.data) - videoLength
		if s.accept(start, MethodMetadata) {
			return boundary{photoEnd: start, videoStart: start, method: MethodMetadata}, nil
		}
	}

	if b, ok := s.findMarker(magicV1, MethodMotionPhotoData); ok {
		return b, nil
	}

	if b, ok := s.findMarker(magicV2, MethodMPVD); ok {
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
		return s.enclosing(b), true
	}

	if searchStart > 0 {
		if b, ok := s.searchMarkerRegion(s.data[:searchStart+len(magic)-1], 0, magic, method); ok {
			return s.enclosing(b), true
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
		start := payloadStart(s.data, markerStart, magic)
		if s.accept(start, method) {
			return boundary{photoEnd: markerStart, videoStart: start, method: method}, true
		}

		region = region[:markerIndex]
	}

	return boundary{}, false
}

// enclosing returns the boundary of the video that the one at b is part of,
// or b if there is none. A video may hold a marker followed by an MP4 of its
// own, in a free box for one, and the last marker is then not the video's.
func (s *splitter) enclosing(b boundary) boundary {
	found := b
	for _, marker := range []struct {
		magic  []byte
		method Method
	}{
		{magicV1, MethodMotionPhotoData},
		{magicV2, MethodMPVD},
	} {
		// Only a video that begins further up than the one found counts,
		// and not what the metadata segments of a JPEG may hold.
		head := s.data[:found.photoEnd]
		for offset, _ := jpegScanStart(s.data); ; {
			index := bytes.Index(head[min(offset, len(head)):], marker.magic)
			if index == -1 {
				break
			}
			markerStart := offset + index
			start := payloadStart(s.data, markerStart, marker.magic)
			offset = markerStart + 1

			if start >= found.videoStart || !looksLikeMP4(s.data[start:]) {
				continue
			}
			// It contains the one found if its boxes, the movie box among
			// them, run past where that one begins.
			if walk := walkMP4(s.data[start:]); !walk.moov || start+walk.length <= b.videoStart {
				continue
			}
			if validateSplitCandidate(s.data, start) == nil {
				found = boundary{photoEnd: markerStart, videoStart: start, method: marker.method}
				break
			}
		}
	}
	return found
}

// payloadStart returns where the video that the marker at markerStart
// announces begins: right after it, or after the 64-bit size that follows
// the type of an mpvd box whose size field says so. That is only taken to be
// the case if the size is one the box can have and a video does begin there.
func payloadStart(data []byte, markerStart int, magic []byte) int {
	start := markerStart + len(magic)
	if !bytes.Equal(magic, magicV2) || markerStart < 4 || len(data)-start < 8 ||
		binary.BigEndian.Uint32(data[markerStart-4:]) != 1 {
		return start
	}

	size := binary.BigEndian.Uint64(data[start:])
	if size < 16 || size > uint64(len(data)-(markerStart-4)) || !looksLikeMP4(data[start+8:]) {
		return start
	}
	return start + 8
}

// mpvdBox returns the extent of the mpvd box whose payload begins at
// payloadStart. ok is false when the bytes before it are not the header of
// such a box, or not one with a plausible size.
func mpvdBox(data []byte, payloadStart int) (start, end int, ok bool) {
	if payloadStart >= 8 && bytes.Equal(data[payloadStart-4:payloadStart], magicV2) {
		start = payloadStart - 8
		switch size := uint64(binary.BigEndian.Uint32(data[start:])); {
		case size == 0: // the box runs to the end
			return start, len(data), true
		case size >= 8 && size <= uint64(len(data)-start):
			return start, start + int(size), true
		}
		return 0, 0, false
	}

	// A header whose size field is 1 is followed by the size in 64 bits.
	if payloadStart >= 16 && bytes.Equal(data[payloadStart-12:payloadStart-8], magicV2) &&
		binary.BigEndian.Uint32(data[payloadStart-16:]) == 1 {
		start = payloadStart - 16
		if size := binary.BigEndian.Uint64(data[payloadStart-8:]); size >= 16 && size <= uint64(len(data)-start) {
			return start, start + int(size), true
		}
	}

	return 0, 0, false
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
