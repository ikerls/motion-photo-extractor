package cli

import (
	"errors"
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
//
// A directory that cannot be read is passed to skipDir and left out, along
// with everything below it.
func resolveInput(input string, skipDir func(dir string, err error)) (files []string, explicit bool, err error) {
	if info, err := os.Stat(input); err == nil {
		if !info.IsDir() {
			return []string{input}, true, nil
		}
		return supportedFilesIn(input, skipDir), false, nil
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

// outputDirs returns the directories that the extraction of inputs writes
// to: outputDir or, without one, the directory of each file that inputs
// designate. These are found without looking for the files themselves, of
// which an interrupted run may have left none. Directories that do not exist
// are left out.
func outputDirs(outputDir string, inputs []string) []string {
	var dirs []string
	seen := make(map[string]bool)
	add := func(dir string) {
		info, err := os.Stat(dir)
		// One that cannot be looked at is kept, to be reported when read.
		if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.IsDir()) {
			return
		}
		key := pathKey(dir)
		if resolved, err := filepath.EvalSymlinks(key); err == nil {
			key = resolved
		}
		if !seen[key] {
			seen[key] = true
			dirs = append(dirs, dir)
		}
	}

	if outputDir != "" {
		add(outputDir)
		return dirs
	}

	for _, input := range inputs {
		info, err := os.Stat(input)
		_, isRegex := regexInput(input)
		switch {
		case err == nil && info.IsDir():
			walkDirs(input, add)
		case err == nil:
			add(filepath.Dir(input))
		case isRegex:
			add(".")
		case containsGlob(input):
			// The directories a pattern designates do not depend on which
			// files it matches in them.
			parents := []string{filepath.Dir(input)}
			if containsGlob(parents[0]) {
				parents, _ = filepath.Glob(parents[0])
			}
			for _, parent := range parents {
				add(parent)
			}
		default:
			add(filepath.Dir(input))
		}
	}
	return dirs
}

// walkDirs calls visit for dir and the directories below it, the same ones
// that supportedFilesIn searches.
func walkDirs(dir string, visit func(dir string)) {
	visit(dir)
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if entry.IsDir() {
			walkDirs(filepath.Join(dir, entry.Name()), visit)
		}
	}
}

// supportedFilesIn returns the supported files in dir and below, in lexical
// order. dir may be a symlink to a directory; those found below it are not
// followed.
func supportedFilesIn(dir string, skipDir func(dir string, err error)) []string {
	// What could be listed before the error is still processed.
	entries, err := os.ReadDir(dir)
	if err != nil {
		skipDir(dir, err)
	}

	var files []string
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		switch {
		case entry.IsDir():
			files = append(files, supportedFilesIn(path, skipDir)...)
		case extractor.SupportedExtension(path):
			files = append(files, path)
		}
	}
	return files
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

// regexInput returns the expression inside a /regex/ argument. One with a
// slash inside is a path instead, of a directory that does not exist: a file
// name has no slash to match.
func regexInput(input string) (string, bool) {
	if len(input) >= 2 && strings.HasPrefix(input, "/") && strings.HasSuffix(input, "/") {
		expr := input[1 : len(input)-1]
		return expr, !strings.Contains(expr, "/")
	}
	return "", false
}

func containsGlob(path string) bool {
	return strings.ContainsAny(path, "*?[")
}
