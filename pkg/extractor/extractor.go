package extractor

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	Overwrite bool

	// RenameOriginal gives the extracted files the input's base name
	// (IMG.jpg, IMG.mp4) and moves the input to IMG_original.jpg in the
	// output directory. Without it the outputs are IMG_photo.jpg and
	// IMG_video.mp4.
	RenameOriginal bool

	// DeleteOriginal removes the input once extraction has succeeded. Combined
	// with RenameOriginal the input is deleted instead of renamed.
	DeleteOriginal bool
}

// Result describes what ExtractFile did.
type Result struct {
	// PhotoPath and VideoPath are the files written, empty if a component was
	// not written.
	PhotoPath string
	VideoPath string

	// OriginalPath is where the input file is now: unchanged, renamed, or
	// empty if it was deleted.
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
// On error nothing is left behind and the input is untouched, with one
// exception: if the outputs were written but the input could not be deleted,
// both a Result and an error are returned.
func ExtractFile(path string, opts Options) (Result, error) {
	if opts.SkipPhoto && opts.SkipVideo {
		return Result{}, ErrNothingToExtract
	}
	if !SupportedExtension(path) {
		return Result{}, fmt.Errorf("%w: %q", ErrUnsupportedExtension, filepath.Ext(path))
	}

	info, err := os.Stat(path)
	if err != nil {
		return Result{}, err
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

	moveOriginal := opts.RenameOriginal && !opts.DeleteOriginal
	if moveOriginal && !opts.Overwrite && exists(out.original) {
		return Result{}, fmt.Errorf("renamed original already exists: %s", out.original)
	}

	var created []string
	fail := func(err error) (Result, error) {
		for _, p := range created {
			os.Remove(p)
		}
		return Result{}, err
	}

	modTime := info.ModTime()

	if writeVideo {
		staged, err := stageFile(out.video, parts.Video, modTime)
		if err == nil {
			err = os.Rename(staged, out.video)
		}
		if err != nil {
			os.Remove(staged)
			return fail(fmt.Errorf("write video: %w", err))
		}
		created = append(created, out.video)
		res.VideoPath = out.video
	}

	// The photo is staged first and only moved into place once the original
	// is out of the way, so the original is never the one being overwritten
	// while it is still the only copy.
	var stagedPhoto string
	if writePhoto {
		SanitizePhoto(parts.Photo)
		stagedPhoto, err = stageFile(out.photo, parts.Photo, modTime)
		if err != nil {
			return fail(fmt.Errorf("write photo: %w", err))
		}
		created = append(created, stagedPhoto)
	}

	if moveOriginal {
		if err := os.Rename(path, out.original); err != nil {
			return fail(fmt.Errorf("rename original: %w", err))
		}
		res.OriginalPath = out.original
	}

	if writePhoto {
		if err := os.Rename(stagedPhoto, out.photo); err != nil {
			if moveOriginal {
				os.Rename(out.original, path)
			}
			return fail(fmt.Errorf("write photo: %w", err))
		}
		res.PhotoPath = out.photo
	}

	if opts.DeleteOriginal {
		// When the photo replaced the input there is nothing left to delete.
		if !(writePhoto && photoReplacesInput) {
			if err := os.Remove(path); err != nil {
				return res, fmt.Errorf("delete original: %w", err)
			}
		}
		res.OriginalPath = ""
	}

	return res, nil
}

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
			video:    filepath.Join(outputDir, base+".mp4"),
			original: filepath.Join(outputDir, base+"_original"+ext),
		}
	}

	return outputPaths{
		photo: filepath.Join(outputDir, base+"_photo"+ext),
		video: filepath.Join(outputDir, base+"_video.mp4"),
	}
}

// stageFile writes data to a temporary sibling of path and returns its name.
// Renaming it to path completes the write.
func stageFile(path string, data []byte, modTime time.Time) (string, error) {
	staged := path + ".part"
	if err := os.WriteFile(staged, data, 0o644); err != nil {
		os.Remove(staged)
		return "", err
	}
	if err := os.Chtimes(staged, modTime, modTime); err != nil {
		os.Remove(staged)
		return "", err
	}
	return staged, nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func isSameFile(path string, info os.FileInfo) bool {
	other, err := os.Stat(path)
	return err == nil && os.SameFile(other, info)
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
