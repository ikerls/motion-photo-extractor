package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// killedBeforePhoto leaves dir as a run with --rename-orig --delete-orig
// does that is killed once the input, IMG.jpg, is out of the photo's way:
// nothing but working files.
func killedBeforePhoto(t *testing.T, dir string) {
	t.Helper()
	files := map[string][]byte{
		"IMG.jpg.11111111.bak":  buildMotionPhotoFixture(),
		"IMG.jpg.22222222.part": []byte("photo"),
		"IMG.mp4.33333333.part": []byte("video"),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func TestPrettyPointsOutLeftoversAndChangesNothing(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	killedBeforePhoto(t, out)
	writeMotionPhotoFixture(t, filepath.Join(dir, "a.jpg"))

	status, got := runPretty(t, dir, "a.jpg", "--output", "out")
	want := strings.Join([]string{
		"! out  3 leftovers of an interrupted run, clean up with --recover",
		"    out/IMG.jpg.11111111.bak",
		"    out/IMG.jpg.22222222.part",
		"    out/IMG.mp4.33333333.part",
	}, "\n")
	if status != 0 || !strings.HasPrefix(got, want) {
		t.Fatalf("status = %d, output:\n%s\nwant it to start with:\n%s", status, got, want)
	}
	assertDirEntries(t, out, "IMG.jpg.11111111.bak", "IMG.jpg.22222222.part", "IMG.mp4.33333333.part", "a_photo.jpg", "a_video.mp4")
}

func TestPrettyRecoverReportsWhatItDid(t *testing.T) {
	dir := t.TempDir()
	killedBeforePhoto(t, dir)
	for name, content := range map[string]string{
		"KEPT.jpg":                   "photo",
		"KEPT.jpg.44444444.bak":      "original",
		"MOVED_original.jpg":         "original",
		"MOVED.jpg.55555555.part":    "photo",
		"motion-photo.66666666.part": "staged",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// No input is needed to clean up the output directory.
	status, got := runPretty(t, dir, "--recover")
	want := []string{
		"✓ IMG.jpg.22222222.part  removed",
		"✓ IMG.mp4.33333333.part  removed",
		"✓ MOVED.jpg.55555555.part  removed",
		"✓ IMG.jpg  restored from IMG.jpg.11111111.bak",
		"! KEPT.jpg.44444444.bak  kept: KEPT.jpg exists; compare the two, then delete this file or rename it by hand",
		"! motion-photo.66666666.part  kept: its name does not tell which output it belongs to; look at it and delete it by hand",
		"! MOVED_original.jpg  moved aside by an interrupted run; rename it to MOVED.jpg to extract it",
	}
	if status != 0 || got != strings.Join(want, "\n")+"\n" {
		t.Fatalf("status = %d, output:\n%s\nwant:\n%s", status, got, strings.Join(want, "\n"))
	}
	assertDirEntries(t, dir, "IMG.jpg", "KEPT.jpg", "KEPT.jpg.44444444.bak", "MOVED_original.jpg", "motion-photo.66666666.part")
	assertFileContent(t, filepath.Join(dir, "IMG.jpg"), buildMotionPhotoFixture())

	status, got = runPretty(t, t.TempDir(), "--recover")
	if want := "– .  no leftovers of an interrupted run\n"; status != 0 || got != want {
		t.Fatalf("with nothing to recover: status = %d, output = %q, want %q", status, got, want)
	}
}

// A run killed once the input was out of the photo's way leaves a directory
// with nothing in it to extract. With --recover the input is put back first,
// and is then found like any other.
func TestRecoverThenExtractsADirectoryThatHeldOnlyLeftovers(t *testing.T) {
	tests := []struct {
		name string
		args func(dir string) []string
	}{
		{name: "directory", args: func(dir string) []string { return []string{dir, "--output", dir} }},
		{name: "glob", args: func(dir string) []string { return []string{filepath.Join(dir, "*.jpg"), "--output", dir} }},
		{name: "directory next to the inputs", args: func(dir string) []string { return []string{filepath.Dir(dir), "--output", ""} }},
		{name: "glob next to the inputs", args: func(dir string) []string { return []string{filepath.Join(dir, "*.jpg"), "--output", ""} }},
		{name: "file next to the inputs", args: func(dir string) []string { return []string{filepath.Join(dir, "IMG.jpg"), "--output", ""} }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "photos")
			if err := os.Mkdir(dir, 0755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			killedBeforePhoto(t, dir)

			args := append(tc.args(dir), "--rename-orig", "--delete-orig", "--recover")
			if status, got := runPretty(t, filepath.Dir(dir), args...); status != 0 {
				t.Fatalf("status = %d, output:\n%s", status, got)
			}
			assertDirEntries(t, dir, "IMG.jpg", "IMG.mp4")
			if got, want := fileSize(filepath.Join(dir, "IMG.mp4")), int64(24); got != want {
				t.Fatalf("video size = %d, want %d", got, want)
			}
		})
	}
}

// An output directory that is yet to be created has nothing to recover, and
// is not created for the looking.
func TestRecoverSkipsAnOutputDirectoryThatDoesNotExist(t *testing.T) {
	dir := t.TempDir()
	writeMotionPhotoFixture(t, filepath.Join(dir, "a.jpg"))

	for _, args := range [][]string{{"a.jpg", "--output", "out"}, {"a.jpg", "--output", "recovered", "--recover"}} {
		if status, got := runPretty(t, dir, args...); status != 0 || strings.Contains(got, "leftover") {
			t.Fatalf("Run(%q): status = %d, output:\n%s", args, status, got)
		}
	}
	assertFileExists(t, filepath.Join(dir, "out", "a_video.mp4"))
	assertFileExists(t, filepath.Join(dir, "recovered", "a_video.mp4"))
}

// Recovery that fails stops the run before anything is extracted.
func TestRecoverFailureStopsTheRun(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory that cannot be read")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeMotionPhotoFixture(t, filepath.Join(dir, "a.jpg"))
	// Written to, but not listed.
	if err := os.Chmod(out, 0333); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(out, 0755) })

	status, got := runPretty(t, dir, "a.jpg", "--output", "out", "--recover")
	if status != 1 || !strings.HasPrefix(got, "✗ recover out: ") {
		t.Fatalf("status = %d, output:\n%s", status, got)
	}
	os.Chmod(out, 0755)
	assertDirEntries(t, out)

	// Without --recover the directory is not looked at, and written to.
	os.Chmod(out, 0333)
	if status, got := runPretty(t, dir, "a.jpg", "--output", "out"); status != 0 {
		t.Fatalf("without --recover: status = %d, output:\n%s", status, got)
	}
}

