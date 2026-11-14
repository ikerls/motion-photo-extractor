//go:build unix

package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A plain kill and a terminal that goes away end a run the way Ctrl+C does:
// the file being processed is finished, and the status is 130.
func TestInterruptContextIsCanceledByTermination(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT} {
		ctx, stop := interruptContext()
		if err := syscall.Kill(os.Getpid(), sig); err != nil {
			stop()
			t.Fatalf("kill: %v", err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			stop()
			t.Fatalf("context not canceled by %v", sig)
		}
		stop()
	}
}

func TestRunStopsWithStatus130WhenTerminated(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	writeMotionPhotoFixture(t, "a.jpg")
	writeMotionPhotoFixture(t, "b.jpg")

	// Terminated as soon as it listens, the run begins no file.
	listening = func(ctx context.Context) {
		syscall.Kill(os.Getpid(), syscall.SIGTERM)
		<-ctx.Done()
	}
	t.Cleanup(func() { listening = func(context.Context) {} })

	var stdout, stderr bytes.Buffer
	status := Run([]string{"a.jpg", "b.jpg", "--output", "out", "--log-format", "text"}, &stdout, &stderr, "test")
	if status != exitInterrupted {
		t.Fatalf("status = %d, want %d\nstderr: %s", status, exitInterrupted, &stderr)
	}
	assertFileDoesNotExist(t, filepath.Join(dir, "out", "a_video.mp4"))
}
