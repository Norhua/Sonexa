//go:build windows

package spotify

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Run on native Windows, not Wine or a cross-compiled binary on another OS.
// Handles held by this test are closed explicitly; the runner's handles must
// return to the warmed-up process baseline after both success and failure.
func TestWindowsRunnerTreeAndHandles(t *testing.T) {
	countHandles := func() uint32 {
		t.Helper()
		proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessHandleCount")
		var count uint32
		ok, _, err := proc.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&count)))
		if ok == 0 {
			t.Fatal(err)
		}
		return count
	}
	r := testRunner(t, "stderr")
	run := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_, err := r.Run(ctx, testPlaylist, nil)
		return err
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	baseline := countHandles()
	for _, mode := range []string{"tree", "tree-bad-frame", "tree-result"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("SONEXA_RUNNER_TEST_PROCESS", mode)
			pidFile := t.TempDir() + "/child.pid"
			t.Setenv("SONEXA_RUNNER_TEST_PID", pidFile)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := r.Run(ctx, testPlaylist, nil); done <- err }()
			var pid int
			deadline := time.Now().Add(5 * time.Second)
			for pid == 0 && time.Now().Before(deadline) {
				if data, err := os.ReadFile(pidFile); err == nil {
					pid, _ = strconv.Atoi(string(data))
				}
				if pid == 0 {
					time.Sleep(10 * time.Millisecond)
				}
			}
			if pid == 0 {
				t.Fatal("child never started")
			}
			child, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
			// An error frame can finish before this test opens the already-dead PID.
			if err != nil && err != windows.ERROR_INVALID_PARAMETER {
				t.Fatal(err)
			}
			if err == nil {
				defer windows.CloseHandle(child)
			}
			if mode == "tree" {
				cancel()
			}
			select {
			case err := <-done:
				if err == nil || (mode != "tree" && ctx.Err() != nil) {
					t.Fatal("failure masked by deadline", err, ctx.Err())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("unbounded helper cleanup")
			}
			if child != 0 {
				status, err := windows.WaitForSingleObject(child, 1000)
				if err != nil || status != windows.WAIT_OBJECT_0 {
					t.Fatal("descendant survived", status, err)
				}
			}
		})
	}
	for i := 0; i < 5; i++ {
		if err := run(); err != nil {
			t.Fatal(err)
		}
	}
	if got := countHandles(); got > baseline {
		t.Fatalf("runner leaked handles: before=%d after=%d", baseline, got)
	}
}
