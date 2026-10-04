package extractor

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"
)

// Options controls how ExtractFile writes its output. The zero value extracts
// both components next to the input file and leaves the input untouched.
type Options struct {
	// OutputDir is where extracted files are written. It is created if
	// missing. Empty means the directory of the input file.
	OutputDir string

	// SkipPhoto and SkipVideo disable extraction of one component.
	SkipPhoto bool
	SkipVideo bool

	// Overwrite replaces existing output files. Without it, a component whose
	// output already exists is left alone and reported in Result.Skipped.
	//
	// It never replaces an earlier IMG_original.jpg, unless that file has the
	// same content as the input.
	Overwrite bool

	// RenameOriginal gives the extracted files the input's base name
	// (IMG.jpg, IMG.mp4) and moves the input to IMG_original.jpg in the
	// output directory, which may be on another filesystem. Without it the
	// outputs are IMG_photo.jpg and IMG_video.mp4.
	//
	// An input that is a link stays one when moved within its directory.
	// Moved to another, it is replaced by a copy of the file it leads to.
	RenameOriginal bool

	// DeleteOriginal removes the input once extraction has succeeded. Combined
	// with RenameOriginal the input is deleted instead of renamed.
	//
	// The input is only deleted when every requested component was written by
	// the call. If one is reported in Result.Skipped, DeleteOriginal is
	// ignored: the existing output may not come from this input.
	DeleteOriginal bool
}

// Result describes what ExtractFile did.
type Result struct {
	// PhotoPath and VideoPath are the files written, empty if a component was
	// not written.
	PhotoPath string
	VideoPath string

	// OriginalPath is where the input file is now: unchanged, renamed, or
	// empty if it was deleted. It is never empty when Skipped is not.
	OriginalPath string

	// Skipped lists outputs that already existed and were not overwritten.
	Skipped []string

	// Method is how the split point was found.
	Method Method
}

// SupportedExtension reports whether path has an extension ExtractFile
// accepts: .jpg, .jpeg or .heic, in any case.
func SupportedExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".heic":
		return true
	default:
		return false
	}
}

