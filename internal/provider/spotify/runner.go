package spotify

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
)

const protocolVersion = 1

type Runner struct{ root string }
type manifest struct {
	Protocol int               `json:"protocol"`
	Target   string            `json:"target"`
	Python   string            `json:"python"`
	Scraper  string            `json:"spotifyScraper"`
	Files    map[string]string `json:"files"`
}

func helperRoot() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(executable)
	if runtime.GOOS == "darwin" && filepath.Base(dir) == "MacOS" && filepath.Base(filepath.Dir(dir)) == "Contents" {
		return filepath.Join(filepath.Dir(dir), "Resources", "spotify-helper"), nil
	}
	// Wails development binary resides in build/bin; never search PATH/cwd.
	if filepath.Base(dir) == "bin" && filepath.Base(filepath.Dir(dir)) == "build" {
		return filepath.Join(filepath.Dir(dir), "spotify-helper", "dist", "spotify-helper"), nil
	}
	return filepath.Join(dir, "resources", "spotify-helper"), nil
}

func (r *Runner) validate() (string, error) {
	bad := func() (string, error) {
		return "", playlists.Failure(playlists.RuntimeUnavailable, "Spotify 运行环境缺失、损坏或版本不匹配")
	}
	root := r.root
	if root == "" {
		var err error
		root, err = helperRoot()
		if err != nil {
			return bad()
		}
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return bad()
	}
	manifestFile, err := os.Open(filepath.Join(root, "manifest.json"))
	if err != nil {
		return bad()
	}
	raw, err := io.ReadAll(io.LimitReader(manifestFile, (4<<20)+1))
	_ = manifestFile.Close()
	if err != nil || len(raw) > 4<<20 {
		return bad()
	}
	var m manifest
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	osname := runtime.GOOS
	if osname == "windows" {
		osname = "win32"
	}
	if json.Unmarshal(raw, &m) != nil || m.Protocol != protocolVersion || m.Target != osname+"-"+arch || m.Python != "3.12.13" || m.Scraper != "3.9.2" || len(m.Files) == 0 {
		return bad()
	}
	name := "spotify-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if _, ok := m.Files[name]; !ok {
		return bad()
	}
	for relative, expected := range m.Files {
		if relative == "manifest.json" || filepath.IsAbs(relative) || strings.Contains(relative, "\\") || filepath.ToSlash(filepath.Clean(relative)) != relative || relative == ".." || strings.HasPrefix(relative, "../") {
			return bad()
		}
		path, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return bad()
		}
		within, err := filepath.Rel(root, path)
		if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
			return bad()
		}
		f, err := os.Open(path)
		if err != nil {
			return bad()
		}
		h := sha256.New()
		_, err = io.Copy(h, io.LimitReader(f, 512<<20))
		_ = f.Close()
		if err != nil || hex.EncodeToString(h.Sum(nil)) != expected {
			return bad()
		}
	}
	// Unlisted files can inject Python modules/native dependencies, even if all
	// expected files still hash correctly. Do not silently accept extra files.
	err = filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if rel == "manifest.json" {
			return nil
		}
		if _, ok := m.Files[rel]; !ok {
			return fmt.Errorf("unlisted resource")
		}
		return nil
	})
	if err != nil {
		return bad()
	}
	return filepath.Join(root, name), nil
}

func safeEnvironment() []string {
	out := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "PYTHON") || strings.HasPrefix(key, "DYLD_") || strings.HasPrefix(key, "LD_") || strings.HasPrefix(key, "_PYI_") || strings.HasPrefix(key, "PYINSTALLER_") || strings.Contains(key, "PROXY") {
			continue
		}
		out = append(out, entry)
	}
	return out
}

type boundedStderr struct {
	mu   sync.Mutex
	data []byte
}

func (w *boundedStderr) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	remaining := (64 << 10) - len(w.data)
	if remaining > 0 {
		w.data = append(w.data, p[:min(remaining, n)]...)
	}
	return n, nil
}

func readFrame(reader io.Reader) (json.RawMessage, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > playlists.MaxSnapshotBytes {
		return nil, fmt.Errorf("frame too large")
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("invalid frame")
	}
	return body, nil
}
func writeFrame(writer io.Writer, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(body) > playlists.MaxSnapshotBytes {
		return fmt.Errorf("frame too large")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(body)))
	if _, err = writer.Write(header[:]); err != nil {
		return err
	}
	_, err = writer.Write(body)
	return err
}

