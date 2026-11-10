package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type shortFrameWriter struct{ writes, shortOn int }

func (w *shortFrameWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == w.shortOn {
		return len(p) - 1, nil
	}
	return len(p), nil
}

func TestFrameShortWriteAndBoundedDiagnostics(t *testing.T) {
	for _, shortOn := range []int{1, 2} {
		if err := writeFrame(&shortFrameWriter{shortOn: shortOn}, map[string]string{"text": "中文"}); !errors.Is(err, io.ErrShortWrite) {
			t.Fatal(err)
		}
	}
	w := &boundedStderr{}
	for i := 0; i < 32; i++ {
		text := strings.Repeat("private diagnostic", 8192)
		if n, err := w.Write([]byte(text)); err != nil || n != len(text) {
			t.Fatal(n, err)
		}
	}
	if len(w.data) != 64<<10 {
		t.Fatal("unbounded retained stderr", len(w.data))
	}
}

func TestHelperResourceLocations(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct{ executable, expected string }{
		{filepath.Join(root, "build", "bin", "sonexa"), filepath.Join(root, "build", "spotify-helper", "dist", "spotify-helper")},
		{filepath.Join(root, "应用 空格", "sonexa"), filepath.Join(root, "应用 空格", "resources", "spotify-helper")},
	} {
		if got := helperRootForExecutable(tc.executable); got != tc.expected {
			t.Fatal(got, tc.expected)
		}
	}
	if runtime.GOOS == "darwin" {
		contents := filepath.Join(root, "中文.app", "Contents")
		if got := helperRootForExecutable(filepath.Join(contents, "MacOS", "sonexa")); got != filepath.Join(contents, "Resources", "spotify-helper") {
			t.Fatal(got)
		}
	}
}

// Unlike the short deadline smoke test, give invalid output enough time to be
// rejected by the protocol itself; timeout must not make a false positive.
func TestRunnerRejectsOutputBeforeDeadline(t *testing.T) {
	for _, mode := range []string{"wrong-id", "wrong-version", "crash", "bad-frame", "over-output", "trailing", "result-crash", "result-block"} {
		t.Run(mode, func(t *testing.T) {
			r := testRunner(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			raw, err := r.Run(ctx, testPlaylist, func(context.Context, json.RawMessage) (map[string]any, error) { return map[string]any{}, nil })
			if err == nil || raw != nil || ctx.Err() != nil {
				t.Fatalf("protocol rejection masked by timeout: %s %v %v", raw, err, ctx.Err())
			}
		})
	}
}
