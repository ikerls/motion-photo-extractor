package extractor

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
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

// The output directory may be on another filesystem, where the original
// cannot be renamed to. It is copied there instead.
func TestExtractFileRenameOriginalAcrossFilesystems(t *testing.T) {
	// Spelled out rather than taken from errCrossDevice, to check that one.
	crossDevice := syscall.EXDEV
	if runtime.GOOS == "windows" {
		crossDevice = syscall.Errno(17) // ERROR_NOT_SAME_DEVICE
	}

	type fixture struct {
		input, output string
		info          os.FileInfo
		original      []byte
		opts          Options
	}
	setup := func(t *testing.T) fixture {
		tempDir := t.TempDir()
		f := fixture{
			input:  filepath.Join(tempDir, "in", "sample.jpg"),
			output: filepath.Join(tempDir, "out"),
		}
		f.opts = Options{OutputDir: f.output, RenameOriginal: true}
		for _, dir := range []string{filepath.Dir(f.input), f.output} {
			if err := os.Mkdir(dir, 0755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
		}
		writeMotionPhotoFixture(t, f.input)
		modTime := time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC)
		if err := os.Chtimes(f.input, modTime, modTime); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		if err := os.Chmod(f.input, 0640); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		var err error
		if f.original, err = os.ReadFile(f.input); err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		if f.info, err = os.Stat(f.input); err != nil {
			t.Fatalf("stat fixture: %v", err)
		}

		renames := 0
		renameFile = func(oldpath, newpath string) error {
			renames++
			return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: crossDevice}
		}
		t.Cleanup(func() {
			renameFile = os.Rename
			if renames != 1 {
				t.Errorf("original renamed %d times, want 1 attempt", renames)
			}
		})
		return f
	}
	moved := func(f fixture) Result {
		return Result{
			PhotoPath:    filepath.Join(f.output, "sample.jpg"),
			VideoPath:    filepath.Join(f.output, "sample.mp4"),
			OriginalPath: filepath.Join(f.output, "sample_original.jpg"),
			Method:       MethodMetadata,
		}
	}

	t.Run("copies the original and removes the input", func(t *testing.T) {
		f := setup(t)

		res, err := ExtractFile(f.input, f.opts)
		if err != nil {
			t.Fatalf("ExtractFile() error = %v", err)
		}

		want := moved(f)
		assertResult(t, res, want)
		assertDirEntries(t, filepath.Dir(f.input))
		assertDirEntries(t, f.output, "sample.jpg", "sample.mp4", "sample_original.jpg")
		assertFileContent(t, want.OriginalPath, f.original)

		info, err := os.Stat(want.OriginalPath)
		if err != nil {
			t.Fatalf("stat original: %v", err)
		}
		if os.SameFile(info, f.info) {
			t.Fatal("original is the input renamed, want a copy")
		}
		if !info.ModTime().Equal(f.info.ModTime()) {
			t.Fatalf("original mod time = %v, want %v", info.ModTime(), f.info.ModTime())
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0640 {
			t.Fatalf("original mode = %v, want %v", info.Mode().Perm(), os.FileMode(0640))
		}
	})

	// As when the input cannot be deleted, what was written is kept. Nothing
	// that was there before is taken away to undo the extraction.
	t.Run("keeps the outputs when the input cannot be removed", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("needs a directory that files cannot be removed from")
		}
		f := setup(t)
		f.opts.Overwrite = true
		earlier := filepath.Join(f.output, "sample_original.jpg")
		if err := os.WriteFile(earlier, f.original, 0644); err != nil {
			t.Fatalf("write earlier original: %v", err)
		}
		if err := os.Chmod(filepath.Dir(f.input), 0555); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		t.Cleanup(func() { os.Chmod(filepath.Dir(f.input), 0755) })

		res, err := ExtractFile(f.input, f.opts)
		if err == nil {
			t.Fatal("ExtractFile() error = nil, want non-nil")
		}
		want := moved(f)
		want.OriginalPath = f.input
		assertResult(t, res, want)
		assertDirEntries(t, filepath.Dir(f.input), "sample.jpg")
		assertFileContent(t, f.input, f.original)
		assertDirEntries(t, f.output, "sample.jpg", "sample.mp4", "sample_original.jpg")
		assertFileContent(t, earlier, f.original)
	})

	// Undoing a failed extraction removes the copy of the original, but not
	// an earlier original that was already there with the same content.
	t.Run("keeps an earlier original when the photo cannot be written", func(t *testing.T) {
		f := setup(t)
		f.opts.Overwrite = true
		earlier := filepath.Join(f.output, "sample_original.jpg")
		if err := os.WriteFile(earlier, f.original, 0644); err != nil {
			t.Fatalf("write earlier original: %v", err)
		}
		// A directory in the photo's place cannot be replaced by it.
		if err := os.MkdirAll(filepath.Join(f.output, "sample.jpg", "sub"), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		if _, err := ExtractFile(f.input, f.opts); err == nil {
			t.Fatal("ExtractFile() error = nil, want non-nil")
		}
		assertDirEntries(t, filepath.Dir(f.input), "sample.jpg")
		assertFileContent(t, f.input, f.original)
		assertDirEntries(t, f.output, "sample.jpg", "sample_original.jpg")
		assertFileContent(t, earlier, f.original)
	})

	t.Run("undoes the copy when the photo cannot be written", func(t *testing.T) {
		f := setup(t)
		f.opts.Overwrite = true
		if err := os.MkdirAll(filepath.Join(f.output, "sample.jpg", "sub"), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		if _, err := ExtractFile(f.input, f.opts); err == nil {
			t.Fatal("ExtractFile() error = nil, want non-nil")
		}
		assertDirEntries(t, filepath.Dir(f.input), "sample.jpg")
		assertFileContent(t, f.input, f.original)
		assertDirEntries(t, f.output, "sample.jpg")
	})

	// A file that looks like a leftover staging file must not be written
	// through: as a link to the input, that would empty the input before it
	// is copied. It is not this call's to remove either.
	t.Run("does not write through a leftover staging link", func(t *testing.T) {
		f := setup(t)
		if err := os.Symlink(f.input, filepath.Join(f.output, "sample_original.jpg.part")); err != nil {
			t.Skipf("cannot create symlinks: %v", err)
		}

		res, err := ExtractFile(f.input, f.opts)
		if err != nil {
			t.Fatalf("ExtractFile() error = %v", err)
		}
		assertResult(t, res, moved(f))
		assertDirEntries(t, f.output, "sample.jpg", "sample.mp4", "sample_original.jpg", "sample_original.jpg.part")
		assertFileContent(t, res.OriginalPath, f.original)
	})

	// An output that links to the input is replaced by the photo, which
	// leaves the input itself in place and still to be removed.
	t.Run("removes an input that the photo output linked to", func(t *testing.T) {
		f := setup(t)
		if err := os.Symlink(f.input, filepath.Join(f.output, "sample.jpg")); err != nil {
			t.Skipf("cannot create symlinks: %v", err)
		}

		res, err := ExtractFile(f.input, f.opts)
		if err != nil {
			t.Fatalf("ExtractFile() error = %v", err)
		}
		assertResult(t, res, moved(f))
		assertDirEntries(t, filepath.Dir(f.input))
		assertFileContent(t, res.OriginalPath, f.original)
		if info, err := os.Lstat(res.PhotoPath); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("photo = %v, err = %v, want a regular file", info, err)
		}
	})
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

// Components are staged under names of their own: a file that happens to
// have a staging name, or a link left under one, is neither written through
// nor removed.
func TestExtractFileDoesNotWriteThroughStagingNames(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	writeMotionPhotoFixture(t, input)
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	output := filepath.Join(tempDir, "out")
	if err := os.Mkdir(output, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(input, filepath.Join(output, "sample_video.mp4.part")); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}
	other := filepath.Join(output, "sample_photo.jpg.part")
	if err := os.WriteFile(other, []byte("someone's file"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	res, err := ExtractFile(input, Options{OutputDir: output})
	if err != nil {
		t.Fatalf("ExtractFile() error = %v", err)
	}
	assertFileContent(t, input, original)
	assertFileContent(t, other, []byte("someone's file"))
	assertFileContent(t, res.VideoPath, minimalMP4Data)
	if info, err := os.Lstat(res.VideoPath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("video = %v, err = %v, want a regular file", info, err)
	}
	assertDirEntries(t, output, "sample_photo.jpg", "sample_photo.jpg.part", "sample_video.mp4", "sample_video.mp4.part")
}

// A failed extraction takes back what it did, outputs that Overwrite
// replaced included: these are put back, not removed.
func TestExtractFileRestoresReplacedOutputsOnFailure(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	writeMotionPhotoFixture(t, input)
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	output := filepath.Join(tempDir, "out")
	// A directory in the photo's place cannot be replaced by it.
	if err := os.MkdirAll(filepath.Join(output, "sample_photo.jpg", "sub"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	video := filepath.Join(output, "sample_video.mp4")
	if err := os.WriteFile(video, []byte("earlier video"), 0644); err != nil {
		t.Fatalf("write earlier video: %v", err)
	}

	if _, err := ExtractFile(input, Options{OutputDir: output, Overwrite: true}); err == nil {
		t.Fatal("ExtractFile() error = nil, want non-nil")
	}
	assertFileContent(t, input, original)
	assertFileContent(t, video, []byte("earlier video"))
	assertDirEntries(t, output, "sample_photo.jpg", "sample_video.mp4")
}

// An earlier original with the same content gives way to the input, and is
// back in its place if the extraction fails after that.
func TestExtractFileRestoresAnEarlierOriginalOnFailure(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	writeMotionPhotoFixture(t, input)
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	earlier := filepath.Join(tempDir, "sample_original.jpg")
	if err := os.WriteFile(earlier, original, 0644); err != nil {
		t.Fatalf("write earlier original: %v", err)
	}
	// A directory in the video's place cannot be replaced by it.
	if err := os.Mkdir(filepath.Join(tempDir, "sample.mp4"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if _, err := ExtractFile(input, Options{RenameOriginal: true, Overwrite: true}); err == nil {
		t.Fatal("ExtractFile() error = nil, want non-nil")
	}
	assertDirEntries(t, tempDir, "sample.jpg", "sample.mp4", "sample_original.jpg")
	assertFileContent(t, input, original)
	assertFileContent(t, earlier, original)
}

func TestExtractFileReplacesOutputsWithOverwrite(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jpg")
	writeMotionPhotoFixture(t, input)
	for _, name := range []string{"sample_photo.jpg", "sample_video.mp4"} {
		if err := os.WriteFile(filepath.Join(tempDir, name), []byte("earlier"), 0644); err != nil {
			t.Fatalf("write earlier output: %v", err)
		}
	}

	res, err := ExtractFile(input, Options{Overwrite: true})
	if err != nil {
		t.Fatalf("ExtractFile() error = %v", err)
	}
	assertFileContent(t, res.VideoPath, minimalMP4Data)
	// Nothing of what was replaced is left behind.
	assertDirEntries(t, tempDir, "sample.jpg", "sample_photo.jpg", "sample_video.mp4")
}

// An output that links to the input is replaced by the photo, which leaves
// the input itself in place and still to be deleted.
func TestExtractFileDeletesAnInputThatThePhotoOutputLinkedTo(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "in", "sample.jpg")
	output := filepath.Join(tempDir, "out")
	for _, dir := range []string{filepath.Dir(input), output} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	writeMotionPhotoFixture(t, input)
	if err := os.Symlink(input, filepath.Join(output, "sample.jpg")); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}

	res, err := ExtractFile(input, Options{OutputDir: output, RenameOriginal: true, DeleteOriginal: true})
	if err != nil {
		t.Fatalf("ExtractFile() error = %v", err)
	}
	assertResult(t, res, Result{
		PhotoPath: filepath.Join(output, "sample.jpg"),
		VideoPath: filepath.Join(output, "sample.mp4"),
		Method:    MethodMetadata,
	})
	assertDirEntries(t, filepath.Dir(input))
	assertDirEntries(t, output, "sample.jpg", "sample.mp4")
	if info, err := os.Lstat(res.PhotoPath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("photo = %v, err = %v, want a regular file", info, err)
	}
}

// The input may be a link to the very file that the photo replaces. The
// link is what was given to delete, and does not survive as one to the photo.
func TestExtractFileDeletesAnInputThatLinksToThePhotoOutput(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "in", "sample.jpg")
	output := filepath.Join(tempDir, "out")
	for _, dir := range []string{filepath.Dir(input), output} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	writeMotionPhotoFixture(t, filepath.Join(output, "sample.jpg"))
	if err := os.Symlink(filepath.Join("..", "out", "sample.jpg"), input); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}

	res, err := ExtractFile(input, Options{OutputDir: output, RenameOriginal: true, DeleteOriginal: true})
	if err != nil {
		t.Fatalf("ExtractFile() error = %v", err)
	}
	if res.OriginalPath != "" {
		t.Fatalf("OriginalPath = %q, want empty", res.OriginalPath)
	}
	assertDirEntries(t, filepath.Dir(input))
	assertDirEntries(t, output, "sample.jpg", "sample.mp4")
}

// Staging adds to the name of an output, which may leave no room for it.
func TestExtractFileStagesOutputsWithLongNames(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, strings.Repeat("a", 238)+".jpg")
	_, data := buildMotionPhotoFixture(minimalMP4Data, len(minimalMP4Data), true)
	if err := os.WriteFile(input, data, 0644); err != nil {
		t.Skipf("cannot create a file with a long name: %v", err)
	}

	res, err := ExtractFile(input, Options{})
	if err != nil {
		t.Fatalf("ExtractFile() error = %v", err)
	}
	assertFileContent(t, res.VideoPath, minimalMP4Data)
	if entries, err := os.ReadDir(tempDir); err != nil || len(entries) != 3 {
		t.Fatalf("entries = %v, err = %v, want the input and its two outputs", entries, err)
	}
}

// A link moved to another directory may no longer lead to its file. The
// original that ends up there is the file's content instead.
func TestExtractFileRenameOriginalCopiesTheFileBehindALink(t *testing.T) {
	tempDir := t.TempDir()
	source := filepath.Join(tempDir, "in", "source.jpg")
	input := filepath.Join(tempDir, "in", "sample.jpg")
	output := filepath.Join(tempDir, "out")
	if err := os.Mkdir(filepath.Dir(input), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeMotionPhotoFixture(t, source)
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.Symlink("source.jpg", input); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}

	res, err := ExtractFile(input, Options{OutputDir: output, RenameOriginal: true})
	if err != nil {
		t.Fatalf("ExtractFile() error = %v", err)
	}
	if info, err := os.Lstat(res.OriginalPath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("original = %v, err = %v, want a regular file", info, err)
	}
	assertFileContent(t, res.OriginalPath, original)
	assertFileContent(t, source, original)
	assertDirEntries(t, filepath.Dir(input), "source.jpg")
	assertDirEntries(t, output, "sample.jpg", "sample.mp4", "sample_original.jpg")

	// In its own directory the link stays one, and still leads to its file.
	if err := os.Symlink("source.jpg", input); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if res, err = ExtractFile(input, Options{RenameOriginal: true}); err != nil {
		t.Fatalf("ExtractFile() in place error = %v", err)
	}
	if info, err := os.Lstat(res.OriginalPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("original = %v, err = %v, want a link", info, err)
	}
	assertFileContent(t, res.OriginalPath, original)
}

func TestExtractFileReportsAMissingFileWhateverItsExtension(t *testing.T) {
	for _, name := range []string{"missing.jpg", "missing", "missing.png"} {
		_, err := ExtractFile(filepath.Join(t.TempDir(), name), Options{})
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("ExtractFile(%s) error = %v, want os.ErrNotExist", name, err)
		}
	}
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
