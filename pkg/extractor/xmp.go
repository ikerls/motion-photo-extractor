package extractor

import "bytes"

var (
	xmpStartTag = []byte("<x:xmpmeta")
	xmpEndTag   = []byte("</x:xmpmeta>")

	motionPhotoSemantic = []byte(`Item:Semantic="MotionPhoto"`)
	stillImageSemantic  = []byte(`Item:Semantic="Still_Image"`)
	lengthAttrPrefix    = []byte(`Item:Length="`)

	motionPhotoEnabledAttrs = [][2][]byte{
		{[]byte(`GCamera:MotionPhoto="1"`), []byte(`GCamera:MotionPhoto="0"`)},
		{[]byte(`Camera:MotionPhoto="1"`), []byte(`Camera:MotionPhoto="0"`)},
		{[]byte(`GCamera:MicroVideo="1"`), []byte(`GCamera:MicroVideo="0"`)},
		{[]byte(`Camera:MicroVideo="1"`), []byte(`Camera:MicroVideo="0"`)},
	}

	motionPhotoOffsetPrefixes = [][]byte{
		[]byte(`GCamera:MotionPhotoOffset="`),
		[]byte(`Camera:MotionPhotoOffset="`),
		[]byte(`GCamera:MicroVideoOffset="`),
		[]byte(`Camera:MicroVideoOffset="`),
	}
)

const xmpSearchLimit = 512 << 10

// SanitizePhoto rewrites the XMP metadata of an extracted photo so it no
// longer advertises an embedded video. Without this, the photo would still be
// detected as a motion photo by galleries and by this package.
//
// The rewrite happens in place and never changes the length of photo.
func SanitizePhoto(photo []byte) {
	start, end, ok := findXMPBlock(headerSearchArea(photo))
	if !ok {
		return
	}

	xmp := photo[start:end]
	for _, replacement := range motionPhotoEnabledAttrs {
		replaceAllSameLength(xmp, replacement[0], replacement[1])
	}
	replaceAllSameLength(xmp, motionPhotoSemantic, stillImageSemantic)
	for _, prefix := range motionPhotoOffsetPrefixes {
		zeroAttributeDigits(xmp, prefix)
	}
}

func findMotionPhotoVideoLength(data []byte) (int, bool) {
	searchArea := headerSearchArea(data)
	if start, end, ok := findXMPBlock(searchArea); ok {
		searchArea = searchArea[start:end]
	}

	if length, ok := findMotionPhotoItemLength(searchArea); ok {
		return length, true
	}

	if offset, ok := findFirstIntAttribute(searchArea, motionPhotoOffsetPrefixes...); ok {
		return offset, true
	}

	return 0, false
}

func headerSearchArea(data []byte) []byte {
	return data[:min(len(data), xmpSearchLimit)]
}

func findXMPBlock(data []byte) (int, int, bool) {
	start := bytes.Index(data, xmpStartTag)
	if start == -1 {
		return 0, 0, false
	}

	end := bytes.Index(data[start:], xmpEndTag)
	if end == -1 {
		return 0, 0, false
	}

	end += start + len(xmpEndTag)
	return start, end, true
}

func findMotionPhotoItemLength(data []byte) (int, bool) {
	searchStart := 0
	for {
		semanticIndex := bytes.Index(data[searchStart:], motionPhotoSemantic)
		if semanticIndex == -1 {
			return 0, false
		}
		semanticIndex += searchStart

		tagStart := bytes.LastIndexByte(data[:semanticIndex], '<')
		tagEnd := bytes.IndexByte(data[semanticIndex:], '>')
		if tagStart != -1 && tagEnd != -1 {
			tag := data[tagStart : semanticIndex+tagEnd+1]
			if length, ok := findIntAttribute(tag, lengthAttrPrefix); ok && length > 0 {
				return length, true
			}
		}

		searchStart = semanticIndex + len(motionPhotoSemantic)
	}
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

	if end == start || end >= len(data) || data[end] != '"' {
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
