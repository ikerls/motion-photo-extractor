package extractor

import (
	"bytes"
	"encoding/binary"
	"strings"
)

var (
	xmpStartTag = []byte("<x:xmpmeta")
	xmpEndTag   = []byte("</x:xmpmeta>")

	motionPhotoSemantics = bothQuotes(`Item:Semantic="MotionPhoto"`)
	stillImageSemantics  = bothQuotes(`Item:Semantic="Still_Image"`)
	lengthAttrPrefixes   = bothQuotes(`Item:Length="`)

	motionPhotoEnabledAttrs = bothQuotes(
		`GCamera:MotionPhoto="1"`,
		`Camera:MotionPhoto="1"`,
		`GCamera:MicroVideo="1"`,
		`Camera:MicroVideo="1"`,
	)
	motionPhotoDisabledAttrs = bothQuotes(
		`GCamera:MotionPhoto="0"`,
		`Camera:MotionPhoto="0"`,
		`GCamera:MicroVideo="0"`,
		`Camera:MicroVideo="0"`,
	)

	motionPhotoOffsetPrefixes = bothQuotes(
		`GCamera:MotionPhotoOffset="`,
		`Camera:MotionPhotoOffset="`,
		`GCamera:MicroVideoOffset="`,
		`Camera:MicroVideoOffset="`,
	)
)

// xmpSearchLimit is how far into a JPEG the metadata is looked for when its
// segments cannot be followed.
const xmpSearchLimit = 512 << 10

// bothQuotes returns attrs followed by the same attributes written with
// single quotes, which XMP allows just as well and ExifTool writes.
func bothQuotes(attrs ...string) [][]byte {
	quoted := make([][]byte, 0, 2*len(attrs))
	for _, attr := range attrs {
		quoted = append(quoted, []byte(attr))
	}
	for _, attr := range attrs {
		quoted = append(quoted, []byte(strings.ReplaceAll(attr, `"`, `'`)))
	}
	return quoted
}

// SanitizePhoto rewrites the XMP metadata of an extracted photo so it no
// longer advertises an embedded video. Without this, the photo would still be
// detected as a motion photo by galleries and by this package.
//
// The rewrite happens in place and never changes the length of photo.
func SanitizePhoto(photo []byte) {
	for _, xmp := range findXMPBlocks(metadataArea(photo)) {
		for i, enabled := range motionPhotoEnabledAttrs {
			replaceAllSameLength(xmp, enabled, motionPhotoDisabledAttrs[i])
		}
		for i, semantic := range motionPhotoSemantics {
			replaceAllSameLength(xmp, semantic, stillImageSemantics[i])
		}
		for _, prefix := range motionPhotoOffsetPrefixes {
			zeroAttributeDigits(xmp, prefix)
		}
	}
}

// findMotionPhotoVideoLengths returns what the metadata of data gives as the
// length of the video, most trusted first. There may be several: a file may
// hold more than one XMP packet, and a packet both an item length and an
// offset, of which any may be stale.
func findMotionPhotoVideoLengths(data []byte) []int {
	searchArea := metadataArea(data)

	blocks := findXMPBlocks(searchArea)
	if len(blocks) == 0 {
		blocks = [][]byte{searchArea}
	}

	var lengths []int
	for _, xmp := range blocks {
		if length, ok := findMotionPhotoItemLength(xmp); ok {
			lengths = append(lengths, length)
		}
		for _, prefix := range motionPhotoOffsetPrefixes {
			if offset, ok := findIntAttribute(xmp, prefix); ok && offset > 0 {
				lengths = append(lengths, offset)
			}
		}
	}
	return lengths
}

// metadataArea returns the part of data that may hold its XMP metadata. In a
// JPEG that is the segments before the image data. Other formats, HEIC for
// one, keep their metadata wherever they see fit.
func metadataArea(data []byte) []byte {
	if !bytes.HasPrefix(data, jpegSOI) {
		return data
	}

	offset, ok := jpegScanStart(data)
	if !ok {
		offset = max(offset, xmpSearchLimit)
	}
	return data[:min(len(data), offset)]
}

