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

func TestRunnerErrorAndResultReapDescendantsHoldingPipes(t *testing.T) {
	for _, mode := range []string{"tree-bad-frame", "tree-result"} {
		t.Run(mode, func(t *testing.T) {
			r := testRunner(t, mode)
			pidFile := t.TempDir() + "/pid"
			t.Setenv("SONEXA_RUNNER_TEST_PID", pidFile)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if raw, err := r.Run(ctx, testPlaylist, nil); err == nil || raw != nil || ctx.Err() != nil {
				t.Fatal("inherited pipes masked helper failure", string(raw), err, ctx.Err())
			}
			raw, err := os.ReadFile(pidFile)
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(string(raw))
			if err != nil || pid <= 0 {
				t.Fatal(pid, err)
			}
			for i := 0; i < 100; i++ {
				if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("descendant survived helper failure")
		})
	}
}
