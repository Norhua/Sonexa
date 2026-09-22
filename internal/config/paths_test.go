package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultPathsUnderUserConfigDir 覆盖 2.1：
// 数据库位于 os.UserConfigDir() 下的应用专属目录，封面缓存目录默认与之同级。
func TestDefaultPathsUnderUserConfigDir(t *testing.T) {
	base, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("本平台无用户配置目录: %v", err)
	}

	p, err := DefaultPaths()
	if err != nil {
		t.Fatalf("解析默认路径失败: %v", err)
	}
	if !strings.HasPrefix(p.AppDir, base) {
		t.Errorf("应用目录 %q 不在用户配置目录 %q 下", p.AppDir, base)
	}
	if filepath.Dir(p.DBPath) != p.AppDir {
		t.Errorf("数据库 %q 不在应用目录内", p.DBPath)
	}
	// 「封面缓存目录默认与数据库同级」
	if filepath.Dir(p.CoverDir) != filepath.Dir(p.DBPath) {
		t.Errorf("封面目录 %q 与数据库 %q 不同级", p.CoverDir, p.DBPath)
	}
}

// TestEnsureCreatesDirectories 覆盖「首次启动创建数据目录」：
// 自动创建，不要求用户选择路径。
func TestEnsureCreatesDirectories(t *testing.T) {
	root := t.TempDir()
	p := Paths{
		AppDir:   filepath.Join(root, "Sonexa"),
		DBPath:   filepath.Join(root, "Sonexa", "sonexa.db"),
		CoverDir: filepath.Join(root, "Sonexa", "covers"),
	}

	if err := p.Ensure(); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	for _, dir := range []string{p.AppDir, p.CoverDir} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("目录 %q 未被创建: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q 不是目录", dir)
		}
	}
	// 重复调用是无操作，不应报错
	if err := p.Ensure(); err != nil {
		t.Errorf("重复创建应无操作, 得到 %v", err)
	}
}

// TestWithCoverDirOverride 覆盖「用户更改封面缓存目录」。
func TestWithCoverDirOverride(t *testing.T) {
	p := Paths{AppDir: "/a", DBPath: "/a/db", CoverDir: "/a/covers"}

	moved := p.WithCoverDir("/elsewhere/covers")
	if moved.CoverDir != "/elsewhere/covers" {
		t.Errorf("封面目录 = %q, 期望 /elsewhere/covers", moved.CoverDir)
	}
	if moved.DBPath != p.DBPath {
		t.Error("更改封面目录不应影响数据库位置")
	}
	// 空值保持默认
	if kept := p.WithCoverDir(""); kept.CoverDir != "/a/covers" {
		t.Errorf("空值应保持默认, 得到 %q", kept.CoverDir)
	}
}

// TestSettingsDefaultsRoundTrip 确保设置可落盘并读回，且未知字段保持默认。
func TestSettingsDefaultsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("建立设置存储失败: %v", err)
	}

	got := s.Get()
	if got.Matching.VariantStrategy != VariantProgressive {
		t.Errorf("变体策略默认 = %q, 期望 progressive", got.Matching.VariantStrategy)
	}
	if got.Matching.MaxRetries != 5 {
		t.Errorf("最大重试 = %d, 期望 5", got.Matching.MaxRetries)
	}
	if got.Cache.CoverConcurrency != 8 {
		t.Errorf("封面并发 = %d, 期望 8", got.Cache.CoverConcurrency)
	}
	if !got.ImportExport.ExportUTF8BOM {
		t.Error("导出应默认使用带 BOM 的 UTF-8")
	}

	got.MusicBrainz.Contact = "test@example.com"
	if err := s.Save(got); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	reloaded, err := NewStore(dir)
	if err != nil {
		t.Fatalf("重新载入失败: %v", err)
	}
	if reloaded.Get().MusicBrainz.Contact != "test@example.com" {
		t.Error("联系方式未被持久化")
	}
	// 未写入文件的项仍保持默认
	if reloaded.Get().Matching.MaxRetries != 5 {
		t.Error("未修改项应保持默认值")
	}
}

// TestPartialSettingsFileKeepsDefaults 覆盖「后续版本新增设置项」：
// 老配置文件缺少的字段保持默认，不需要迁移。
func TestPartialSettingsFileKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	partial := `{"musicbrainz":{"contact":"a@b.c"}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(partial), 0o644); err != nil {
		t.Fatalf("写入部分配置失败: %v", err)
	}

	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("载入失败: %v", err)
	}
	got := s.Get()
	if got.MusicBrainz.Contact != "a@b.c" {
		t.Error("已写入的字段应被读出")
	}
	if got.MusicBrainz.RatePerSec != 1.0 {
		t.Errorf("缺失的速率应取默认 1.0, 得到 %v", got.MusicBrainz.RatePerSec)
	}
	if got.Matching.MaxRetries != 5 {
		t.Errorf("缺失的重试次数应取默认 5, 得到 %d", got.Matching.MaxRetries)
	}
}