func TestRunRequiresAnInputUnlessRecoveringAnOutputDirectory(t *testing.T) {
	for _, args := range [][]string{nil, {"--recover", "--output", ""}} {
		if status, got := runPretty(t, t.TempDir(), args...); status != 1 || !strings.HasPrefix(got, "✗ no input specified") {
			t.Fatalf("Run(%q): status = %d, output:\n%s", args, status, got)
		}
	}
}

func TestOutputDirs(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, dir := range []string{"photos/2023", "photos/2024/trip", "other", "out"} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join("other", "a.jpg"), []byte("x"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	linked := os.Symlink("photos", "album") == nil

	tests := []struct {
		name   string
		output string
		inputs []string
		want   []string
	}{
		{name: "output directory", output: "out", inputs: []string{"photos"}, want: []string{"out"}},
		{name: "output directory that does not exist", output: "missing", inputs: []string{"photos"}},
		{name: "directory and all below it", inputs: []string{"photos"}, want: []string{"photos", "photos/2023", "photos/2024", "photos/2024/trip"}},
		{name: "file, there or not", inputs: []string{"other/a.jpg", "photos/2023/gone.jpg"}, want: []string{"other", "photos/2023"}},
		{name: "file in a directory that does not exist", inputs: []string{"missing/a.jpg"}},
		{name: "glob of files", inputs: []string{"photos/2023/*.jpg"}, want: []string{"photos/2023"}},
		{name: "glob of directories", inputs: []string{"photos/20*/*.jpg"}, want: []string{"photos/2023", "photos/2024"}},
		{name: "regex", inputs: []string{`/IMG_\d+\.jpg/`}, want: []string{"."}},
		{name: "same directory twice", inputs: []string{"other/a.jpg", "other", "./other/b.jpg"}, want: []string{"other"}},
	}
	if linked {
		tests = append(tests, struct {
			name   string
			output string
			inputs []string
			want   []string
		}{name: "same directory through a link", inputs: []string{"photos/2023/a.jpg", "album/2023/b.jpg"}, want: []string{"photos/2023"}})
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := outputDirs(tc.output, tc.inputs)
			if err != nil {
				t.Fatalf("outputDirs() error = %v", err)
			}
			for i := range got {
				got[i] = filepath.ToSlash(got[i])
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("outputDirs(%q, %q) = %q, want %q", tc.output, tc.inputs, got, tc.want)
			}
		})
	}
}