func (r *Runner) Run(ctx context.Context, ref string, exchange func(context.Context, json.RawMessage) (map[string]any, error)) (json.RawMessage, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	executable, err := r.validate()
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, mapError(err)
	}
	requestBytes := make([]byte, 16)
	if _, err = rand.Read(requestBytes); err != nil {
		return nil, err
	}
	requestID := hex.EncodeToString(requestBytes)
	cmd := exec.CommandContext(ctx, executable)
	cmd.Dir = filepath.Dir(executable)
	cmd.Env = safeEnvironment()
	cmd.Stderr = &boundedStderr{}
	prepareProcess(cmd)
	cmd.WaitDelay = 3 * time.Second
	cmd.Cancel = func() error { return terminateProcess(cmd) }
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, mapError(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, mapError(err)
	}
	if err = cmd.Start(); err != nil {
		return nil, playlists.Failure(playlists.RuntimeUnavailable, "Spotify 辅助程序无法启动")
	}
	cleanup, err := containProcess(cmd)
	if err != nil {
		_ = terminateProcess(cmd)
		_ = cmd.Wait()
		return nil, mapError(err)
	}
	var cleanupOnce sync.Once
	clean := func() { cleanupOnce.Do(cleanup) }
	watchDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			clean()
			_ = terminateProcess(cmd)
			_ = stdout.Close()
			_ = stdin.Close()
		case <-watchDone:
		}
	}()
	waited := false
	defer func() {
		close(watchDone)
		_ = stdin.Close()
		_ = stdout.Close()
		if !waited {
			_ = terminateProcess(cmd)
			_ = cmd.Wait()
		}
		clean()
	}()
	if err = writeFrame(stdin, map[string]any{"protocol": protocolVersion, "kind": "playlist.read", "requestId": requestID, "playlistRef": ref}); err != nil {
		return nil, mapError(err)
	}
	reader := io.LimitReader(stdout, 64<<20)
	hello := false
	sequence := 0
	for {
		frame, err := readFrame(reader)
		if err != nil {
			if ctx.Err() != nil {
				return nil, mapError(ctx.Err())
			}
			return nil, playlists.Failure(playlists.Incomplete, "Spotify 辅助程序通信中断或响应无效")
		}
		var message struct {
			Protocol  int                 `json:"protocol"`
			RequestID string              `json:"requestId"`
			Kind      string              `json:"kind"`
			Sequence  int                 `json:"sequence"`
			Snapshot  json.RawMessage     `json:"snapshot"`
			Code      playlists.ErrorCode `json:"code"`
			Python    string              `json:"python"`
			Scraper   string              `json:"spotifyScraper"`
			Frozen    bool                `json:"frozen"`
		}
		if json.Unmarshal(frame, &message) != nil || message.Protocol != protocolVersion || message.RequestID != requestID {
			return nil, playlists.Failure(playlists.Incomplete, "Spotify 辅助程序响应标识不匹配")
		}
		switch message.Kind {
		case "hello":
			if hello || message.Python != "3.12.13" || message.Scraper != "3.9.2" || !message.Frozen {
				return nil, playlists.Failure(playlists.RuntimeUnavailable, "Spotify 辅助程序握手不匹配")
			}
			hello = true
		case "http.request":
			if !hello || message.Sequence != sequence+1 || sequence >= playlists.MaxSends {
				return nil, playlists.Failure(playlists.Incomplete, "Spotify 请求序号无效")
			}
			sequence++
			response, err := exchange(ctx, frame)
			if err != nil {
				return nil, err
			}
			response["protocol"], response["kind"], response["requestId"], response["sequence"] = protocolVersion, "http.response", requestID, sequence
			if err = writeFrame(stdin, response); err != nil {
				return nil, mapError(err)
			}
		case "result":
			if !hello || len(message.Snapshot) == 0 {
				return nil, playlists.Failure(playlists.Incomplete, "Spotify 返回无效结果")
			}
			_ = stdin.Close()
			// A result is not success until the helper ends cleanly without extra
			// stdout. Bound EOF and process exit independently of the 15m fetch.
			finished := make(chan error, 1)
			go func() {
				var extra [1]byte
				n, e := stdout.Read(extra[:])
				if n != 0 || e != io.EOF {
					_ = terminateProcess(cmd)
					_ = cmd.Wait()
					finished <- fmt.Errorf("unexpected trailing output")
					return
				}
				finished <- cmd.Wait()
			}()
			select {
			case err := <-finished:
				waited = true
				if err != nil {
					return nil, playlists.Failure(playlists.Incomplete, "Spotify 辅助程序异常退出")
				}
			case <-time.After(3 * time.Second):
				_ = terminateProcess(cmd)
				_ = stdout.Close()
				<-finished
				waited = true
				return nil, playlists.Failure(playlists.Incomplete, "Spotify 辅助程序退出超时")
			}
			if ctx.Err() != nil {
				return nil, mapError(ctx.Err())
			}
			return message.Snapshot, nil
		case "error":
			code := message.Code
			switch code {
			case playlists.Input, playlists.Access, playlists.Restricted, playlists.Incomplete, playlists.Temporary, playlists.Cancelled, playlists.RuntimeUnavailable, playlists.Resource:
			default:
				code = playlists.Incomplete
			}
			return nil, playlists.Failure(code, "Spotify 辅助程序未能完成读取")
		default:
			return nil, playlists.Failure(playlists.Incomplete, "Spotify 辅助程序消息类型无效")
		}
	}
}