// ExtractFile splits the motion photo at path and writes its components
// according to opts. Outputs keep the modification time of the input.
//
// On error everything is as it was found: nothing is left behind, an output
// that Overwrite replaced is put back and the input is untouched. There is
// one exception: if the outputs were written but the input could not be
// deleted, or removed once copied elsewhere, both a Result and an error are
// returned.
//
// A process that is killed while ExtractFile is at work has no such chance
// to clean up. What it leaves behind is found by FindLeftovers and dealt
// with by Recover.
func ExtractFile(path string, opts Options) (Result, error) {
	if opts.SkipPhoto && opts.SkipVideo {
		return Result{}, ErrNothingToExtract
	}

	// A path that leads nowhere is reported as such, whatever its extension.
	info, err := os.Stat(path)
	if err != nil {
		return Result{}, err
	}
	// The input itself, which is not the file it leads to if it is a link.
	entry, err := os.Lstat(path)
	if err != nil {
		return Result{}, err
	}
	if !SupportedExtension(path) {
		return Result{}, fmt.Errorf("%w: %q", ErrUnsupportedExtension, filepath.Ext(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}

	parts, err := Split(data)
	if err != nil {
		return Result{}, err
	}

	outputDir := cmp.Or(opts.OutputDir, filepath.Dir(path))
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create output directory: %w", err)
	}

	out := planOutputs(path, outputDir, opts.RenameOriginal)
	res := Result{OriginalPath: path, Method: parts.Method}

	// With RenameOriginal and the output directory being the input's own, the
	// photo takes the input's place. That is not a pre-existing output.
	photoReplacesInput := isSameFile(out.photo, info)

	wanted := func(target string, skip, replacesInput bool) bool {
		if skip {
			return false
		}
		if opts.Overwrite || replacesInput || !exists(target) {
			return true
		}
		res.Skipped = append(res.Skipped, target)
		return false
	}
	writePhoto := wanted(out.photo, opts.SkipPhoto, photoReplacesInput)
	writeVideo := wanted(out.video, opts.SkipVideo, false)

	// An output that was already there may have been extracted from another
	// file, so it is no reason to give up the input.
	deleteOriginal := opts.DeleteOriginal && len(res.Skipped) == 0

	// An earlier original is not an output and cannot be extracted again, so
	// Overwrite only replaces it with itself.
	moveOriginal := opts.RenameOriginal && !deleteOriginal
	originalExisted := moveOriginal && exists(out.original)
	if originalExisted {
		if !opts.Overwrite {
			return Result{}, fmt.Errorf("renamed original already exists: %s", out.original)
		}
		if !hasContent(out.original, data) {
			return Result{}, fmt.Errorf("renamed original already exists with different content: %s", out.original)
		}
	}

	var done changes
	fail := func(err error) (Result, error) {
		if undoErr := done.revert(); undoErr != nil {
			err = fmt.Errorf("%w; not everything could be put back: %w", err, undoErr)
		}
		return Result{}, err
	}

	modTime := info.ModTime()

	// Both components are staged before anything is moved into place, so
	// that what can go wrong while writing them does so with nothing to
	// take back but the staged files.
	var stagedVideo, stagedPhoto string
	if writeVideo {
		if stagedVideo, err = done.stage(out.video, parts.Video, modTime); err != nil {
			return fail(fmt.Errorf("write video: %w", err))
		}
	}
	if writePhoto {
		SanitizePhoto(parts.Photo)
		if stagedPhoto, err = done.stage(out.photo, parts.Photo, modTime); err != nil {
			return fail(fmt.Errorf("write photo: %w", err))
		}
	}

	// An original that cannot be renamed, the output directory being on
	// another filesystem, is copied there. So is the file behind an input
	// that is a link: moved to another directory, the link may lead nowhere.
	// The input is then removed last: until that point, undoing the
	// extraction never involves putting it back.
	copiedOriginal := false
	if moveOriginal {
		// An earlier original is kept until the extraction is through, to
		// be put back should it fail.
		if err := done.clear(out.original); err != nil {
			return fail(fmt.Errorf("rename original: %w", err))
		}

		err := errCrossDevice
		if entry.Mode()&os.ModeSymlink == 0 || isSameFile(outputDir, dirInfo(path)) {
			err = renameFile(path, out.original)
		}
		if errors.Is(err, errCrossDevice) {
			copiedOriginal = true
			err = copyFile(path, out.original, info)
		}
		if err != nil {
			return fail(fmt.Errorf("rename original: %w", err))
		}
		if copiedOriginal {
			done.undo = append(done.undo, func() error { return removeIfThere(out.original) })
		} else {
			done.undo = append(done.undo, func() error { return os.Rename(out.original, path) })
		}
		res.OriginalPath = out.original
	}

	// The photo is only moved into place now that the original is out of the
	// way, so the original is never the one being overwritten while it is
	// still the only copy.
	if writeVideo {
		if err := done.publish(stagedVideo, out.video); err != nil {
			return fail(fmt.Errorf("write video: %w", err))
		}
		res.VideoPath = out.video
	}
	if writePhoto {
		if err := done.publish(stagedPhoto, out.photo); err != nil {
			return fail(fmt.Errorf("write photo: %w", err))
		}
		res.PhotoPath = out.photo
	}
	done.settle()

	// Unless the photo took its place, the input is still there. Should it
	// not go away, the outputs are kept, as when it cannot be deleted.
	if copiedOriginal && isSameEntry(path, entry) {
		if err := os.Remove(path); err != nil {
			res.OriginalPath = path
			return res, fmt.Errorf("remove original, copied to %s: %w", out.original, err)
		}
	}

	if deleteOriginal {
		// When the photo replaced the input there is nothing left to delete.
		// A photo that replaced a link to the input, or the file that the
		// input links to, left the input in place.
		if isSameEntry(path, entry) {
			if err := os.Remove(path); err != nil {
				return res, fmt.Errorf("delete original: %w", err)
			}
		}
		res.OriginalPath = ""
	}

	return res, nil
}

// Targets returns the paths ExtractFile may write for path with opts: the
// requested components and, with RenameOriginal, the moved original. Two
// inputs that share a target cannot both be extracted with the same opts.
func Targets(path string, opts Options) []string {
	out := planOutputs(path, cmp.Or(opts.OutputDir, filepath.Dir(path)), opts.RenameOriginal)

	var targets []string
	if !opts.SkipPhoto {
		targets = append(targets, out.photo)
	}
	if !opts.SkipVideo {
		targets = append(targets, out.video)
	}
	if out.original != "" {
		targets = append(targets, out.original)
	}
	return targets
}

// videoExtension is the extension of the extracted video.
const videoExtension = ".mp4"

type outputPaths struct {
	photo    string
	video    string
	original string
}

func planOutputs(input, outputDir string, renameOriginal bool) outputPaths {
	ext := filepath.Ext(input)
	base := strings.TrimSuffix(filepath.Base(input), ext)

	if renameOriginal {
		return outputPaths{
			photo:    filepath.Join(outputDir, base+ext),
			video:    filepath.Join(outputDir, base+videoExtension),
			original: filepath.Join(outputDir, base+originalSuffix+ext),
		}
	}

	return outputPaths{
		photo: filepath.Join(outputDir, base+"_photo"+ext),
		video: filepath.Join(outputDir, base+"_video"+videoExtension),
	}
}

// changes records what an extraction did to the filesystem, so that a failure
// leaves it as it was found.
type changes struct {
	// undo takes the changes back, when run in reverse order.
	undo []func() error
	// backups are the files that were moved out of the way, kept for undo.
	backups []string
}

// stage writes data to a new file next to target and returns its name.
// Publishing it completes the write.
func (c *changes) stage(target string, data []byte, modTime time.Time) (string, error) {
	file, err := createSibling(target, stagedSuffix, 0o644)
	if err != nil {
		return "", err
	}
	staged := file.Name()
	c.undo = append(c.undo, func() error { return removeIfThere(staged) })

	_, err = file.Write(data)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chtimes(staged, modTime, modTime)
	}
	return staged, err
}

