package extractor

import (
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

func TestParseLeftoverName(t *testing.T) {
	tests := []struct {
		name   string
		target string
		kind   LeftoverKind
		ok     bool
	}{
		{name: "IMG_video.mp4.1a2b3c4d.part", target: "IMG_video.mp4", kind: LeftoverStaged, ok: true},
		{name: "IMG.jpg.00000000.bak", target: "IMG.jpg", kind: LeftoverBackup, ok: true},
		{name: "IMG.JPEG.deadbeef.bak", target: "IMG.JPEG", kind: LeftoverBackup, ok: true},
		{name: "IMG.HEIC.deadbeef.part", target: "IMG.HEIC", kind: LeftoverStaged, ok: true},
		{name: "IMG_original.jpg.deadbeef.part", target: "IMG_original.jpg", kind: LeftoverStaged, ok: true},
		{name: "motion-photo.deadbeef.part", kind: LeftoverStaged, ok: true},
		{name: "motion-photo.deadbeef.bak", kind: LeftoverBackup, ok: true},

		// Other files, however much they look like one.
		{name: "notes.txt.deadbeef.part"},
		{name: "IMG.jpg.DEADBEEF.part"},
		{name: "IMG.jpg.deadbee.part"},
		{name: "IMG.jpg.deadbeef0.part"},
		{name: "IMG.jpg.deadbeeg.part"},
		{name: "IMG.jpg.part"},
		{name: "IMG.jpg.deadbeef.tmp"},
		{name: "IMG.jpg.deadbeef"},
		{name: ".jpg.deadbeef.part"},
		{name: "deadbeef.part"},
		{name: "IMG.jpg"},
	}

	for _, tc := range tests {
		target, kind, ok := parseLeftoverName(tc.name)
		if ok != tc.ok || target != tc.target || kind != tc.kind {
			t.Errorf("parseLeftoverName(%q) = %q, %q, %t; want %q, %q, %t", tc.name, target, kind, ok, tc.target, tc.kind, tc.ok)
		}
	}
}

// The names that ExtractFile gives its working files are the ones that
// FindLeftovers recognizes.
func TestCreateSiblingNamesAreLeftoverNames(t *testing.T) {
	dir := t.TempDir()
	names := map[string]string{
		filepath.Join(dir, "IMG_video.mp4"):                 "IMG_video.mp4",
		filepath.Join(dir, strings.Repeat("a", 250)+".jpg"): "",
	}

	for path, wantTarget := range names {
		for suffix, wantKind := range map[string]LeftoverKind{stagedSuffix: LeftoverStaged, backupSuffix: LeftoverBackup} {
			file, err := createSibling(path, suffix, 0o644)
			if err != nil {
				t.Fatalf("createSibling() error = %v", err)
			}
			file.Close()

			target, kind, ok := parseLeftoverName(filepath.Base(file.Name()))
			if !ok || target != wantTarget || kind != wantKind {
				t.Fatalf("parseLeftoverName(%q) = %q, %q, %t; want %q, %q, true", filepath.Base(file.Name()), target, kind, ok, wantTarget, wantKind)
			}
		}
	}
}

func TestFindLeftoversChangesNothing(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"IMG.jpg":                     "photo",
		"IMG.jpg.11111111.bak":        "original",
		"IMG_video.mp4.22222222.part": "half a video",
		"motion-photo.33333333.part":  "staged",
		"notes.txt.44444444.part":     "someone's file",
		"IMG.jpg.DEADBEEF.bak":        "someone's file",
	})
	if err := os.Symlink("nowhere", filepath.Join(dir, "LINK.jpg")); err == nil {
		writeFiles(t, dir, map[string]string{"LINK.jpg.55555555.bak": "backup"})
	}

	found, err := FindLeftovers(dir)
	if err != nil {
		t.Fatalf("FindLeftovers() error = %v", err)
	}

	type summary struct {
		name, target string
		kind         LeftoverKind
		state        TargetState
	}
	want := []summary{
		{"IMG.jpg.11111111.bak", "IMG.jpg", LeftoverBackup, TargetExists},
		{"IMG_video.mp4.22222222.part", "IMG_video.mp4", LeftoverStaged, TargetMissing},
		// A link that leads nowhere is something at the target all the same.
		{"LINK.jpg.55555555.bak", "LINK.jpg", LeftoverBackup, TargetExists},
		{"motion-photo.33333333.part", "", LeftoverStaged, TargetUnknown},
	}
	var got []summary
	for _, leftover := range found {
		target := leftover.Target
		if target != "" {
			target = filepath.Base(target)
		}
		got = append(got, summary{filepath.Base(leftover.Path), target, leftover.Kind, leftover.TargetState})
	}
	if _, err := os.Lstat(filepath.Join(dir, "LINK.jpg")); err != nil {
		want = slices.Delete(want, 2, 3)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("FindLeftovers() = %v, want %v", got, want)
	}

	if entries, err := os.ReadDir(dir); err != nil || len(entries) < 6 {
		t.Fatalf("entries = %v, err = %v, want all of them still there", entries, err)
	}

	if _, err := FindLeftovers(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("FindLeftovers() of a missing directory error = %v, want os.ErrNotExist", err)
	}
}

