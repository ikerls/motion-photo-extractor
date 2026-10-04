package cli

import (
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
			got := outputDirs(tc.output, tc.inputs)
			for i := range got {
				got[i] = filepath.ToSlash(got[i])
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("outputDirs(%q, %q) = %q, want %q", tc.output, tc.inputs, got, tc.want)
			}
		})
	}
}

func TestInterruptSignalsIncludeTermination(t *testing.T) {
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP} {
		if !slices.Contains(interruptSignals, sig) {
			t.Fatalf("interruptSignals = %v, want %v among them", interruptSignals, sig)
		}
	}
}