// clear moves the file at target, if there is one, out of the way rather
// than have it overwritten: it is put back by revert or removed by settle.
func (c *changes) clear(target string) error {
	info, err := os.Lstat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", target)
	}

	file, err := createSibling(target, backupSuffix, 0o600)
	if err != nil {
		return err
	}
	backup := file.Name()
	file.Close()
	// A link is moved as such, not the file it leads to.
	if err := os.Rename(target, backup); err != nil {
		os.Remove(backup)
		return err
	}

	c.undo = append(c.undo, func() error { return os.Rename(backup, target) })
	c.backups = append(c.backups, backup)
	return nil
}

// publish moves staged to target, in place of what may be there.
func (c *changes) publish(staged, target string) error {
	if err := c.clear(target); err != nil {
		return err
	}
	if err := os.Rename(staged, target); err != nil {
		return err
	}
	c.undo = append(c.undo, func() error { return removeIfThere(target) })
	return nil
}

// revert takes every change back. The error tells what could not be, such
// as a backup that is still under its own name.
func (c *changes) revert() error {
	var errs []error
	for _, undo := range slices.Backward(c.undo) {
		if err := undo(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// removeIfThere removes the file at path. One that is no longer there, a
// staged file that was published for one, needs no removing.
func removeIfThere(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// settle makes the changes final: what was moved out of the way is removed.
func (c *changes) settle() {
	for _, backup := range c.backups {
		os.Remove(backup)
	}
}

// createSibling creates a file next to path, named after it with a random
// part and suffix. The name is new: a file that is already there, or a link
// left in its place, is never written through.
func createSibling(path, suffix string, perm os.FileMode) (*os.File, error) {
	create := func(name string) (*os.File, error) {
		for attempt := 0; ; attempt++ {
			unique := fmt.Sprintf("%s.%08x%s", name, rand.Uint32(), suffix)
			file, err := os.OpenFile(unique, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
			if err == nil || !errors.Is(err, fs.ErrExist) || attempt == 100 {
				return file, err
			}
		}
	}

	file, err := create(path)
	if err != nil {
		// The name of path may leave no room for more: a short one does.
		if short, shortErr := create(filepath.Join(filepath.Dir(path), fallbackName)); shortErr == nil {
			return short, nil
		}
	}
	return file, err
}

// renameFile moves the original. Tests replace it to stand in for an output
// directory on another filesystem.
var renameFile = os.Rename

// errCrossDevice is what os.Rename fails with when its two paths are on
// different filesystems.
var errCrossDevice = func() error {
	if runtime.GOOS == "windows" {
		return syscall.Errno(0x11) // ERROR_NOT_SAME_DEVICE
	}
	return syscall.EXDEV
}()

// copyFile copies src, which info describes, to dst along with its
// permissions and modification time. On error dst is left as it was.
func copyFile(src, dst string, info os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := createSibling(dst, stagedSuffix, 0o600)
	if err != nil {
		return err
	}
	staged := out.Name()
	_, err = io.Copy(out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(staged, info.Mode().Perm())
	}
	if err == nil {
		err = os.Chtimes(staged, info.ModTime(), info.ModTime())
	}
	if err == nil {
		err = os.Rename(staged, dst)
	}
	if err != nil {
		os.Remove(staged)
	}
	return err
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func hasContent(path string, want []byte) bool {
	got, err := os.ReadFile(path)
	return err == nil && bytes.Equal(got, want)
}

// isSameFile reports whether path leads to the file that info describes.
func isSameFile(path string, info os.FileInfo) bool {
	other, err := os.Stat(path)
	return err == nil && info != nil && os.SameFile(other, info)
}

// isSameEntry reports whether path is still the directory entry that entry
// describes, be it a file or a link, rather than something that replaced it.
func isSameEntry(path string, entry os.FileInfo) bool {
	other, err := os.Lstat(path)
	return err == nil && os.SameFile(other, entry)
}

// dirInfo describes the directory that holds path, nil if it cannot be read.
func dirInfo(path string) os.FileInfo {
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return nil
	}
	return info
}

// Extractor is the original entry point of this package.
//
// Deprecated: use ExtractFile.
type Extractor struct{}

// New returns an Extractor.
//
// Deprecated: use ExtractFile.
func New() *Extractor {
	return &Extractor{}
}

// Process extracts filename into outputDir.
//
// Deprecated: use ExtractFile, which takes Options and reports what it wrote.
func (e *Extractor) Process(filename, outputDir string, deleteOrig, renameOrig, extractPhoto, extractVideo bool, force bool) error {
	_, err := ExtractFile(filename, Options{
		OutputDir:      outputDir,
		SkipPhoto:      !extractPhoto,
		SkipVideo:      !extractVideo,
		Overwrite:      force,
		RenameOriginal: renameOrig,
		DeleteOriginal: deleteOrig,
	})
	return err
}
