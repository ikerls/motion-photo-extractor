package extractor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// The files ExtractFile works with next to its outputs are named after them:
// IMG_video.mp4.1a2b3c4d.part while a component is written, and
// IMG_video.mp4.1a2b3c4d.bak for what an output replaces until the
// extraction is through. An output whose name leaves no room for that gets
// motion-photo.1a2b3c4d.part instead.
const (
	stagedSuffix = ".part"
	backupSuffix = ".bak"
	fallbackName = "motion-photo"

	// originalSuffix is what RenameOriginal adds to the name of the input.
	originalSuffix = "_original"
)

// LeftoverKind tells what a file that an interrupted extraction left behind
// was for.
type LeftoverKind string

const (
	// LeftoverStaged is an output that was being written and never moved
	// into place. It is not the only copy of anything.
	LeftoverStaged LeftoverKind = "staged"
	// LeftoverBackup is what was moved out of the way of an output: an
	// earlier output, an earlier original, or the input itself.
	LeftoverBackup LeftoverKind = "backup"
)

// TargetState tells whether the output that a leftover belongs to is there.
type TargetState string

const (
	TargetExists  TargetState = "exists"
	TargetMissing TargetState = "missing"
	// TargetUnknown is the state of a target that could not be looked up, or
	// that the name of the leftover does not give.
	TargetUnknown TargetState = "unknown"
)

// Leftover is a file left behind by an extraction that was killed before it
// could clean up.
type Leftover struct {
	Path string
	Kind LeftoverKind

	// Target is the output the file was staged for or moved out of the way
	// of. It is empty when the name of the file does not tell.
	Target      string
	TargetState TargetState
	// TargetErr is why the target could not be looked up.
	TargetErr error

	// info describes the file itself, not what it may link to. It is nil if
	// the file could not be examined, and err tells why.
	info fs.FileInfo
	err  error
}

