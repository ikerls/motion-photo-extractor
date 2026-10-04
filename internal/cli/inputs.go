package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"

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
//
// The error tells where the looking failed. The directories that were found
// all the same are returned along with it.
func outputDirs(outputDir string, inputs []string) ([]string, error) {
	var (
		dirs []string
		errs []error
		seen = make(map[string]bool)
	)
	add := func(dir string) {
		// Files are written to the path as cleaned, wherever a link in it
		// followed by .. would lead.
		dir = filepath.Clean(dir)
		// One that cannot be looked at is kept, to be reported when read.
		if !mayBeDir(dir) {
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

	// addAsWritten adds a directory that an input designates if it is one as
	// it is written. A path such as missing/.. leads nowhere, whatever
	// cleaning makes of it, and no file is found through it. Nor is one
	// through a path that cannot be followed, which is an error.
	addAsWritten := func(dir string) {
		info, err := os.Stat(dir)
		switch {
		case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		case err != nil:
			errs = append(errs, err)
		case info.IsDir():
			add(dir)
		}
	}

	if outputDir != "" {
		// What is there in place of the directory is not one to skip. The
		// path is the one files are written to, as below.
		outputDir = filepath.Clean(outputDir)
		if info, err := os.Stat(outputDir); err == nil && !info.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", outputDir)
		}
		add(outputDir)
		return dirs, nil
	}

	for _, input := range inputs {
		info, err := os.Stat(input)
		_, isRegex := regexInput(input)
		switch {
		case err == nil && info.IsDir():
			// As written, like the search for its files: the directories
			// below it are those of where the path leads.
			walkDirs(input, add)
		case err == nil:
			add(filepath.Dir(input))
		case isRegex:
			add(".")
		case containsGlob(input):
			// The directories a pattern designates do not depend on which
			// files it matches in them.
			parents, err := globDirs(input)
			if err != nil {
				errs = append(errs, err)
			}
			for _, parent := range parents {
				addAsWritten(parent)
			}
		default:
			// A file that is not there may have been, in a directory that is.
			_, parent := cleanGlobDir(dirPart(input))
			addAsWritten(parent)
		}
	}
	return dirs, errors.Join(errs...)
}

// mayBeDir reports whether path is a directory, or cannot be looked at to
// tell. It is not one if there is nothing at path, or a file at or above it.
func mayBeDir(path string) bool {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return false
	}
	return err != nil || info.IsDir()
}

// dirPart returns the part of path that names its directory, as written.
func dirPart(path string) string {
	dir, _ := filepath.Split(path)
	return dir
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

// globDirs returns the directories in which filepath.Glob looks for the
// files that pattern matches. They are found by Glob itself, so that they
// are the same in every case. Unlike Glob, globDirs tells of a directory
// that could not be read, in which more of them may be. What was found is
// returned along with the error.
func globDirs(pattern string) ([]string, error) {
	// A pattern that Glob turns down designates nothing. Glob is asked
	// itself: it finds some mistakes only when it comes to match a name.
	if _, err := filepath.Glob(pattern); err != nil {
		return nil, fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
	}

	prefix, dir := cleanGlobDir(dirPart(pattern))
	if !hasGlobMeta(dir[prefix:]) {
		return []string{dir}, nil
	}
	matches, err := filepath.Glob(dir)
	if err != nil {
		return nil, fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
	}
	return matches, hiddenGlobErrors(dir)
}

// hiddenGlobErrors returns what kept filepath.Glob(pattern) from reading a
// directory it looked for matches in, which it says nothing about. The
// pattern is taken apart the way Glob does.
func hiddenGlobErrors(pattern string) error {
	prefix, dir := cleanGlobDir(dirPart(pattern))

	var errs []error
	parents := []string{dir}
	if hasGlobMeta(dir[prefix:]) {
		if dir == pattern {
			return nil
		}
		errs = append(errs, hiddenGlobErrors(dir))
		parents, _ = filepath.Glob(dir)
	}

	for _, parent := range parents {
		// What is not a directory holds nothing that could match.
		if !mayBeDir(parent) {
			continue
		}
		file, err := os.Open(parent)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		file.Close()
	}
	return errors.Join(errs...)
}

// cleanGlobDir prepares the directory part of a pattern, as filepath.Split
// returns it, the way filepath.Glob does: but for a root, the separator it
// ends with is dropped, and nothing else about it is changed. prefix is the
// length of what begins it and is no pattern, whatever it is made of: the
// name of a Windows volume, such as \\?\C:.
func cleanGlobDir(dir string) (prefix int, cleaned string) {
	volume := len(filepath.VolumeName(dir))
	switch {
	case dir == "":
		return 0, "."
	case volume+1 == len(dir) && os.IsPathSeparator(dir[len(dir)-1]):
		return volume + 1, dir // a root
	case volume == len(dir) && len(dir) == 2:
		return volume, dir + "." // a drive, as in C:
	default:
		return min(volume, len(dir)-1), dir[:len(dir)-1]
	}
}

// hasGlobMeta reports whether path is more than a literal name to a glob
// pattern: besides the characters that match several names, a backslash
// stands for the character after it, except on Windows where it separates
// directories.
func hasGlobMeta(path string) bool {
	if runtime.GOOS != "windows" && strings.Contains(path, `\`) {
		return true
	}
	return containsGlob(path)
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