// A directory is the one its files are written to: the path as cleaned,
// wherever a link in it followed by .. would lead.
func TestRecoverStaysInTheDirectoryThatIsWrittenTo(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"work", "actual/child"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "actual", "child"), filepath.Join(root, "work", "alias")); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}
	// Where the link followed by .. leads, and what must not be touched.
	elsewhere := filepath.Join(root, "actual", "precious.mp4.11111111.part")
	if err := os.WriteFile(elsewhere, []byte("staged"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	unrelated := filepath.Join(root, "work", "precious.mp4.11111111.part")
	if err := os.Mkdir(unrelated, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	status, got := runPretty(t, root, "--recover", "--output", "work/alias/..")
	if want := "! work/precious.mp4.11111111.part  kept: not a regular file; look at it and delete it by hand\n"; status != 0 || got != want {
		t.Fatalf("status = %d, output = %q, want %q", status, got, want)
	}
	assertFileExists(t, elsewhere)
	assertFileExists(t, unrelated)
}

// What is there in place of the output directory is not a directory with
// nothing to recover.
func TestRecoverRejectsAnOutputPathThatIsAFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "out"), []byte("file"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	status, got := runPretty(t, dir, "--recover", "--output", "out")
	if want := "✗ recover: out is not a directory\n"; status != 1 || got != want {
		t.Fatalf("status = %d, output = %q, want %q", status, got, want)
	}

	// The path is the one files are written to: here the file again, not
	// the directory that the link followed by .. leads to.
	if err := os.MkdirAll(filepath.Join(dir, "actual", "child"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "actual", "out"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(filepath.Join("actual", "child"), filepath.Join(dir, "alias")); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}
	status, got = runPretty(t, dir, "--recover", "--output", "alias/../out")
	if want := "✗ recover: out is not a directory\n"; status != 1 || got != want {
		t.Fatalf("through a link: status = %d, output = %q, want %q", status, got, want)
	}
}

// One directory that cannot be recovered does not keep the others from
// being, but does keep the run from extracting anything.
func TestRecoverGoesOnAfterADirectoryFails(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory that cannot be read")
	}
	dir := t.TempDir()
	for _, sub := range []string{"locked", "open"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	writeMotionPhotoFixture(t, filepath.Join(dir, "open", "a.jpg"))
	staged := filepath.Join(dir, "open", "a_video.mp4.11111111.part")
	if err := os.WriteFile(staged, []byte("staged"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Chmod(filepath.Join(dir, "locked"), 0333); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "locked"), 0755) })

	status, got := runPretty(t, dir, "locked/gone.jpg", "open/a.jpg", "--output", "", "--recover")
	if status != 1 || !strings.Contains(got, "✓ open/a_video.mp4.11111111.part  removed") || !strings.Contains(got, "✗ recover locked: ") {
		t.Fatalf("status = %d, output:\n%s", status, got)
	}
	assertFileDoesNotExist(t, staged)
	assertFileDoesNotExist(t, filepath.Join(dir, "open", "a_video.mp4"))
}

