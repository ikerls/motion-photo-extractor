package extractor

import (
	"bytes"
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

	containerItemRe     = regexp.MustCompile(`(?s)<[^>]*Item:Semantic="[^"]+"[^>]*/>`)
	semanticAttrRe      = regexp.MustCompile(`Item:Semantic="([^"]+)"`)
	lengthAttrRe        = regexp.MustCompile(`Item:Length="(\d+)"`)
	motionPhotoOffsetRe = regexp.MustCompile(`(?:GCamera|Camera):(?:MicroVideo|MotionPhoto)Offset="(\d+)"`)
)

type Extractor struct{}

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
	if videoLength, ok := findMotionPhotoVideoLength(data); ok {
		videoStart := len(data) - videoLength
		if videoStart < 0 || videoStart >= len(data) {
			return 0, "", fmt.Errorf("invalid motion photo video length: %d", videoLength)
		}
		return videoStart, "metadata", nil
	}

	if markerIndex := bytes.LastIndex(data, magicV1); markerIndex != -1 {
		return markerIndex + len(magicV1), "MotionPhoto_Data marker", nil
	}

	if markerIndex := bytes.LastIndex(data, magicV2); markerIndex != -1 {
		return markerIndex + len(magicV2), "mpvd marker", nil
	}

	return 0, "", fmt.Errorf("no motion photo metadata or marker found in file")
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
	eoiIndex := bytes.LastIndex(data, []byte{0xFF, 0xD9})
	if eoiIndex == -1 {
		return -1
	}
	return eoiIndex + 2
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
