package extractor

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestExtractFileExtractsOnlyVideoWhenPhotoSkipped(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	output := filepath.Join(tempDir, "out")
	writeMotionPhotoFixture(t, input)

	res, err := ExtractFile(input, Options{OutputDir: output, SkipPhoto: true})
	if err != nil {
		t.Fatalf("ExtractFile() error = %v", err)
	}

	want := Result{
		VideoPath:    filepath.Join(output, "sample_video.mp4"),
		OriginalPath: input,
		Method:       MethodMetadata,
	}
	assertResult(t, res, want)
	assertFileDoesNotExist(t, filepath.Join(output, "sample_photo.jpg"))
	assertFileExists(t, want.VideoPath)
}

func TestExtractFileExtractsOnlyPhotoWhenVideoSkipped(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	output := filepath.Join(tempDir, "out")
	writeMotionPhotoFixture(t, input)

	if _, err := ExtractFile(input, Options{OutputDir: output, SkipVideo: true}); err != nil {
		t.Fatalf("ExtractFile() error = %v", err)
	}

	assertFileExists(t, filepath.Join(output, "sample_photo.jpg"))
	assertFileDoesNotExist(t, filepath.Join(output, "sample_video.mp4"))
}

func TestExtractFileReturnsErrorWhenBothComponentsSkipped(t *testing.T) {
	input := filepath.Join(t.TempDir(), "sample.jpg")
	writeMotionPhotoFixture(t, input)

	_, err := ExtractFile(input, Options{SkipPhoto: true, SkipVideo: true})
	if !errors.Is(err, ErrNothingToExtract) {
		t.Fatalf("ExtractFile() error = %v, want ErrNothingToExtract", err)
	}
}

func TestExtractFileRejectsUnsupportedExtension(t *testing.T) {
	input := filepath.Join(t.TempDir(), "sample.png")
	writeMotionPhotoFixture(t, input)

	_, err := ExtractFile(input, Options{})
	if !errors.Is(err, ErrUnsupportedExtension) {
		t.Fatalf("ExtractFile() error = %v, want ErrUnsupportedExtension", err)
	}
}

