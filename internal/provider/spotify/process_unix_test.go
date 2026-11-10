//go:build !windows

package spotify

import (
	"context"
	"errors"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
)

func TestRunnerCancelsProcessTree(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "timeout"}[timeout], func(t *testing.T) {
			testRunnerTreeRecovery(t, timeout)
		})
	}
}

func testRunnerTreeRecovery(t *testing.T, timeout bool) {
	t.Helper()
	r := testRunner(t, "tree")
	pidFile := t.TempDir() + "/pid"
	t.Setenv("SONEXA_RUNNER_TEST_PID", pidFile)
	ctx, cancel := context.WithCancel(context.Background())
	if timeout {
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	}
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := r.Run(ctx, testPlaylist, nil); done <- err }()
	deadline := time.After(5 * time.Second)
	var pid int
	for pid == 0 {
		if raw, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(string(raw))
		}
		select {
		case <-deadline:
			t.Fatal("helper did not start child")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if !timeout {
		cancel()
	}
	select {
	case err := <-done:
		if playlists.Code(err) != playlists.Cancelled {
			t.Fatal("interrupted helper did not return cancellation", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("unbounded process cleanup")
	}
	for i := 0; i < 100; i++ {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			// A fresh process can run after cancellation/timeout; no stale
			// pipe, process group or output is reused by the Runner.
			t.Setenv("SONEXA_RUNNER_TEST_PROCESS", "stderr")
			retry, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if raw, err := r.Run(retry, testPlaylist, nil); err != nil || string(raw) != `{"ok":true}` {
				t.Fatal("fresh helper failed after interruption", string(raw), err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("descendant survived cancellation")
}
