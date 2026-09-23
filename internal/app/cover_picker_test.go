package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/store"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func coverPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestPickAndAdoptCoverUsesSystemPickerAndCopiesWithoutNetwork(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	defer a.coverSvc.Close()
	settings := a.settings.Get()
	settings.MusicBrainz.Contact = ""
	if err := a.settings.Save(settings); err != nil {
		t.Fatal(err)
	}
	track, err := a.store.CreateTrack(&store.Track{Title: "song", Album: strPtrOrNil("album")})
	if err != nil {
		t.Fatal(err)
	}
	row, _ := a.store.GetTrack(track)
	dir := t.TempDir()
	source := filepath.Join(dir, "original.png")
	data := coverPNG(t, 800, 600)
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	a.pickCoverFile = func(_ context.Context, options wruntime.OpenDialogOptions) (string, error) {
		calls++
		if !strings.Contains(options.Filters[0].Pattern, "*.webp") {
			t.Fatal("图片过滤器不完整")
		}
		return source, nil
	}
	result, err := a.PickAndAdoptCover(*row.ReleaseID, 0)
	if err != nil || result.Cancelled || result.Revision != 1 || calls != 1 {
		t.Fatalf("导入失败: %+v %v", result, err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	files, _ := a.store.ListReleaseCoverFiles(*row.ReleaseID)
	if len(files) != 1 || files[0].Width != 800 || !strings.Contains(files[0].RelativePath, "-local-") {
		t.Fatalf("未按本地来源保存: %+v", files)
	}
	if got, err := a.coverSvc.Library().Read(files[0]); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("没有独立副本: %v", err)
	}
	selection, _ := a.store.GetCoverSelection(*row.ReleaseID)
	for _, mode := range []string{"cancel", "corrupt", "permission"} {
		a.pickCoverFile = func(context.Context, wruntime.OpenDialogOptions) (string, error) {
			if mode == "cancel" {
				return "", nil
			}
			if mode == "permission" {
				return "", os.ErrPermission
			}
			return source, nil
		}
		if err := os.WriteFile(source, []byte("not an image"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := a.PickAndAdoptCover(*row.ReleaseID, 1)
		if mode == "cancel" {
			if err != nil || !got.Cancelled {
				t.Fatal("取消应无错误")
			}
		} else if err == nil {
			t.Fatal("非法文件应拒绝")
		}
		after, _ := a.store.GetCoverSelection(*row.ReleaseID)
		if after.Revision != selection.Revision {
			t.Fatal("失败改变了采用关系")
		}
	}
	a.pickCoverFile = func(context.Context, wruntime.OpenDialogOptions) (string, error) {
		t.Fatal("无歌集不应打开选择器")
		return "", nil
	}
	if _, err := a.PickAndAdoptCover(9999, 0); err == nil {
		t.Fatal("无歌集应拒绝")
	}
}

func TestAboutKeepsAttributionWithoutITunesExpiryClaims(t *testing.T) {
	about := NewApp("test").About()
	seen := map[string]bool{}
	for _, entry := range about.Attributions {
		seen[entry.Name] = true
		if strings.Contains(entry.Text, "短期缓存") || strings.Contains(entry.Text, "长期事实") {
			t.Fatal(entry.Text)
		}
		if !strings.HasPrefix(entry.URL, "https://") {
			t.Fatal(entry.URL)
		}
	}
	for _, name := range []string{"Apple / iTunes", "QQ 音乐", "网易云音乐", "Cover Art Archive"} {
		if !seen[name] {
			t.Fatal(name)
		}
	}
}

func TestPickerErrorPropagates(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	defer a.coverSvc.Close()
	id, err := a.store.FindOrCreateLocalRelease("album", nil)
	if err != nil {
		t.Fatal(err)
	}
	a.pickCoverFile = func(context.Context, wruntime.OpenDialogOptions) (string, error) { return "", os.ErrPermission }
	if _, err := a.PickAndAdoptCover(id, 0); !errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
}