func TestRecoverRemovesStagedFilesAndRestoresBackups(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		// A run that was killed once the input was out of the photo's way.
		"IMG.jpg.11111111.bak":  "original",
		"IMG.jpg.22222222.part": "photo",
		"IMG.mp4.33333333.part": "video",
		// Files of someone else.
		"notes.txt.44444444.part": "notes",
		"IMG.jpg.DEADBEEF.bak":    "upper case",
		"keep.jpg":                "keep",
	})

	report, err := Recover(dir)
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}

	assertDirEntries(t, dir, "IMG.jpg", "IMG.jpg.DEADBEEF.bak", "keep.jpg", "notes.txt.44444444.part")
	assertFileContent(t, filepath.Join(dir, "IMG.jpg"), []byte("original"))

	if got, want := baseNames(report.Removed), []string{"IMG.jpg.22222222.part", "IMG.mp4.33333333.part"}; !slices.Equal(got, want) {
		t.Fatalf("Removed = %q, want %q", got, want)
	}
	if len(report.Restored) != 1 || filepath.Base(report.Restored[0].Target) != "IMG.jpg" ||
		filepath.Base(report.Restored[0].Backup) != "IMG.jpg.11111111.bak" || report.Restored[0].BackupRemains {
		t.Fatalf("Restored = %+v", report.Restored)
	}
	if len(report.Retained) != 0 || len(report.MovedAside) != 0 || report.Empty() {
		t.Fatalf("report = %+v", report)
	}

	// There is nothing left to do a second time.
	if report, err = Recover(dir); err != nil || !report.Empty() {
		t.Fatalf("second Recover() = %+v, %v; want an empty report", report, err)
	}
}

func TestRecoverRetainsBackupsThatAreNotPlainlyTheOnlyCopy(t *testing.T) {
	tests := []struct {
		name   string
		files  map[string]string
		setup  func(t *testing.T, dir string)
		reason string
		kept   []string // backups that must still be there, untouched
	}{
		{
			name:   "target exists",
			files:  map[string]string{"IMG.jpg": "photo", "IMG.jpg.11111111.bak": "original"},
			reason: "IMG.jpg exists",
			kept:   []string{"IMG.jpg.11111111.bak"},
		},
		{
			name:  "target is a link that leads nowhere",
			files: map[string]string{"IMG.jpg.11111111.bak": "original"},
			setup: func(t *testing.T, dir string) {
				if err := os.Symlink("nowhere", filepath.Join(dir, "IMG.jpg")); err != nil {
					t.Skipf("cannot create symlinks: %v", err)
				}
			},
			reason: "IMG.jpg exists",
			kept:   []string{"IMG.jpg.11111111.bak"},
		},
		{
			name:   "several backups",
			files:  map[string]string{"IMG.jpg.11111111.bak": "one", "IMG.jpg.22222222.bak": "two"},
			reason: "one of several backups of IMG.jpg",
			kept:   []string{"IMG.jpg.11111111.bak", "IMG.jpg.22222222.bak"},
		},
		{
			name:  "backups of names that differ in case",
			files: map[string]string{"IMG.jpg.11111111.bak": "one", "img.jpg.22222222.bak": "two"},
			setup: func(t *testing.T, dir string) {
				if len(dirEntries(t, dir)) != 2 {
					t.Skip("needs a filesystem that tells names apart by case")
				}
			},
			reason: "one of several backups of",
			kept:   []string{"IMG.jpg.11111111.bak", "img.jpg.22222222.bak"},
		},
		{
			// What a run leaves that was killed between reserving the name
			// of a backup and moving the file to it, and what a later run
			// that moved the original aside must not find to restore.
			name:   "empty backup",
			files:  map[string]string{"IMG.jpg.11111111.bak": "", "IMG_original.jpg": "original"},
			reason: "empty, so it may be a name that was reserved and never used",
			kept:   []string{"IMG.jpg.11111111.bak"},
		},
		{
			name:  "backup that is a link",
			files: map[string]string{"elsewhere.jpg": "original"},
			setup: func(t *testing.T, dir string) {
				if err := os.Symlink("elsewhere.jpg", filepath.Join(dir, "IMG.jpg.11111111.bak")); err != nil {
					t.Skipf("cannot create symlinks: %v", err)
				}
			},
			reason: "not a regular file",
			kept:   []string{"IMG.jpg.11111111.bak"},
		},
		{
			name:   "name that stands in for a long one",
			files:  map[string]string{"motion-photo.11111111.bak": "original", "motion-photo.22222222.part": "staged"},
			reason: "its name does not tell which output it belongs to",
			kept:   []string{"motion-photo.11111111.bak", "motion-photo.22222222.part"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, tc.files)
			if tc.setup != nil {
				tc.setup(t, dir)
			}
			before := dirEntries(t, dir)

			report, err := Recover(dir)
			if err != nil {
				t.Fatalf("Recover() error = %v", err)
			}
			if len(report.Restored) != 0 || len(report.Removed) != 0 {
				t.Fatalf("report = %+v, want nothing restored or removed", report)
			}
			if got := baseNames(retainedPaths(report)); !slices.Equal(got, tc.kept) {
				t.Fatalf("Retained = %q, want %q", got, tc.kept)
			}
			for _, retained := range report.Retained {
				if !strings.Contains(retained.Reason, tc.reason) {
					t.Fatalf("reason = %q, want it to contain %q", retained.Reason, tc.reason)
				}
			}
			if after := dirEntries(t, dir); !slices.Equal(after, before) {
				t.Fatalf("entries = %q, want them unchanged: %q", after, before)
			}
			for name, content := range tc.files {
				assertFileContent(t, filepath.Join(dir, name), []byte(content))
			}
		})
	}
}

