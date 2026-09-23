package app

import (
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/store"
)

func TestClearCoverRemovesFilesAndStopsAutoFetch(t *testing.T) {
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
	selection, err := a.coverSvc.Library().ImportLocal(id, coverPNG(t, 900, 900), 0)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := a.store.ListReleaseCoverFiles(id)
	if len(files) != 1 {
		t.Fatal("采用未落盘")
	}
	if _, err := a.ClearCover(id, selection.Revision+5); !errors.Is(err, store.ErrCoverSelectionChanged) {
		t.Fatal("过期版本应被拒绝", err)
	}
	result, err := a.ClearCover(id, selection.Revision)
	if err != nil || result.Revision != selection.Revision+1 {
		t.Fatal(result, err)
	}
	if _, err := a.coverSvc.Library().Read(files[0]); err == nil {
		t.Fatal("封面文件未删除")
	}
	if left, _ := a.store.ListReleaseCoverFiles(id); len(left) != 0 {
		t.Fatal("文件记录未删除")
	}
	got, _ := a.store.GetCoverSelection(id)
	if got == nil || !got.Cleared {
		t.Fatal("未记录已清除")
	}
	// 清除后可以按新版本重新采用。
	if _, err := a.coverSvc.Library().ImportLocal(id, coverPNG(t, 300, 300), result.Revision); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.store.GetCoverSelection(id); got.Cleared {
		t.Fatal("重新采用后仍是已清除")
	}
}
