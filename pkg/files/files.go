package files

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func ReadFileWithInfo(filename string) ([]byte, os.FileInfo, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read file: %v", err)
	}

	fileInfo, err := os.Stat(filename)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get file info: %v", err)
	}

	return data, fileInfo, nil
}

func WriteFileWithTimestamp(path string, data []byte, modTime time.Time) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := file.Write(data); err != nil {
		return err
	}

	return os.Chtimes(path, modTime, modTime)
}

func GenerateOutputPaths(filename, outputDir string, renameOrig bool) (jpegPath, mp4Path, origPath string) {
	baseFilename := filepath.Base(filename)
	baseFilename = strings.TrimSuffix(baseFilename, filepath.Ext(baseFilename))
	ext := filepath.Ext(filename)

	if renameOrig {
		jpegPath = filepath.Join(outputDir, baseFilename+ext)
		mp4Path = filepath.Join(outputDir, baseFilename+".mp4")
		origPath = filepath.Join(outputDir, baseFilename+"_original"+ext)
	} else {
		jpegPath = filepath.Join(outputDir, baseFilename+"_photo"+ext)
		mp4Path = filepath.Join(outputDir, baseFilename+"_video.mp4")
	}
	return
}
