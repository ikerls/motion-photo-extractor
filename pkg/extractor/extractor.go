package extractor

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

	containerItemRe     = regexp.MustCompile(`(?s)<[^>]*Item:Semantic="[^"]+"[^>]*/>`)
	semanticAttrRe      = regexp.MustCompile(`Item:Semantic="([^"]+)"`)
	lengthAttrRe        = regexp.MustCompile(`Item:Length="(\d+)"`)
	motionPhotoOffsetRe = regexp.MustCompile(`(?:GCamera|Camera):(?:MicroVideo|MotionPhoto)Offset="(\d+)"`)
	offsetValueRe       = regexp.MustCompile(`((?:GCamera|Camera):(?:MicroVideo|MotionPhoto)Offset=")(\d+)(")`)
	semanticValueRe     = regexp.MustCompile(`Item:Semantic="MotionPhoto"`)
)

type Extractor struct{}

type splitCandidate struct {
	start  int
	source string
}

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
	candidates := collectSplitCandidates(data)
	if len(candidates) == 0 {
		return 0, "", fmt.Errorf("no motion photo metadata or marker found in file")
	}

	issues := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if err := validateSplitCandidate(data, candidate.start); err != nil {
			issues = append(issues, fmt.Sprintf("%s: %v", candidate.source, err))
			continue
		}
		return candidate.start, candidate.source, nil
	}

	return 0, "", fmt.Errorf("no valid motion photo video found: %s", strings.Join(issues, "; "))
}

func collectSplitCandidates(data []byte) []splitCandidate {
	candidates := make([]splitCandidate, 0, 3)

	if videoLength, ok := findMotionPhotoVideoLength(data); ok {
		candidates = append(candidates, splitCandidate{
			start:  len(data) - videoLength,
			source: "metadata",
		})
	}

	if markerIndex := bytes.LastIndex(data, magicV1); markerIndex != -1 {
		candidates = append(candidates, splitCandidate{
			start:  markerIndex + len(magicV1),
			source: "MotionPhoto_Data marker",
		})
	}

	if markerIndex := bytes.LastIndex(data, magicV2); markerIndex != -1 {
		candidates = append(candidates, splitCandidate{
			start:  markerIndex + len(magicV2),
			source: "mpvd marker",
		})
	}

	return dedupeSplitCandidates(candidates)
}

func dedupeSplitCandidates(candidates []splitCandidate) []splitCandidate {
	seen := make(map[int]struct{}, len(candidates))
	deduped := make([]splitCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := seen[candidate.start]; ok {
			continue
		}
		seen[candidate.start] = struct{}{}
		deduped = append(deduped, candidate)
	}
	return deduped
}

func validateSplitCandidate(data []byte, start int) error {
	if start <= 0 || start >= len(data) {
		return fmt.Errorf("candidate start %d out of range", start)
	}

	if bytes.HasPrefix(data, jpegSOI) {
		if findJPEGEnd(data[:start]) == -1 {
			return fmt.Errorf("no JPEG end marker before candidate")
		}
	}

	if !looksLikeMP4(data[start:]) {
		return fmt.Errorf("candidate payload does not look like MP4")
	}

	return nil
}

func findMotionPhotoVideoLength(data []byte) (int, bool) {
	for _, item := range containerItemRe.FindAll(data, -1) {
		semantic, ok := findAttribute(item, semanticAttrRe)
		if !ok || semantic != "MotionPhoto" {
			continue
		}

		length, ok := findIntAttribute(item, lengthAttrRe)
		if ok && length > 0 {
			return length, true
		}
	}

	offset, ok := findIntAttribute(data, motionPhotoOffsetRe)
	if ok && offset > 0 {
		return offset, true
	}

	return 0, false
}

func findAttribute(data []byte, re *regexp.Regexp) (string, bool) {
	match := re.FindSubmatch(data)
	if len(match) < 2 {
		return "", false
	}
	return string(match[1]), true
}

func findIntAttribute(data []byte, re *regexp.Regexp) (int, bool) {
	value, ok := findAttribute(data, re)
	if !ok {
		return 0, false
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func findJPEGEnd(data []byte) int {
	eoiIndex := bytes.LastIndex(data, jpegEOI)
	if eoiIndex == -1 {
		return -1
	}
	return eoiIndex + 2
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
	sanitized := append([]byte(nil), data...)

	replacements := [][2][]byte{
		{[]byte(`GCamera:MotionPhoto="1"`), []byte(`GCamera:MotionPhoto="0"`)},
		{[]byte(`Camera:MotionPhoto="1"`), []byte(`Camera:MotionPhoto="0"`)},
		{[]byte(`GCamera:MicroVideo="1"`), []byte(`GCamera:MicroVideo="0"`)},
		{[]byte(`Camera:MicroVideo="1"`), []byte(`Camera:MicroVideo="0"`)},
	}

	for _, replacement := range replacements {
		sanitized = bytes.ReplaceAll(sanitized, replacement[0], replacement[1])
	}

	sanitized = semanticValueRe.ReplaceAll(sanitized, []byte(`Item:Semantic="Still_Image"`))
	sanitized = offsetValueRe.ReplaceAllFunc(sanitized, func(match []byte) []byte {
		parts := offsetValueRe.FindSubmatch(match)
		if len(parts) != 4 {
			return match
		}

		replacement := make([]byte, 0, len(match))
		replacement = append(replacement, parts[1]...)
		replacement = append(replacement, bytes.Repeat([]byte("0"), len(parts[2]))...)
		replacement = append(replacement, parts[3]...)
		return replacement
	})

	return sanitized
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
