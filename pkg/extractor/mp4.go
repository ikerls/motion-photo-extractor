package extractor

import "encoding/binary"

func looksLikeMP4(data []byte) bool {
	const (
		maxBoxesToScan  = 4
		maxBytesToSniff = 4096
	)

	sniffLimit := min(len(data), maxBytesToSniff)

	offset := 0
	for boxesSeen := 0; boxesSeen < maxBoxesToScan && offset+8 <= sniffLimit; boxesSeen++ {
		boxSize, headerSize, boxType, ok := readMP4BoxHeader(data[offset:])
		if !ok {
			return false
		}

		if boxSize < headerSize || offset+boxSize > len(data) {
			return false
		}

		if boxType == "ftyp" {
			return boxSize >= 16
		}

		if !isAllowedLeadingMP4Box(boxType) || offset+boxSize > sniffLimit {
			return false
		}

		offset += boxSize
	}

	return false
}

// mp4Walk is what following the top-level boxes of an MP4 found.
type mp4Walk struct {
	// length is what the boxes span that follow one another.
	length int
	// moov and media tell whether the movie box and media data, an mdat
	// box, are among them.
	moov, media bool
	// cut is set when the walk ended at what may be a box that runs past
	// the end of the data.
	cut bool
}

// whole reports whether the boxes make up a video with nothing missing.
func (w mp4Walk) whole() bool {
	return w.moov && w.media && !w.cut
}

// mp4Length returns how much of data is the MP4 it starts with. What follows
// the video in a motion photo is left out, provided the video is whole: if
// its boxes cannot be followed to both a moov box and the media data, or one
// of them is cut short, all of data is kept.
func mp4Length(data []byte) int {
	if walk := walkMP4(data); walk.whole() {
		return walk.length
	}
	return len(data)
}

// walkMP4 follows the top-level boxes data starts with.
func walkMP4(data []byte) mp4Walk {
	var walk mp4Walk
	for len(data)-walk.length >= 8 {
		box := data[walk.length:]
		if !isPrintableBoxType(box[4:8]) {
			break
		}

		size := uint64(binary.BigEndian.Uint32(box[:4]))
		switch {
		case size == 0: // the box runs to the end
			size = uint64(len(box))
		case size == 1 && len(box) >= 16:
			size = binary.BigEndian.Uint64(box[8:16])
			if size < 16 {
				return walk
			}
		case size < 8:
			return walk
		}
		if size > uint64(len(box)) {
			walk.cut = true
			return walk
		}

		switch string(box[4:8]) {
		case "moov":
			walk.moov = true
		case "mdat":
			walk.media = true
		}
		walk.length += int(size)
	}

	return walk
}

// isPrintableBoxType reports whether boxType may be the type of a box. Types
// are four printable characters, which need not be letters or digits; the
// binary data that follows a video is told apart by not being so.
func isPrintableBoxType(boxType []byte) bool {
	for _, b := range boxType {
		if b < 0x20 || b > 0x7E {
			return false
		}
	}
	return true
}

func readMP4BoxHeader(data []byte) (boxSize int, headerSize int, boxType string, ok bool) {
	if len(data) < 8 {
		return 0, 0, "", false
	}

	boxTypeBytes := data[4:8]
	if !isASCIIBoxType(boxTypeBytes) {
		return 0, 0, "", false
	}

	size := binary.BigEndian.Uint32(data[:4])
	headerSize = 8

	switch size {
	case 0:
		return 0, 0, "", false
	case 1:
		if len(data) < 16 {
			return 0, 0, "", false
		}
		largeSize := binary.BigEndian.Uint64(data[8:16])
		if largeSize > uint64(len(data)) || largeSize < 16 {
			return 0, 0, "", false
		}
		boxSize = int(largeSize)
		headerSize = 16
	default:
		if size < 8 {
			return 0, 0, "", false
		}
		boxSize = int(size)
	}

	return boxSize, headerSize, string(boxTypeBytes), true
}

func isASCIIBoxType(boxType []byte) bool {
	for _, b := range boxType {
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == ' ' {
			continue
		}
		return false
	}
	return true
}

func isAllowedLeadingMP4Box(boxType string) bool {
	switch boxType {
	case "free", "skip", "wide", "uuid":
		return true
	default:
		return false
	}
}
