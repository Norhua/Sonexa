package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/norhua/sonexa/internal/lyricdoc"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type LyricsPickResult struct {
	Cancelled bool  `json:"cancelled"`
	Revision  int64 `json:"revision"`
}

// PickAndAdoptLyrics is the local TXT/LRC/XLRC input adapter. Online documents never
// enter this path: only local file bytes are parsed/normalized here.
func (a *App) PickAndAdoptLyrics(trackID, revision int64) (*LyricsPickResult, error) {
	if _, err := a.LyricsRevision(trackID); err != nil {
		return nil, err
	}
	pick := a.pickLyricsFile
	if pick == nil {
		pick = wruntime.OpenFileDialog
	}
	path, err := pick(a.ctx, wruntime.OpenDialogOptions{Title: "选择歌词", Filters: []wruntime.FileFilter{{DisplayName: "UTF-8 歌词（TXT / LRC / XLRC）", Pattern: "*.txt;*.lrc;*.xlrc"}}})
	if err != nil {
		return nil, err
	}
	if path == "" {
		return &LyricsPickResult{Cancelled: true, Revision: revision}, nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".txt" && ext != ".lrc" && ext != ".xlrc" {
		return nil, fmt.Errorf("请选择 UTF-8 编码的 .txt、.lrc 或 .xlrc 文件")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > lyricdoc.MaxTextBytes {
		return nil, fmt.Errorf("歌词必须是 2 MiB 以内的普通文本文件")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > lyricdoc.MaxTextBytes {
		return nil, fmt.Errorf("歌词必须是 2 MiB 以内的普通文本文件")
	}
	body, err := io.ReadAll(io.LimitReader(f, lyricdoc.MaxTextBytes+1))
	if err != nil {
		return nil, err
	}
	var doc *lyricdoc.Document
	if ext == ".xlrc" {
		doc, err = lyricdoc.ParseXLRC(string(body))
	} else {
		doc, err = lyricdoc.ParseText(string(body))
	}
	if err != nil {
		return nil, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	rev, err := a.saveLyrics(ctx, trackID, doc, revision)
	if err != nil {
		return nil, err
	}
	return &LyricsPickResult{Revision: rev}, nil
}
