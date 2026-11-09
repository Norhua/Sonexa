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
)

func TestRunnerCancelsProcessTree(t *testing.T) {
	r := testRunner(t, "tree")
	pidFile := t.TempDir() + "/pid"
	t.Setenv("SONEXA_RUNNER_TEST_PID", pidFile)
	ctx, cancel := context.WithCancel(context.Background())
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
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancel accepted as success")
	}
	for i := 0; i < 100; i++ {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("descendant survived cancellation")
}