// A pattern may designate directories that cannot be found, the one above
// them not being readable. Recovery then cannot tell that there is nothing
// to recover.
func TestRecoverFailsWhenTheDirectoriesOfAPatternCannotBeFound(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory that cannot be read")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "locked", "album"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeMotionPhotoFixture(t, filepath.Join(dir, "good.jpg"))
	if err := os.Chmod(filepath.Join(dir, "locked"), 0333); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "locked"), 0755) })

	status, got := runPretty(t, dir, "locked/*/*.jpg", "good.jpg", "--output", "", "--recover")
	if status != 1 || !strings.Contains(got, "✗ recover: ") || !strings.Contains(got, "locked") {
		t.Fatalf("status = %d, output:\n%s", status, got)
	}
	assertFileDoesNotExist(t, filepath.Join(dir, "good_video.mp4"))

	// Without --recover the pattern matches nothing, as before.
	if status, got := runPretty(t, dir, "locked/*/*.jpg", "good.jpg", "--output", ""); status != 0 {
		t.Fatalf("without --recover: status = %d, output:\n%s", status, got)
	}
}

// The directories of a pattern are the ones filepath.Glob finds for the part
// of it that names them, whatever that part looks like.
func TestGlobDirsAreThoseOfGlob(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	dirs := []string{"photos/2023/trip", "photos/2024/trip", "photos/misc", "photos/.hidden/trip", "a/real/album", "star*/album"}
	if runtime.GOOS != "windows" {
		dirs = append(dirs, "literal/album", `lit\eral/album`, `lit\\eral/album`)
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join("photos", "2025"), []byte("a file"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Symlink("photos", "album"); err != nil {
		t.Logf("without a link among the directories: %v", err)
	}

	patterns := []string{
		"photos/20*", "photos/20*/trip", "photos/*/t*", "photos/[0-9]*", "*/misc", "*", "*/*", "al*/2023",
		"missing/*", "photos/*/missing", "./photos/*", "../*", root + "/photos/*",
		// What a path is cleaned of is not nothing to a pattern.
		"a/*/./album", "a/*//album", "a/*/../*/album", "photos/./20*", "photos//20*", "photos/20*/",
	}
	if runtime.GOOS != "windows" {
		patterns = append(patterns,
			`lit\\eral`, `lit\\eral/*`, `*/alb\\um`, `star\\*/album`, `star\\*/*`, `lit\\\\eral/*`, `a/*/./alb\\um`, `a/*//alb\\um`,
			`a/*/../*/alb\\um`, `photos/[\\0-9]*`, `photos/20*\\`,
			// A single backslash stands for the character after it.
			`lit\eral`, `lit\eral/*`, `*/alb\um`, `star\*/album`, `star\*/*`, `a/*/./alb\um`, `a/*//alb\um`, `a/*/../*/alb\um`, `photos/[\0-9]*`,
		)
	}

	for _, dirPattern := range patterns {
		got, err := globDirs(dirPattern + string(filepath.Separator) + "*.jpg")
		if err != nil {
			t.Fatalf("globDirs(%q) error = %v", dirPattern, err)
		}
		// A part without anything to match is a path, taken as it is.
		want := []string{dirPattern}
		if hasGlobMeta(dirPattern) {
			if want, err = filepath.Glob(dirPattern); err != nil {
				t.Fatalf("Glob(%q) error = %v", dirPattern, err)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("globDirs(%q) = %q, want %q as Glob gives", dirPattern, got, want)
		}
	}

	if got, err := globDirs("*.jpg"); err != nil || !slices.Equal(got, []string{"."}) {
		t.Fatalf(`globDirs("*.jpg") = %q, %v; want the current directory`, got, err)
	}
	if got, err := globDirs(string(filepath.Separator) + "*.jpg"); err != nil || !slices.Equal(got, []string{string(filepath.Separator)}) {
		t.Fatalf(`globDirs("/*.jpg") = %q, %v; want the root`, got, err)
	}
}

// A pattern is malformed whatever there is for it to match, and wherever in
// it the mistake is: also in a part that cleaning the path would drop.
func TestGlobDirsRejectsMalformedPatterns(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join("photos", "misc"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	for _, pattern := range []string{"photos/[/x/*.jpg", "photos/misc/[", "missing/[/*.jpg", "[/photos/*.jpg", "photos/[/../misc/*.jpg", "[/../photos/*.jpg"} {
		if dirs, err := globDirs(pattern); err == nil || dirs != nil {
			t.Fatalf("globDirs(%q) = %q, %v; want an error and no directories", pattern, dirs, err)
		}
	}
}

// The directory that is recovered is the one the files of a pattern are
// extracted in, not one whose name is the pattern taken literally.
func TestRecoverLooksWhereAnEscapedPatternLeads(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a backslash separates directories")
	}
	dir := t.TempDir()
	for _, sub := range []string{"literal/album", `lit\eral/album`} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	writeMotionPhotoFixture(t, filepath.Join(dir, "literal", "album", "good.jpg"))
	staged := filepath.Join(dir, "literal", "album", "real.mp4.11111111.part")
	unrelated := filepath.Join(dir, `lit\eral`, "album", "precious.mp4.22222222.part")
	for _, path := range []string{staged, unrelated} {
		if err := os.WriteFile(path, []byte("staged"), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	if status, got := runPretty(t, dir, `lit\eral/*/*.jpg`, "--output", "", "--recover"); status != 0 {
		t.Fatalf("status = %d, output:\n%s", status, got)
	}
	assertFileDoesNotExist(t, staged)
	assertFileExists(t, unrelated)
	assertFileExists(t, filepath.Join(dir, "literal", "album", "good_video.mp4"))
}

// What a path is cleaned of is not nothing to a pattern: a/*/./album matches
// no directory, and none is recovered for it.
func TestRecoverLeavesAloneWhatAPatternDoesNotMatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a backslash separates directories")
	}
	for _, pattern := range []string{`a/*/./alb\um/*.jpg`, `a/*//alb\um/*.jpg`, `a/*/../*/alb\um/*.jpg`} {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "a", "real", "album"), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		staged := filepath.Join(dir, "a", "real", "album", "precious.mp4.11111111.part")
		if err := os.WriteFile(staged, []byte("staged"), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		// The part that names the directories, exactly as it is written.
		matches, err := filepath.Glob(dir + "/" + strings.TrimSuffix(pattern, "/*.jpg"))
		if err != nil {
			t.Fatalf("Glob() error = %v", err)
		}
		status, got := runPretty(t, dir, pattern, "--output", "", "--recover")
		if status != 0 {
			t.Fatalf("Run(%q): status = %d, output:\n%s", pattern, status, got)
		}
		// Recovered only if the pattern leads there, as Glob tells.
		if _, err := os.Stat(staged); (err != nil) != (len(matches) > 0) {
			t.Fatalf("Run(%q): staged file gone = %t, Glob matches = %q\n%s", pattern, err != nil, matches, got)
		}
	}
}

// A path that leads nowhere as it is written designates no directory,
// whatever cleaning it makes of it: missing/.. is not the directory above.
func TestRecoverLeavesAloneWhatAPathDoesNotLeadTo(t *testing.T) {
	for _, input := range []string{"a/missing/../*.jpg", "a/missing/../IMG.jpg", "a/file/../*.jpg"} {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "a"), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		staged := filepath.Join(dir, "a", "precious.mp4.11111111.part")
		for _, path := range []string{staged, filepath.Join(dir, "a", "file")} {
			if err := os.WriteFile(path, []byte("staged"), 0644); err != nil {
				t.Fatalf("write file: %v", err)
			}
		}

		_, got := runPretty(t, dir, input, "--output", "", "--recover")
		if _, err := os.Stat(staged); err != nil {
			t.Fatalf("Run(%q) removed a file where the input does not lead:\n%s", input, got)
		}
	}
}

// A path that cannot be followed as it is written designates no directory
// either, and is an error: what cleaning makes of it is not looked at.
func TestRecoverFailsOnAPathThatCannotBeFollowed(t *testing.T) {
	for _, input := range []string{"a/loop/../*.jpg", "a/loop/../IMG.jpg"} {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "a"), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.Symlink("loop", filepath.Join(dir, "a", "loop")); err != nil {
			t.Skipf("cannot create symlinks: %v", err)
		}
		staged := filepath.Join(dir, "a", "precious.mp4.11111111.part")
		if err := os.WriteFile(staged, []byte("staged"), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		status, got := runPretty(t, dir, input, "--output", "", "--recover")
		if status != 1 || !strings.HasPrefix(got, "✗ recover: ") {
			t.Fatalf("Run(%q): status = %d, output:\n%s", input, status, got)
		}
		assertFileExists(t, staged)
	}
}

// The directories below one that is given are those of where its path
// leads, as for the files that are looked for in them.
func TestRecoverWalksADirectoryAsItIsWritten(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"a/child", "a/unrelated", "outside/child"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.Symlink(filepath.Join("..", "outside", "child"), filepath.Join(dir, "a", "link")); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}
	staged := filepath.Join(dir, "a", "unrelated", "precious.mp4.11111111.part")
	if err := os.WriteFile(staged, []byte("staged"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// The path leads to outside, whose only directory is child.
	if status, got := runPretty(t, dir, "a/link/..", "--output", "", "--recover"); status != 0 {
		t.Fatalf("status = %d, output:\n%s", status, got)
	}
	assertFileExists(t, staged)

	t.Chdir(dir)
	got, err := outputDirs("", []string{"a/link/.."})
	if want := []string{"a", "a/child"}; err != nil || !slices.Equal(got, want) {
		t.Fatalf("outputDirs() = %q, %v; want %q", got, err, want)
	}
}

// Some mistakes in a pattern only show when a name is matched against it.
// They are found before anything is recovered all the same.
func TestRecoverRejectsAPatternThatIsMalformedInItsLastPart(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "real", "album"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	staged := filepath.Join(dir, "a", "real", "album", "precious.mp4.11111111.part")
	if err := os.WriteFile(staged, []byte("staged"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	for _, pattern := range []string{"a/*/album/[", "a/*/album/p*[", "a/*/alb*[/*.jpg"} {
		status, got := runPretty(t, dir, pattern, "--output", "", "--recover")
		if status != 1 || !strings.HasPrefix(got, "✗ recover: invalid glob pattern") {
			t.Fatalf("Run(%q): status = %d, output:\n%s", pattern, status, got)
		}
		assertFileExists(t, staged)
	}
}

// A file where a pattern expects a directory, at any depth, is nothing that
// keeps recovery from going on.
func TestRecoverIsNotStoppedByAFileAboveAPattern(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "a"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "file"), []byte("file"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	writeMotionPhotoFixture(t, filepath.Join(dir, "good.jpg"))

	for _, pattern := range []string{"a/file/x/*/*.jpg", "a/file/*/*.jpg", "a/file/x/*.jpg", "a/*/x/y/*/*.jpg"} {
		if status, got := runPretty(t, dir, pattern, "good.jpg", "--output", "", "--recover", "--force"); status != 0 {
			t.Fatalf("Run(%q): status = %d, output:\n%s", pattern, status, got)
		}
		assertFileExists(t, filepath.Join(dir, "good_video.mp4"))
	}
}

func TestCleanGlobDir(t *testing.T) {
	sep := string(filepath.Separator)
	tests := []struct {
		dir     string
		prefix  int
		cleaned string
	}{
		{dir: "", cleaned: "."},
		{dir: sep, prefix: 1, cleaned: sep},
		{dir: "photos" + sep, cleaned: "photos"},
		{dir: "photos" + sep + "*" + sep, cleaned: "photos" + sep + "*"},
		{dir: "." + sep, cleaned: "."},
		{dir: "photos" + sep + sep, cleaned: "photos" + sep},
	}
	if runtime.GOOS == "windows" {
		// As cleanGlobPathWindows of path/filepath has them.
		tests = append(tests, []struct {
			dir     string
			prefix  int
			cleaned string
		}{
			{dir: `C:\`, prefix: 3, cleaned: `C:\`},
			{dir: `C:`, prefix: 2, cleaned: `C:.`},
			{dir: `C:photos\`, prefix: 2, cleaned: `C:photos`},
			{dir: `\\host\share\`, prefix: 13, cleaned: `\\host\share\`},
			{dir: `\\host\`, prefix: 6, cleaned: `\\host`},
			{dir: `\\?\C:\`, prefix: 7, cleaned: `\\?\C:\`},
			{dir: `\\?\C:\photos\`, prefix: 6, cleaned: `\\?\C:\photos`},
		}...)
	}

	for _, tc := range tests {
		prefix, cleaned := cleanGlobDir(tc.dir)
		if prefix != tc.prefix || cleaned != tc.cleaned {
			t.Errorf("cleanGlobDir(%q) = %d, %q; want %d, %q", tc.dir, prefix, cleaned, tc.prefix, tc.cleaned)
		}
	}
}

// A file that a pattern matches where it designates directories holds none,
// and is no reason for recovery to fail.
func TestRecoverSkipsFilesAmongTheDirectoriesOfAPattern(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "real", "b"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "file"), []byte("file"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	staged := filepath.Join(dir, "a", "real", "b", "old.mp4.11111111.part")
	if err := os.WriteFile(staged, []byte("staged"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	writeMotionPhotoFixture(t, filepath.Join(dir, "good.jpg"))

	if status, got := runPretty(t, dir, "a/*/b/*.jpg", "good.jpg", "--output", "", "--recover"); status != 0 {
		t.Fatalf("status = %d, output:\n%s", status, got)
	}
	assertFileDoesNotExist(t, staged)
	assertFileExists(t, filepath.Join(dir, "good_video.mp4"))
}

// A malformed pattern is turned down before anything is recovered.
func TestRecoverRejectsAMalformedPattern(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "empty"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeMotionPhotoFixture(t, filepath.Join(dir, "good.jpg"))

	status, got := runPretty(t, dir, "empty/[/*.jpg", "good.jpg", "--output", "", "--recover")
	if status != 1 || !strings.HasPrefix(got, "✗ recover: invalid glob pattern") {
		t.Fatalf("status = %d, output:\n%s", status, got)
	}
	assertFileDoesNotExist(t, filepath.Join(dir, "good_video.mp4"))

	// Also where cleaning the path would drop the mistake, and before the
	// directory that is left of it is touched.
	if err := os.Mkdir(filepath.Join(dir, "album"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	staged := filepath.Join(dir, "album", "precious.mp4.11111111.part")
	if err := os.WriteFile(staged, []byte("staged"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	status, got = runPretty(t, dir, "empty/[/../../album/*.jpg", "--output", "", "--recover")
	if status != 1 || !strings.HasPrefix(got, "✗ recover: invalid glob pattern") {
		t.Fatalf("status = %d, output:\n%s", status, got)
	}
	assertFileExists(t, staged)
}

// Structured logs name the leftovers of a directory in one line too.
func TestRunLogsLeftoversOncePerDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	killedBeforePhoto(t, dir)
	writeMotionPhotoFixture(t, filepath.Join(dir, "a.jpg"))

	var stdout, stderr bytes.Buffer
	if status := Run([]string{"a.jpg", "--log-format", "text"}, &stdout, &stderr, "test"); status != 0 {
		t.Fatalf("status = %d, stderr: %s", status, &stderr)
	}
	got := stderr.String()
	if strings.Count(got, "Leftovers of an interrupted run") != 1 ||
		!strings.Contains(got, `files="IMG.jpg.11111111.bak IMG.jpg.22222222.part IMG.mp4.33333333.part"`) {
		t.Fatalf("stderr = %s, want one line naming the three leftovers", got)
	}
}

func TestInterruptSignalsIncludeTermination(t *testing.T) {
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP} {
		if !slices.Contains(interruptSignals, sig) {
			t.Fatalf("interruptSignals = %v, want %v among them", interruptSignals, sig)
		}
	}
}