func TestRecoverRetainsStagedFilesThatAreNotRegular(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "IMG.mp4.11111111.part"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFiles(t, dir, map[string]string{"precious.mp4": "precious"})
	if err := os.Symlink("precious.mp4", filepath.Join(dir, "IMG.mp4.22222222.part")); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}

	report, err := Recover(dir)
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if got, want := baseNames(retainedPaths(report)), []string{"IMG.mp4.11111111.part", "IMG.mp4.22222222.part"}; !slices.Equal(got, want) || len(report.Removed) != 0 {
		t.Fatalf("Retained = %q, Removed = %q; want %q and none", got, report.Removed, want)
	}
	assertDirEntries(t, dir, "IMG.mp4.11111111.part", "IMG.mp4.22222222.part", "precious.mp4")
}

// A target that appears once it was found missing is not overwritten.
func TestRecoverDoesNotOverwriteATargetThatAppears(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"IMG.jpg.11111111.bak": "original"})
	target := filepath.Join(dir, "IMG.jpg")

	linkFile = func(oldname, newname string) error {
		if err := os.WriteFile(target, []byte("written meanwhile"), 0644); err != nil {
			t.Fatalf("write target: %v", err)
		}
		return os.Link(oldname, newname)
	}
	t.Cleanup(func() { linkFile = os.Link })

	report, err := Recover(dir)
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if len(report.Restored) != 0 || len(report.Retained) != 1 || !strings.Contains(report.Retained[0].Reason, "could not be put back") {
		t.Fatalf("report = %+v, want the backup retained", report)
	}
	assertFileContent(t, target, []byte("written meanwhile"))
	assertFileContent(t, filepath.Join(dir, "IMG.jpg.11111111.bak"), []byte("original"))
}

// A filesystem without links cannot restore without the risk of overwriting.
func TestRecoverRetainsABackupThatCannotBeLinked(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"IMG.jpg.11111111.bak": "original"})

	linkFile = func(oldname, newname string) error {
		return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: syscall.EPERM}
	}
	t.Cleanup(func() { linkFile = os.Link })

	report, err := Recover(dir)
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if len(report.Retained) != 1 || !strings.Contains(report.Retained[0].Reason, "rename it to IMG.jpg by hand") {
		t.Fatalf("report = %+v, want the backup retained with what to do", report)
	}
	if strings.Contains(report.Retained[0].Reason, dir) {
		t.Fatalf("reason = %q, want it without paths", report.Retained[0].Reason)
	}
	assertDirEntries(t, dir, "IMG.jpg.11111111.bak")
}