func TestExtractFileDefaultsToInputDirectoryAndKeepsModTime(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	writeMotionPhotoFixture(t, input)
	modTime := time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := os.Chtimes(input, modTime, modTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	res, err := ExtractFile(input, Options{})
	if err != nil {
		t.Fatalf("ExtractFile() error = %v", err)
	}

	want := Result{
		PhotoPath:    filepath.Join(tempDir, "sample_photo.jpg"),
		VideoPath:    filepath.Join(tempDir, "sample_video.mp4"),
		OriginalPath: input,
		Method:       MethodMetadata,
	}
	assertResult(t, res, want)

	for _, path := range []string{want.PhotoPath, want.VideoPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if !info.ModTime().Equal(modTime) {
			t.Fatalf("%s mod time = %v, want %v", path, info.ModTime(), modTime)
		}
	}
	assertDirEntries(t, tempDir, "sample.jpg", "sample_photo.jpg", "sample_video.mp4")
}

func TestExtractFileSanitizesPhotoAndRejectsSecondPass(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	output := filepath.Join(tempDir, "out")
	writeMotionPhotoFixture(t, input)

	if _, err := ExtractFile(input, Options{OutputDir: output}); err != nil {
		t.Fatalf("ExtractFile() error = %v", err)
	}

	photoPath := filepath.Join(output, "sample_photo.jpg")
	photoData, err := os.ReadFile(photoPath)
	if err != nil {
		t.Fatalf("read extracted photo: %v", err)
	}

	if bytes.Contains(photoData, []byte(`Item:Semantic="MotionPhoto"`)) {
		t.Fatal("extracted photo still contains motion photo semantic metadata")
	}
	if bytes.Contains(photoData, []byte(`GCamera:MotionPhoto="1"`)) {
		t.Fatal("extracted photo still advertises MotionPhoto=1")
	}
	if bytes.Contains(photoData, []byte(`GCamera:MotionPhotoOffset="24"`)) {
		t.Fatal("extracted photo still contains a non-zero motion photo offset")
	}

	secondOutput := filepath.Join(tempDir, "out-second-pass")
	_, err = ExtractFile(photoPath, Options{OutputDir: secondOutput})
	if !errors.Is(err, ErrNotMotionPhoto) {
		t.Fatalf("ExtractFile() error = %v on extracted photo, want ErrNotMotionPhoto", err)
	}

	assertFileDoesNotExist(t, filepath.Join(secondOutput, "sample_photo_photo.jpg"))
	assertFileDoesNotExist(t, filepath.Join(secondOutput, "sample_photo_video.mp4"))
}

func TestExtractFileLeavesExistingOutputsUnlessOverwrite(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	writeMotionPhotoFixture(t, input)
	photoPath := filepath.Join(tempDir, "sample_photo.jpg")
	if err := os.WriteFile(photoPath, []byte("existing"), 0644); err != nil {
		t.Fatalf("write existing photo: %v", err)
	}

	res, err := ExtractFile(input, Options{})
	if err != nil {
		t.Fatalf("ExtractFile() error = %v", err)
	}
	assertResult(t, res, Result{
		VideoPath:    filepath.Join(tempDir, "sample_video.mp4"),
		OriginalPath: input,
		Skipped:      []string{photoPath},
		Method:       MethodMetadata,
	})
	assertFileContent(t, photoPath, []byte("existing"))

	res, err = ExtractFile(input, Options{Overwrite: true})
	if err != nil {
		t.Fatalf("ExtractFile() with Overwrite error = %v", err)
	}
	if res.PhotoPath != photoPath || len(res.Skipped) != 0 {
		t.Fatalf("ExtractFile() with Overwrite = %+v, want photo written and nothing skipped", res)
	}
	if data, _ := os.ReadFile(photoPath); bytes.Equal(data, []byte("existing")) {
		t.Fatal("existing photo was not overwritten")
	}
}

// With RenameOriginal and no separate output directory the photo takes the
// input's name. The input must end up as _original, not be overwritten.
func TestExtractFileRenameOriginalInPlace(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	writeMotionPhotoFixture(t, input)
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	res, err := ExtractFile(input, Options{RenameOriginal: true})
	if err != nil {
		t.Fatalf("ExtractFile() error = %v", err)
	}

	want := Result{
		PhotoPath:    input,
		VideoPath:    filepath.Join(tempDir, "sample.mp4"),
		OriginalPath: filepath.Join(tempDir, "sample_original.jpg"),
		Method:       MethodMetadata,
	}
	assertResult(t, res, want)
	assertDirEntries(t, tempDir, "sample.jpg", "sample.mp4", "sample_original.jpg")
	assertFileContent(t, want.OriginalPath, original)

	if _, err := ExtractFile(input, Options{}); !errors.Is(err, ErrNotMotionPhoto) {
		t.Fatalf("ExtractFile() on extracted photo error = %v, want ErrNotMotionPhoto", err)
	}
}

func TestExtractFileRenameOriginalRefusesToClobberEarlierOriginal(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	writeMotionPhotoFixture(t, input)
	earlier := filepath.Join(tempDir, "sample_original.jpg")
	if err := os.WriteFile(earlier, []byte("earlier"), 0644); err != nil {
		t.Fatalf("write earlier original: %v", err)
	}

	if _, err := ExtractFile(input, Options{RenameOriginal: true}); err == nil {
		t.Fatal("ExtractFile() error = nil, want non-nil")
	}

	assertDirEntries(t, tempDir, "sample.jpg", "sample_original.jpg")
	assertFileContent(t, earlier, []byte("earlier"))
}

// Overwrite replaces outputs, which can be extracted again. An earlier
// original cannot, so it is only replaced by the same content.
func TestExtractFileOverwriteKeepsADifferentEarlierOriginal(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	writeMotionPhotoFixture(t, input)
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	earlier := filepath.Join(tempDir, "sample_original.jpg")
	if err := os.WriteFile(earlier, []byte("earlier"), 0644); err != nil {
		t.Fatalf("write earlier original: %v", err)
	}

	opts := Options{RenameOriginal: true, Overwrite: true}
	if _, err := ExtractFile(input, opts); err == nil {
		t.Fatal("ExtractFile() error = nil, want non-nil")
	}
	assertDirEntries(t, tempDir, "sample.jpg", "sample_original.jpg")
	assertFileContent(t, input, original)
	assertFileContent(t, earlier, []byte("earlier"))

	if err := os.WriteFile(earlier, original, 0644); err != nil {
		t.Fatalf("write identical original: %v", err)
	}
	if _, err := ExtractFile(input, opts); err != nil {
		t.Fatalf("ExtractFile() with identical earlier original error = %v", err)
	}
	assertDirEntries(t, tempDir, "sample.jpg", "sample.mp4", "sample_original.jpg")
	assertFileContent(t, earlier, original)
}

// An output that already exists may come from another file, so the input is
// not deleted for it.
func TestExtractFileDeleteOriginalKeepsInputWhenOutputSkipped(t *testing.T) {
	t.Run("separate outputs", func(t *testing.T) {
		tempDir := t.TempDir()
		input := filepath.Join(tempDir, "sample.jpg")
		writeMotionPhotoFixture(t, input)
		videoPath := filepath.Join(tempDir, "sample_video.mp4")
		if err := os.WriteFile(videoPath, []byte("existing"), 0644); err != nil {
			t.Fatalf("write existing video: %v", err)
		}

		res, err := ExtractFile(input, Options{DeleteOriginal: true})
		if err != nil {
			t.Fatalf("ExtractFile() error = %v", err)
		}
		assertResult(t, res, Result{
			PhotoPath:    filepath.Join(tempDir, "sample_photo.jpg"),
			OriginalPath: input,
			Skipped:      []string{videoPath},
			Method:       MethodMetadata,
		})
		assertDirEntries(t, tempDir, "sample.jpg", "sample_photo.jpg", "sample_video.mp4")
		assertFileContent(t, videoPath, []byte("existing"))

		res, err = ExtractFile(input, Options{DeleteOriginal: true, Overwrite: true})
		if err != nil {
			t.Fatalf("ExtractFile() with Overwrite error = %v", err)
		}
		if res.OriginalPath != "" {
			t.Fatalf("OriginalPath with Overwrite = %q, want empty", res.OriginalPath)
		}
		assertDirEntries(t, tempDir, "sample_photo.jpg", "sample_video.mp4")
	})

	// The photo would take the input's place, which deletes it just as well.
	// The input is moved aside instead, as without DeleteOriginal.
	t.Run("photo replaces input", func(t *testing.T) {
		tempDir := t.TempDir()
		input := filepath.Join(tempDir, "sample.jpg")
		writeMotionPhotoFixture(t, input)
		original, err := os.ReadFile(input)
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		videoPath := filepath.Join(tempDir, "sample.mp4")
		if err := os.WriteFile(videoPath, []byte("existing"), 0644); err != nil {
			t.Fatalf("write existing video: %v", err)
		}

		res, err := ExtractFile(input, Options{DeleteOriginal: true, RenameOriginal: true})
		if err != nil {
			t.Fatalf("ExtractFile() error = %v", err)
		}
		want := Result{
			PhotoPath:    input,
			OriginalPath: filepath.Join(tempDir, "sample_original.jpg"),
			Skipped:      []string{videoPath},
			Method:       MethodMetadata,
		}
		assertResult(t, res, want)
		assertFileContent(t, want.OriginalPath, original)
		assertFileContent(t, videoPath, []byte("existing"))
	})
}

func TestTargets(t *testing.T) {
	input := filepath.Join("photos", "sample.jpg")

	tests := []struct {
		name string
		opts Options
		want []string
	}{
		{
			name: "next to the input",
			want: []string{filepath.Join("photos", "sample_photo.jpg"), filepath.Join("photos", "sample_video.mp4")},
		},
		{
			name: "output directory without photo",
			opts: Options{OutputDir: "out", SkipPhoto: true},
			want: []string{filepath.Join("out", "sample_video.mp4")},
		},
		{
			name: "rename original",
			opts: Options{OutputDir: "out", RenameOriginal: true},
			want: []string{
				filepath.Join("out", "sample.jpg"),
				filepath.Join("out", "sample.mp4"),
				filepath.Join("out", "sample_original.jpg"),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Targets(input, tc.opts); !slices.Equal(got, tc.want) {
				t.Fatalf("Targets() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractFileDeleteOriginal(t *testing.T) {
	t.Run("separate outputs", func(t *testing.T) {
		tempDir := t.TempDir()
		input := filepath.Join(tempDir, "sample.jpg")
		writeMotionPhotoFixture(t, input)

		res, err := ExtractFile(input, Options{DeleteOriginal: true})
		if err != nil {
			t.Fatalf("ExtractFile() error = %v", err)
		}
		if res.OriginalPath != "" {
			t.Fatalf("OriginalPath = %q, want empty", res.OriginalPath)
		}
		assertDirEntries(t, tempDir, "sample_photo.jpg", "sample_video.mp4")
	})

	t.Run("photo replaces input", func(t *testing.T) {
		tempDir := t.TempDir()
		input := filepath.Join(tempDir, "sample.jpg")
		writeMotionPhotoFixture(t, input)

		res, err := ExtractFile(input, Options{DeleteOriginal: true, RenameOriginal: true})
		if err != nil {
			t.Fatalf("ExtractFile() error = %v", err)
		}
		if res.PhotoPath != input || res.OriginalPath != "" {
			t.Fatalf("ExtractFile() = %+v, want photo at %s and no original", res, input)
		}
		assertDirEntries(t, tempDir, "sample.jpg", "sample.mp4")

		if _, err := ExtractFile(input, Options{}); !errors.Is(err, ErrNotMotionPhoto) {
			t.Fatalf("ExtractFile() on extracted photo error = %v, want ErrNotMotionPhoto", err)
		}
	})
}

func TestExtractFileLeavesNothingBehindWhenNotAMotionPhoto(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "plain.jpg")
	if err := os.WriteFile(input, buildTrailingMPVDFalsePositive(), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err := ExtractFile(input, Options{OutputDir: filepath.Join(tempDir, "out"), DeleteOriginal: true})
	if !errors.Is(err, ErrNotMotionPhoto) {
		t.Fatalf("ExtractFile() error = %v, want ErrNotMotionPhoto", err)
	}
	assertDirEntries(t, tempDir, "plain.jpg")
}

func TestProcessStillWorks(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	output := filepath.Join(tempDir, "out")
	writeMotionPhotoFixture(t, input)

	if err := New().Process(input, output, false, false, false, true, false); err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	assertFileDoesNotExist(t, filepath.Join(output, "sample_photo.jpg"))
	assertFileExists(t, filepath.Join(output, "sample_video.mp4"))
}

func writeMotionPhotoFixture(t *testing.T, path string) {
	t.Helper()
	_, data := buildMotionPhotoFixture(minimalMP4Data, len(minimalMP4Data), true)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func assertResult(t *testing.T, got, want Result) {
	t.Helper()
	if got.PhotoPath != want.PhotoPath || got.VideoPath != want.VideoPath ||
		got.OriginalPath != want.OriginalPath || got.Method != want.Method ||
		!slices.Equal(got.Skipped, want.Skipped) {
		t.Fatalf("ExtractFile() = %+v, want %+v", got, want)
	}
}

func assertDirEntries(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("entries of %s = %q, want %q", dir, got, want)
	}
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("content of %s = %q, want %q", path, got, want)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s to exist: %v", path, err)
	}
}

func assertFileDoesNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file %s not to exist, got err=%v", path, err)
	}
}
