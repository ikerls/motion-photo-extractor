package extractor

import (
	"bytes"
	"fmt"
	"github.com/charmbracelet/log"
	"github.com/ikerls/motion-photos-extractor/pkg/files"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	magicV1 = []byte("MotionPhoto_Data")
	magicV2 = []byte("mpvd")
)

type Extractor struct{}

func New() *Extractor {
	return &Extractor{}
}

func (e *Extractor) Process(filename, outputDir string, deleteOrig, renameOrig, extractPhoto bool, force bool) error {
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
		deleteOrig, renameOrig, extractPhoto, force)
}

func (e *Extractor) splitContent(data []byte) (jpegData, mp4Data []byte, err error) {
	log.Debugf("Searching for motion photo marker...")

	markerIndex := bytes.Index(data, magicV2)
	markerSize := len(magicV2)

	if markerIndex == -1 {
		markerIndex = bytes.Index(data, magicV1)
		markerSize = len(magicV1)
	}

	if markerIndex == -1 {
		return nil, nil, fmt.Errorf("no motion photo marker found in file")
	}

	log.Debugf("Found marker at position: %d\n", markerIndex)
	return data[:markerIndex], data[markerIndex+markerSize:], nil
}

func (e *Extractor) writeFiles(filename, outputDir string, jpegData, mp4Data []byte, modTime time.Time,
	deleteOrig, renameOrig, extractPhoto bool, force bool) error {
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
		if _, err := os.Stat(mp4Path); err == nil {
			log.Warnf("MP4 file already exists: %s (skipping video extraction)\n", mp4Path)
			return nil
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

	log.Debugf("Writing MP4 video (%d bytes) to: %s\n", len(mp4Data), mp4Path)
	if err := files.WriteFileWithTimestamp(mp4Path, mp4Data, modTime); err != nil {
		log.Errorf("Error writing MP4 file: %v\n", err)
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
		log.Infof("- MP4 video: %s\n", mp4Path)
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