// Restored and removed are two steps. When the second fails, the first is
// reported along with the error.
func TestRecoverReportsWhatItDidAlongWithAnError(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"IMG.jpg.11111111.bak":  "original",
		"IMG.mp4.22222222.part": "video",
	})

	failure := errors.New("cannot remove")
	removeFile = func(name string) error { return failure }
	t.Cleanup(func() { removeFile = os.Remove })

	report, err := Recover(dir)
	if !errors.Is(err, failure) {
		t.Fatalf("Recover() error = %v, want the removal failure", err)
	}
	if len(report.Restored) != 1 || !report.Restored[0].BackupRemains {
		t.Fatalf("Restored = %+v, want the backup restored and still there", report.Restored)
	}
	if len(report.Removed) != 0 || len(report.Retained) != 1 || !strings.Contains(report.Retained[0].Reason, "could not be removed") {
		t.Fatalf("report = %+v, want the staged file retained", report)
	}
	assertFileContent(t, filepath.Join(dir, "IMG.jpg"), []byte("original"))
}

// With RenameOriginal the input is moved aside before the outputs are moved
// into place. A run killed in between leaves the original where it is: it is
// pointed out, not moved back.
func TestRecoverPointsOutAnOriginalThatWasMovedAside(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"IMG_original.jpg":      "original",
		"IMG.jpg.11111111.part": "photo",
		"IMG.mp4.22222222.part": "video",
		// A staged file whose original is not there says nothing.
		"OTHER.jpg.33333333.part": "photo",
	})

	report, err := Recover(dir)
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	want := []MovedOriginal{{Original: filepath.Join(dir, "IMG_original.jpg"), Input: filepath.Join(dir, "IMG.jpg")}}
	if !slices.Equal(report.MovedAside, want) {
		t.Fatalf("MovedAside = %+v, want %+v", report.MovedAside, want)
	}
	assertDirEntries(t, dir, "IMG_original.jpg")
}

// A target that cannot be looked up is not known to be missing.
func TestRecoverRetainsLeftoversItCannotExamine(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory whose files cannot be examined")
	}
	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFiles(t, dir, map[string]string{"IMG.jpg.11111111.bak": "original", "IMG.mp4.22222222.part": "video"})
	// Listed, but not looked into.
	if err := os.Chmod(dir, 0444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0755) })

	found, err := FindLeftovers(dir)
	if err != nil {
		t.Fatalf("FindLeftovers() error = %v", err)
	}
	for _, leftover := range found {
		if leftover.TargetState != TargetUnknown || leftover.TargetErr == nil {
			t.Fatalf("leftover = %+v, want its target state unknown", leftover)
		}
	}

	report, err := Recover(dir)
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if len(report.Retained) != 2 || len(report.Removed) != 0 || len(report.Restored) != 0 {
		t.Fatalf("report = %+v, want both leftovers retained", report)
	}
}

// What a killed ExtractFile leaves behind is what Recover deals with: here
// the input, which the photo was about to take the place of.
func TestRecoverAfterAnExtractionKilledBeforeThePhotoWasPublished(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "sample.jpg")
	writeMotionPhotoFixture(t, input)
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	// The state it is killed in: both components staged, the video
	// published, the input moved out of the photo's way.
	var done changes
	stagedVideo, err := done.stage(filepath.Join(dir, "sample.mp4"), minimalMP4Data, modTimeOf(t, input))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if _, err := done.stage(input, []byte("photo"), modTimeOf(t, input)); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if err := done.publish(stagedVideo, filepath.Join(dir, "sample.mp4")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := done.clear(input); err != nil {
		t.Fatalf("clear: %v", err)
	}

	report, err := Recover(dir)
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if len(report.Restored) != 1 || len(report.Removed) != 1 || len(report.Retained) != 0 {
		t.Fatalf("report = %+v, want the input restored and the staged photo removed", report)
	}
	assertDirEntries(t, dir, "sample.jpg", "sample.mp4")
	assertFileContent(t, input, original)

	// The extraction can then be done again.
	if _, err := ExtractFile(input, Options{RenameOriginal: true, DeleteOriginal: true, Overwrite: true}); err != nil {
		t.Fatalf("ExtractFile() after recovery error = %v", err)
	}
	assertDirEntries(t, dir, "sample.jpg", "sample.mp4")
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func baseNames(paths []string) []string {
	var names []string
	for _, path := range paths {
		names = append(names, filepath.Base(path))
	}
	return names
}

func retainedPaths(report RecoveryReport) []string {
	var paths []string
	for _, retained := range report.Retained {
		paths = append(paths, retained.Path)
	}
	return paths
}

func modTimeOf(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.ModTime()
}
