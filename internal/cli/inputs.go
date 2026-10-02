package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ikerls/motion-photo-extractor/pkg/extractor"
)

// resolveInput turns one input argument into the files it designates. An
// argument is, in order of preference: an existing file or directory, a
// /regex/ matched against the current directory, or a glob pattern.
//
// explicit is true when the argument names a single file directly rather
// than being expanded.
func resolveInput(input string) (files []string, explicit bool, err error) {
	if info, err := os.Stat(input); err == nil {
		if !info.IsDir() {
			return []string{input}, true, nil
		}
		files, err := supportedFilesIn(input)
		return files, false, err
	}

	if expr, ok := regexInput(input); ok {
		pattern, err := regexp.Compile(expr)
		if err != nil {
			return nil, false, fmt.Errorf("invalid regex pattern: %w", err)
		}
		files, err := matchRegex(".", pattern)
		return files, false, err
	}

	if containsGlob(input) {
		matches, err := filepath.Glob(input)
		if err != nil {
			return nil, false, fmt.Errorf("invalid glob pattern %q: %w", input, err)
		}
		return keepSupportedFiles(matches), false, nil
	}

	// Not found; let extraction report the error for this path.
	return []string{input}, true, nil
}

func supportedFilesIn(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && extractor.SupportedExtension(path) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func matchRegex(dir string, pattern *regexp.Regexp) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && pattern.MatchString(entry.Name()) && extractor.SupportedExtension(entry.Name()) {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	return files, nil
}

func keepSupportedFiles(paths []string) []string {
	var files []string
	for _, path := range paths {
		if !extractor.SupportedExtension(path) {
			continue
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			files = append(files, path)
		}
	}
	return files
}

// regexInput returns the expression inside a /regex/ argument.
func regexInput(input string) (string, bool) {
	if len(input) >= 2 && strings.HasPrefix(input, "/") && strings.HasSuffix(input, "/") {
		return input[1 : len(input)-1], true
	}
	return "", false
}

func containsGlob(path string) bool {
	return strings.ContainsAny(path, "*?[")
}
