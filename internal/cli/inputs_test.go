package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestContainsGlob(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "no glob", in: "photo.jpg", want: false},
		{name: "asterisk", in: "*.jpg", want: true},
		{name: "question", in: "IMG_?.jpg", want: true},
		{name: "character class", in: "IMG_[0-9].jpg", want: true},
		{name: "regex delimiters are not glob", in: "/IMG_\\d+\\.jpg/", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := containsGlob(tc.in)
			if got != tc.want {
				t.Fatalf("containsGlob(%q) = %t, want %t", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolveInputExistingFileIsExplicit(t *testing.T) {
	// A name full of glob characters still designates the file if it exists.
	path := filepath.Join(t.TempDir(), "IMG[1].jpg")
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	files, explicit, err := resolveInput(path, failOnSkippedDir(t))
	if err != nil {
		t.Fatalf("resolveInput() error = %v", err)
	}
	if !explicit || !slices.Equal(files, []string{path}) {
		t.Fatalf("resolveInput() = %v, explicit=%t; want [%s], explicit=true", files, explicit, path)
	}
}

func TestResolveInputDirectoryWithTrailingSlashIsNotRegex(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "sub", "a.JPG")
	if err := os.MkdirAll(filepath.Dir(want), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, path := range []string{want, filepath.Join(dir, "notes.txt")} {
		if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	files, explicit, err := resolveInput(dir+string(filepath.Separator), failOnSkippedDir(t))
	if err != nil {
		t.Fatalf("resolveInput() error = %v", err)
	}
	if explicit || !slices.Equal(files, []string{want}) {
		t.Fatalf("resolveInput() = %v, explicit=%t; want [%s], explicit=false", files, explicit, want)
	}
}

func TestResolveInputMissingFileIsLeftForExtraction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.jpg")

	files, explicit, err := resolveInput(path, failOnSkippedDir(t))
	if err != nil {
		t.Fatalf("resolveInput() error = %v", err)
	}
	if !explicit || !slices.Equal(files, []string{path}) {
		t.Fatalf("resolveInput() = %v, explicit=%t; want [%s], explicit=true", files, explicit, path)
	}
}

func TestResolveInputFollowsSymlinkToDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "photos")
	link := filepath.Join(dir, "link")
	if err := os.MkdirAll(filepath.Join(target, "sub"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}
	// A symlink inside the directory is not followed: it may lead back up.
	if err := os.Symlink(target, filepath.Join(target, "sub", "loop")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	want := []string{filepath.Join(link, "a.jpg"), filepath.Join(link, "sub", "b.heic")}
	for _, path := range want {
		if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	files, explicit, err := resolveInput(link, failOnSkippedDir(t))
	if err != nil {
		t.Fatalf("resolveInput() error = %v", err)
	}
	if explicit || !slices.Equal(files, want) {
		t.Fatalf("resolveInput() = %v, explicit=%t; want %v, explicit=false", files, explicit, want)
	}
}

func TestResolveInputLeavesOutUnreadableDirectories(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory that cannot be read")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "b-locked")
	want := []string{filepath.Join(dir, "a", "a.jpg"), filepath.Join(dir, "c.jpg")}
	for _, path := range append([]string{filepath.Join(locked, "hidden.jpg")}, want...) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0755) })

	var skipped []string
	files, _, err := resolveInput(dir, func(dir string, err error) {
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("skipDir(%s) error = %v, want a permission error", dir, err)
		}
		skipped = append(skipped, dir)
	})
	if err != nil {
		t.Fatalf("resolveInput() error = %v", err)
	}
	if !slices.Equal(files, want) || !slices.Equal(skipped, []string{locked}) {
		t.Fatalf("resolveInput() = %v, skipped %v; want %v, skipped [%s]", files, skipped, want, locked)
	}
}

// failOnSkippedDir is the skipDir of an input that is expected to be read
// in full.
func failOnSkippedDir(t *testing.T) func(string, error) {
	return func(dir string, err error) {
		t.Helper()
		t.Errorf("directory %s skipped: %v", dir, err)
	}
}