// FindLeftovers returns the files in dir that an interrupted extraction left
// behind, going by their names: an output that ExtractFile writes, followed
// by eight hexadecimal digits and .part or .bak. Nothing is changed.
//
// A name is all there is to go by, so a file of that name that comes from
// somewhere else is returned just the same.
func FindLeftovers(dir string) ([]Leftover, error) {
	// The directory is read under the name that its files are then reached
	// by, which is also the one ExtractFile writes to. Left as given, a path
	// such as link/.. would be listed where the link leads and its files
	// looked for next to the link.
	dir = filepath.Clean(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var found []Leftover
	for _, entry := range entries {
		target, kind, ok := parseLeftoverName(entry.Name())
		if !ok {
			continue
		}

		leftover := Leftover{Path: filepath.Join(dir, entry.Name()), Kind: kind, TargetState: TargetUnknown}
		leftover.info, leftover.err = entry.Info()
		if errors.Is(leftover.err, fs.ErrNotExist) {
			continue // gone since the directory was listed
		}

		if target != "" {
			leftover.Target = filepath.Join(dir, target)
			// A link that leads nowhere is still something at the target,
			// and only its absence tells that the target is missing.
			switch _, err := os.Lstat(leftover.Target); {
			case err == nil:
				leftover.TargetState = TargetExists
			case errors.Is(err, fs.ErrNotExist):
				leftover.TargetState = TargetMissing
			default:
				leftover.TargetErr = err
			}
		}
		found = append(found, leftover)
	}
	return found, nil
}

// parseLeftoverName returns the output that a file called name was staged
// for or moved out of the way of, which is empty for the name that stands
// in for one that is too long. ok is false if name is not that of a leftover.
func parseLeftoverName(name string) (target string, kind LeftoverKind, ok bool) {
	switch {
	case strings.HasSuffix(name, stagedSuffix):
		name, kind = strings.TrimSuffix(name, stagedSuffix), LeftoverStaged
	case strings.HasSuffix(name, backupSuffix):
		name, kind = strings.TrimSuffix(name, backupSuffix), LeftoverBackup
	default:
		return "", "", false
	}

	const randomPart = len(".1a2b3c4d")
	if len(name) <= randomPart || name[len(name)-randomPart] != '.' {
		return "", "", false
	}
	for _, digit := range name[len(name)-randomPart+1:] {
		if !strings.ContainsRune("0123456789abcdef", digit) {
			return "", "", false
		}
	}
	target = name[:len(name)-randomPart]

	if target == fallbackName {
		return "", kind, true
	}
	// A file may be called .jpg, and its outputs then .jpg and .mp4.
	if !SupportedExtension(target) && !strings.EqualFold(filepath.Ext(target), videoExtension) {
		return "", "", false
	}
	return target, kind, true
}

// RecoveryReport is what Recover did with the leftovers of a directory.
type RecoveryReport struct {
	// Removed are the staged files that were deleted.
	Removed []string
	// Restored are the backups that were put back in place.
	Restored []RestoredBackup
	// Retained are the leftovers that were left alone, for someone to look at.
	Retained []RetainedLeftover
	// MovedAside are originals that the interrupted extraction had moved
	// aside before it stopped. They are left where they are.
	MovedAside []MovedOriginal
}

// RestoredBackup is a backup that Recover put back where it came from.
type RestoredBackup struct {
	Target string
	Backup string
	// BackupRemains is set when the backup could not be removed once it was
	// restored. Target and Backup are then the same file.
	BackupRemains bool
}

// RetainedLeftover is a leftover that Recover did not touch.
type RetainedLeftover struct {
	Leftover
	// Reason says why, and what to do about it.
	Reason string
}

// MovedOriginal is an input that an interrupted extraction with
// RenameOriginal moved to Original without writing what was to take its
// place. Renaming it back to Input undoes that.
type MovedOriginal struct {
	Original string
	Input    string
}

// Empty reports whether there was nothing to recover.
func (r RecoveryReport) Empty() bool {
	return len(r.Removed)+len(r.Restored)+len(r.Retained)+len(r.MovedAside) == 0
}

// linkFile and removeFile are replaced by tests to stand in for a filesystem
// that changes or fails while Recover is at work.
var (
	linkFile   = os.Link
	removeFile = os.Remove
)

// Recover cleans up the files in dir that an interrupted extraction left
// behind, as found by FindLeftovers:
//
//   - A staged file is removed.
//   - A backup is put back if its target is missing, it is the only backup
//     of that target and it is a regular file that is not empty. Nothing
//     that appears at the target in the meantime is overwritten.
//   - Every other backup is left alone and returned along with the reason:
//     the extraction it comes from may or may not have gone through.
//
// It must not be called while an extraction is writing to dir, whose files
// it would take for leftovers. It is best effort after a process was killed,
// and makes no promise after a power loss.
//
// The report is also returned, as far as it got, along with an error.
func Recover(dir string) (RecoveryReport, error) {
	found, err := FindLeftovers(dir)
	if err != nil {
		return RecoveryReport{}, err
	}

	// Counted before anything is changed. Names that only differ in case
	// may be one target, and are taken to be.
	backups := make(map[string]int)
	for _, leftover := range found {
		if leftover.Kind == LeftoverBackup && leftover.Target != "" {
			backups[foldCase(leftover.Target)]++
		}
	}

	var (
		report RecoveryReport
		errs   []error
		hinted = make(map[string]bool)
	)
	for _, leftover := range found {
		retain := func(format string, args ...any) {
			report.Retained = append(report.Retained, RetainedLeftover{Leftover: leftover, Reason: fmt.Sprintf(format, args...)})
		}
		target := filepath.Base(leftover.Target)

		switch {
		case leftover.info == nil:
			retain("cannot be examined: %v", reason(leftover.err))

		case leftover.Target == "":
			retain("its name does not tell which output it belongs to; look at it and delete it by hand")

		case leftover.Kind == LeftoverStaged:
			if !leftover.info.Mode().IsRegular() {
				retain("not a regular file; look at it and delete it by hand")
				break
			}
			if err := removeFile(leftover.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
				retain("could not be removed: %v", reason(err))
				break
			}
			report.Removed = append(report.Removed, leftover.Path)

			if moved, ok := movedOriginal(leftover); ok && !hinted[moved.Input] {
				hinted[moved.Input] = true
				report.MovedAside = append(report.MovedAside, moved)
			}

		case leftover.TargetState == TargetExists:
			retain("%s exists; compare the two, then delete this file or rename it by hand", target)

		case leftover.TargetState != TargetMissing:
			retain("cannot tell whether %s exists: %v", target, reason(leftover.TargetErr))

		case backups[foldCase(leftover.Target)] > 1:
			retain("one of several backups of %s; look at them and rename the right one by hand", target)

		case !leftover.info.Mode().IsRegular():
			retain("not a regular file; rename it to %s by hand", target)

		case leftover.info.Size() == 0:
			retain("empty, so it may be a name that was reserved and never used; delete it, or rename it to %s by hand", target)

		default:
			// A link, unlike a rename, is not made over what may have
			// appeared at the target since it was looked up.
			if err := linkFile(leftover.Path, leftover.Target); err != nil {
				retain("could not be put back (%v); rename it to %s by hand", reason(err), target)
				break
			}
			restored := RestoredBackup{Target: leftover.Target, Backup: leftover.Path}
			if err := removeFile(leftover.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
				restored.BackupRemains = true
			}
			report.Restored = append(report.Restored, restored)
		}
	}

	return report, errors.Join(errs...)
}

// foldCase returns a string that is the same for every name that differs
// from name only in case, as strings.EqualFold tells: each letter is replaced
// by the first of those it is folded with.
func foldCase(name string) string {
	return strings.Map(func(r rune) rune {
		first := r
		for folded := unicode.SimpleFold(r); folded != r; folded = unicode.SimpleFold(folded) {
			first = min(first, folded)
		}
		return first
	}, name)
}

// movedOriginal reports whether the staged file leftover was to take the
// place of an input that was moved aside for it and is still there.
func movedOriginal(leftover Leftover) (MovedOriginal, bool) {
	if leftover.TargetState != TargetMissing || !SupportedExtension(leftover.Target) {
		return MovedOriginal{}, false
	}

	ext := filepath.Ext(leftover.Target)
	original := strings.TrimSuffix(leftover.Target, ext) + originalSuffix + ext
	if !exists(original) {
		return MovedOriginal{}, false
	}
	return MovedOriginal{Original: original, Input: leftover.Target}, true
}

// reason is the message of err without the paths of the files involved,
// which are reported next to it anyway.
func reason(err error) error {
	var (
		pathErr *fs.PathError
		linkErr *os.LinkError
	)
	switch {
	case errors.As(err, &pathErr):
		return pathErr.Err
	case errors.As(err, &linkErr):
		return linkErr.Err
	default:
		return err
	}
}
