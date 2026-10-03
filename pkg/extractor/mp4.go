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