// jpegScanStart returns where the image data of a JPEG begins, after the
// segments that hold its metadata. ok is false if data is not a JPEG whose
// segments can be followed that far; offset is then how far they could be.
func jpegScanStart(data []byte) (offset int, ok bool) {
	if !bytes.HasPrefix(data, jpegSOI) {
		return 0, false
	}

	offset = len(jpegSOI)
	for len(data)-offset >= 4 && data[offset] == 0xFF {
		switch marker := data[offset+1]; marker {
		case 0xFF: // fill byte
			offset++
			continue
		case 0xDA: // start of scan: the image data follows
			return offset, true
		}

		// A segment that is not all there is not one to follow.
		size := int(binary.BigEndian.Uint16(data[offset+2:]))
		if size < 2 || size > len(data)-offset-2 {
			break
		}
		offset += 2 + size
	}

	return offset, false
}

// findXMPBlocks returns the XMP packets in data, in order.
func findXMPBlocks(data []byte) [][]byte {
	var blocks [][]byte
	for {
		start := bytes.Index(data, xmpStartTag)
		if start == -1 {
			return blocks
		}

		end := bytes.Index(data[start:], xmpEndTag)
		if end == -1 {
			return blocks
		}
		end += start + len(xmpEndTag)

		blocks = append(blocks, data[start:end])
		data = data[end:]
	}
}

func findMotionPhotoItemLength(data []byte) (int, bool) {
	for _, semantic := range motionPhotoSemantics {
		searchStart := 0
		for {
			semanticIndex := bytes.Index(data[searchStart:], semantic)
			if semanticIndex == -1 {
				break
			}
			semanticIndex += searchStart

			tagStart := bytes.LastIndexByte(data[:semanticIndex], '<')
			tagEnd := bytes.IndexByte(data[semanticIndex:], '>')
			if tagStart != -1 && tagEnd != -1 {
				tag := data[tagStart : semanticIndex+tagEnd+1]
				if length, ok := findFirstIntAttribute(tag, lengthAttrPrefixes...); ok {
					return length, true
				}
			}

			searchStart = semanticIndex + len(semantic)
		}
	}

	return 0, false
}

func findFirstIntAttribute(data []byte, prefixes ...[]byte) (int, bool) {
	for _, prefix := range prefixes {
		if value, ok := findIntAttribute(data, prefix); ok && value > 0 {
			return value, true
		}
	}
	return 0, false
}

func findIntAttribute(data, prefix []byte) (int, bool) {
	start := bytes.Index(data, prefix)
	if start == -1 {
		return 0, false
	}

	start += len(prefix)
	end := start
	for end < len(data) && data[end] >= '0' && data[end] <= '9' {
		end++
	}

	// The value ends with the quote that the prefix opened it with.
	if end == start || end >= len(data) || data[end] != prefix[len(prefix)-1] {
		return 0, false
	}

	return parsePositiveInt(data[start:end])
}

func parsePositiveInt(data []byte) (int, bool) {
	if len(data) == 0 {
		return 0, false
	}

	value := 0
	for _, digit := range data {
		if digit < '0' || digit > '9' {
			return 0, false
		}
		value = (value * 10) + int(digit-'0')
	}

	return value, true
}

func replaceAllSameLength(data, oldValue, newValue []byte) {
	if len(oldValue) != len(newValue) {
		return
	}

	searchStart := 0
	for {
		matchIndex := bytes.Index(data[searchStart:], oldValue)
		if matchIndex == -1 {
			return
		}
		matchIndex += searchStart
		copy(data[matchIndex:matchIndex+len(newValue)], newValue)
		searchStart = matchIndex + len(oldValue)
	}
}

func zeroAttributeDigits(data, prefix []byte) {
	searchStart := 0
	for {
		attrIndex := bytes.Index(data[searchStart:], prefix)
		if attrIndex == -1 {
			return
		}

		digitIndex := searchStart + attrIndex + len(prefix)
		for digitIndex < len(data) && data[digitIndex] >= '0' && data[digitIndex] <= '9' {
			data[digitIndex] = '0'
			digitIndex++
		}

		searchStart = digitIndex
	}
}
