package spotify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("SONEXA_RUNNER_TEST_PROCESS"); mode != "" {
		runTestProcess(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runTestProcess(mode string) {
	if mode == "grandchild" {
		for {
			time.Sleep(time.Hour)
		}
	}
	raw, err := readFrame(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	var request map[string]any
	_ = json.Unmarshal(raw, &request)
	id := request["requestId"]
	if mode == "wrong-id" {
		id = "wrong"
	}
	hello := map[string]any{"protocol": 1, "kind": "hello", "requestId": id, "python": "3.12.13", "spotifyScraper": "3.9.2", "frozen": true}
	if mode == "wrong-version" {
		hello["python"] = "3.14.8"
	}
	_ = writeFrame(os.Stdout, hello)
	switch mode {
	case "crash":
		os.Exit(2)
	case "bad-frame":
		_, _ = os.Stdout.Write([]byte{255, 255, 255, 255})
		return
	case "over-output":
		for i := 0; i < 520; i++ {
			_ = writeFrame(os.Stdout, map[string]any{"protocol": 1, "kind": "http.request", "requestId": id, "sequence": i + 1, "padding": strings.Repeat("x", 150000)})
		}
		return
	case "block":
		for {
			time.Sleep(time.Hour)
		}
	case "tree":
		executable, _ := os.Executable()
		cmd := exec.Command(executable)
		cmd.Env = []string{}
		for _, entry := range safeEnvironment() {
			if !strings.HasPrefix(entry, "SONEXA_RUNNER_TEST_PROCESS=") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		cmd.Env = append(cmd.Env, "SONEXA_RUNNER_TEST_PROCESS=grandchild")
		if err := cmd.Start(); err != nil {
			os.Exit(3)
		}
		_ = os.WriteFile(os.Getenv("SONEXA_RUNNER_TEST_PID"), []byte(fmt.Sprint(cmd.Process.Pid)), 0600)
		for {
			time.Sleep(time.Hour)
		}
	case "stderr":
		_, _ = io.WriteString(os.Stderr, strings.Repeat("private diagnostics", 10000))
	case "request":
		_ = writeFrame(os.Stdout, map[string]any{"protocol": 1, "kind": "http.request", "requestId": id, "sequence": 1, "method": "GET", "url": "https://open.spotify.com/embed/playlist/" + testPlaylist})
		if _, err := readFrame(os.Stdin); err != nil {
			os.Exit(2)
		}
	}
	_ = writeFrame(os.Stdout, map[string]any{"protocol": 1, "kind": "result", "requestId": id, "snapshot": map[string]any{"ok": true}})
	if mode == "trailing" {
		_, _ = os.Stdout.Write([]byte("unexpected trailing bytes"))
	}
	if mode == "result-crash" {
		os.Exit(2)
	}
	if mode == "result-block" {
		for {
			time.Sleep(time.Hour)
		}
	}
}

func testRunner(t *testing.T, mode string) *Runner {
	t.Helper()
	root := filepath.Join(t.TempDir(), "中文 helper 空格")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	name := "spotify-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(root, name), data, 0700); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	osname := runtime.GOOS
	if osname == "windows" {
		osname = "win32"
	}
	m := manifest{Protocol: 1, Target: osname + "-" + arch, Python: "3.12.13", Scraper: "3.9.2", Files: map[string]string{name: hex.EncodeToString(hash[:])}}
	raw, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SONEXA_RUNNER_TEST_PROCESS", mode)
	return &Runner{root: root}
}

func TestRunnerProcessFailuresAndCancellation(t *testing.T) {
	for _, mode := range []string{"wrong-id", "wrong-version", "crash", "bad-frame", "over-output", "trailing", "result-crash", "result-block", "block"} {
		t.Run(mode, func(t *testing.T) {
			r := testRunner(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			start := time.Now()
			if raw, err := r.Run(ctx, testPlaylist, func(context.Context, json.RawMessage) (map[string]any, error) { return map[string]any{}, nil }); err == nil || raw != nil {
				t.Fatalf("mode %s succeeded: %s %v", mode, raw, err)
			}
			if time.Since(start) > 4*time.Second {
				t.Fatal("unbounded process cleanup")
			}
		})
	}
}

func TestRunnerBoundedStderrAndArgumentData(t *testing.T) {
	r := testRunner(t, "stderr")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("PYTHONPATH", "malicious")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if raw, err := r.Run(ctx, ";touch should-not-run", nil); err != nil || string(raw) != "{\"ok\":true}" {
		t.Fatalf("%s %v", raw, err)
	}
}

func TestRunnerManifestRejectsMismatches(t *testing.T) {
	for _, mode := range []string{"corrupt", "wrong-arch", "extra", "escape"} {
		t.Run(mode, func(t *testing.T) {
			r := testRunner(t, "stderr")
			path := filepath.Join(r.root, "manifest.json")
			raw, _ := os.ReadFile(path)
			var m manifest
			_ = json.Unmarshal(raw, &m)
			switch mode {
			case "corrupt":
				for key := range m.Files {
					m.Files[key] = strings.Repeat("0", 64)
				}
			case "wrong-arch":
				m.Target = "unsupported-arch"
			case "extra":
				_ = os.WriteFile(filepath.Join(r.root, "injected.py"), []byte("bad"), 0600)
			case "escape":
				m.Files["../escape"] = "bad"
			}
			raw, _ = json.Marshal(m)
			_ = os.WriteFile(path, raw, 0600)
			if _, err := r.validate(); playlists.Code(err) != playlists.RuntimeUnavailable {
				t.Fatal(err)
			}
		})
	}
}
