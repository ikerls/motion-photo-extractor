package extractor

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/ikerls/motion-photo-extractor/pkg/files"
)

var (
	magicV1 = []byte("MotionPhoto_Data")
	magicV2 = []byte("mpvd")
	jpegSOI = []byte{0xFF, 0xD8}
	jpegEOI = []byte{0xFF, 0xD9}

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

type Extractor struct{}

const (
	xmpSearchLimit       = 512 << 10
	markerTailSearchSize = 16 << 20
	jpegTailSearchSize   = 256 << 10
)

func New() *Extractor {
	return &Extractor{}
}

func (e *Extractor) Process(filename, outputDir string, deleteOrig, renameOrig, extractPhoto, extractVideo bool, force bool) error {
	if !extractPhoto && !extractVideo {
		return fmt.Errorf("nothing to extract: both photo and video extraction are disabled")
	}

	if err := validateExtension(filename); err != nil {
		return err
	}

	data, fileInfo, err := files.ReadFileWithInfo(filename)
	if err != nil {
		return err
	}

	log.Infof("Processing file: %s\n", filename)
	jpegData, mp4Data, err := e.splitContent(data)
	if err != nil {
		return err
	}
	if extractPhoto {
		jpegData = sanitizeExtractedPhoto(jpegData)
	}

	return e.writeFiles(filename, outputDir, jpegData, mp4Data, fileInfo.ModTime(),
		deleteOrig, renameOrig, extractPhoto, extractVideo, force)
}

func (e *Extractor) splitContent(data []byte) (jpegData, mp4Data []byte, err error) {
	log.Debugf("Searching for motion photo split point...")

	videoStart, splitSource, err := findVideoStart(data)
	if err != nil {
		return nil, nil, err
	}

	jpegEnd := findJPEGEnd(data[:videoStart])
	if jpegEnd == -1 {
		jpegEnd = videoStart
	}

	log.Debugf("Using %s split point at position: %d\n", splitSource, videoStart)
	return data[:jpegEnd], data[videoStart:], nil
}

func findVideoStart(data []byte) (int, string, error) {
	seen := make(map[int]struct{}, 3)
	issues := make([]string, 0, 3)

	if videoLength, ok := findMotionPhotoVideoLength(data); ok {
		start := len(data) - videoLength
		if err := recordCandidateIssue(data, seen, start, "metadata"); err == nil {
			return start, "metadata", nil
		} else {
			issues = append(issues, err.Error())
		}
	}

	if start, ok, issue := findMarkerCandidate(data, magicV1, "MotionPhoto_Data marker", seen); ok {
		return start, "MotionPhoto_Data marker", nil
	} else if issue != "" {
		issues = append(issues, issue)
	}

	if start, ok, issue := findMarkerCandidate(data, magicV2, "mpvd marker", seen); ok {
		return start, "mpvd marker", nil
	} else if issue != "" {
		issues = append(issues, issue)
	}

	if len(issues) == 0 {
		return 0, "", fmt.Errorf("no motion photo metadata or marker found in file")
	}

	return 0, "", fmt.Errorf("no valid motion photo video found: %s", strings.Join(issues, "; "))
}

func recordCandidateIssue(data []byte, seen map[int]struct{}, start int, source string) error {
	if _, ok := seen[start]; ok {
		return fmt.Errorf("%s: duplicate split candidate at %d", source, start)
	}
	seen[start] = struct{}{}

	if err := validateSplitCandidate(data, start); err != nil {
		return fmt.Errorf("%s: %v", source, err)
	}

	return nil
}

func findMarkerCandidate(data, magic []byte, source string, seen map[int]struct{}) (int, bool, string) {
	if len(data) < len(magic) {
		return 0, false, ""
	}

	searchStart := 0
	if len(data) > markerTailSearchSize {
		searchStart = len(data) - markerTailSearchSize
	}

	if start, ok, issue := searchMarkerRegion(data, data[searchStart:], searchStart, magic, source, seen); ok {
		return start, true, ""
	} else if issue != "" && searchStart == 0 {
		return 0, false, issue
	}

	if searchStart == 0 {
		return 0, false, ""
	}

	return searchMarkerRegion(data, data[:searchStart+len(magic)-1], 0, magic, source, seen)
}

func searchMarkerRegion(data, region []byte, base int, magic []byte, source string, seen map[int]struct{}) (int, bool, string) {
	var lastIssue string

	for len(region) >= len(magic) {
		markerIndex := bytes.LastIndex(region, magic)
		if markerIndex == -1 {
			break
		}

		start := base + markerIndex + len(magic)
		if err := recordCandidateIssue(data, seen, start, source); err == nil {
			return start, true, ""
		} else {
			lastIssue = err.Error()
		}

		region = region[:markerIndex]
	}

	return 0, false, lastIssue
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

func findMotionPhotoVideoLength(data []byte) (int, bool) {
	searchArea := headerSearchArea(data)
	if xmp, ok := xmpSearchArea(searchArea); ok {
		searchArea = xmp
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
	if len(data) > xmpSearchLimit {
		return data[:xmpSearchLimit]
	}
	return data
}

func xmpSearchArea(data []byte) ([]byte, bool) {
	start, end, ok := findXMPBlock(data)
	if !ok {
		return nil, false
	}
	return data[start:end], true
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

func findJPEGEnd(data []byte) int {
	return findJPEGEndBefore(data, len(data))
}

func findJPEGEndBefore(data []byte, limit int) int {
	if limit > len(data) {
		limit = len(data)
	}

	searchStart := 0
	if limit > jpegTailSearchSize {
		searchStart = limit - jpegTailSearchSize
	}

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

func looksLikeMP4(data []byte) bool {
	const (
		maxBoxesToScan  = 4
		maxBytesToSniff = 4096
	)

	sniffLimit := len(data)
	if sniffLimit > maxBytesToSniff {
		sniffLimit = maxBytesToSniff
	}

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

func sanitizeExtractedPhoto(data []byte) []byte {
	searchArea := headerSearchArea(data)
	start, end, ok := findXMPBlock(searchArea)
	if !ok {
		return data
	}

	xmp := data[start:end]
	for _, replacement := range motionPhotoEnabledAttrs {
		replaceAllSameLength(xmp, replacement[0], replacement[1])
	}
	replaceAllSameLength(xmp, motionPhotoSemantic, stillImageSemantic)
	for _, prefix := range motionPhotoOffsetPrefixes {
		zeroAttributeDigits(xmp, prefix)
	}

	return data
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

func (e *Extractor) writeFiles(filename, outputDir string, jpegData, mp4Data []byte, modTime time.Time,
	deleteOrig, renameOrig, extractPhoto, extractVideo bool, force bool) error {
	log.Infof("Writing files to: %s\n", outputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	jpegPath, mp4Path, origPath := files.GenerateOutputPaths(filename, outputDir, renameOrig)

	if !force {
		if extractPhoto {
			if _, err := os.Stat(jpegPath); err == nil {
				log.Warnf("JPEG file already exists: %s (skipping photo extraction)\n", jpegPath)
				extractPhoto = false
			}
		}
		if extractVideo {
			if _, err := os.Stat(mp4Path); err == nil {
				log.Warnf("MP4 file already exists: %s (skipping video extraction)\n", mp4Path)
				extractVideo = false
			}
		}
	}

	photoSuccess := false
	videoSuccess := false

	if extractPhoto {
		log.Debugf("Writing JPEG image (%d bytes) to: %s\n", len(jpegData), jpegPath)
		if err := files.WriteFileWithTimestamp(jpegPath, jpegData, modTime); err != nil {
			log.Errorf("Error writing JPEG file: %v\n", err)
		} else {
			photoSuccess = true
		}
	} else {
		photoSuccess = true // Skip photo extraction but mark as success
	}

	if extractVideo {
		log.Debugf("Writing MP4 video (%d bytes) to: %s\n", len(mp4Data), mp4Path)
		if err := files.WriteFileWithTimestamp(mp4Path, mp4Data, modTime); err != nil {
			log.Errorf("Error writing MP4 file: %v\n", err)
		} else {
			videoSuccess = true
		}
	} else {
		videoSuccess = true
	}

	if deleteOrig && photoSuccess && videoSuccess {
		if renameOrig {
			if err := os.Rename(filename, origPath); err == nil {
				os.Remove(origPath)
			}
		} else {
			os.Remove(filename)
		}
		log.Info("Original file deleted.")
	} else if renameOrig && photoSuccess && videoSuccess {
		if err := os.Rename(filename, origPath); err != nil {
			log.Errorf("Error renaming original file: %v\n", err)
		} else {
			log.Infof("Original file renamed to: %s\n", origPath)
		}
	}

	if photoSuccess && videoSuccess {
		log.Infof("\nSuccess! Files extracted.")
		if extractPhoto {
			log.Infof("- JPEG image: %s\n", jpegPath)
		}
		if extractVideo {
			log.Infof("- MP4 video: %s\n", mp4Path)
		}
		return nil
	}

	return fmt.Errorf("extraction failed")
}

func validateExtension(filename string) error {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".heic" {
		return fmt.Errorf("unsupported file extension: %s", ext)
	}
	return nil
}
