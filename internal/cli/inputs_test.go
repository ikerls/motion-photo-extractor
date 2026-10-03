package cli

import (
	"os"
	"path/filepath"
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

	files, explicit, err := resolveInput(path)
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

	files, explicit, err := resolveInput(dir + string(filepath.Separator))
	if err != nil {
		t.Fatalf("resolveInput() error = %v", err)
	}
	if explicit || !slices.Equal(files, []string{want}) {
		t.Fatalf("resolveInput() = %v, explicit=%t; want [%s], explicit=false", files, explicit, want)
	}
}

func TestResolveInputMissingFileIsLeftForExtraction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.jpg")

	files, explicit, err := resolveInput(path)
	if err != nil {
		t.Fatalf("resolveInput() error = %v", err)
	}
	if !explicit || !slices.Equal(files, []string{path}) {
		t.Fatalf("resolveInput() = %v, explicit=%t; want [%s], explicit=true", files, explicit, path)
	}
}
