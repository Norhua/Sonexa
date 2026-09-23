package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCoverSettingsMovePreservesSelectionAndFailedSave(t *testing.T) {
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
	if _, err := a.coverSvc.Library().ImportLocal(id, coverPNG(t, 801, 801), 0); err != nil {
		t.Fatal(err)
	}
	old := a.paths.CoverDir
	next := a.settings.Get()
	next.Cache.CoverDir = filepath.Join(t.TempDir(), "covers")
	next.Cache.CoverConcurrency = 2
	if err := a.SaveSettings(next); err != nil {
		t.Fatal(err)
	}
	files, _ := a.store.ListReleaseCoverFiles(id)
	if _, err := a.coverSvc.Library().Read(files[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(old, files[0].RelativePath)); err != nil {
		t.Fatal("旧根未保留用于回滚")
	}
	if a.coverSvc.Pool().Concurrency() != 2 {
		t.Fatal("并发未更新")
	}
	bad := next
	bad.Cache.CoverDir = filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(bad.Cache.CoverDir, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveSettings(bad); err == nil {
		t.Fatal("非法目录应拒绝")
	}
	if a.settings.Get().Cache.CoverDir != next.Cache.CoverDir || a.paths.CoverDir != next.Cache.CoverDir {
		t.Fatal("失败切换提交了设置")
	}
	if err := a.ClearCache(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.coverSvc.Library().Read(files[0]); err != nil {
		t.Fatal("清缓存删除采用图")
	}
}
