// Package config 负责应用的本地数据位置与用户可配置项。
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// AppDirName 是 os.UserConfigDir() 下的应用专属目录名。
const AppDirName = "Sonexa"

// Paths 汇总应用使用的本地路径。数据库与默认封面缓存目录同级。
type Paths struct {
	AppDir    string // 应用专属目录
	DBPath    string // SQLite 数据库文件
	LyricsDir string // 当前歌词与编辑临时文件
	CoverDir  string // 封面缓存目录（默认与数据库同级，可在设置中改）
}

// DefaultPaths 解析默认路径，但不创建任何目录。
func DefaultPaths() (Paths, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, fmt.Errorf("定位用户配置目录失败: %w", err)
	}
	appDir := filepath.Join(base, AppDirName)
	return Paths{
		AppDir:    appDir,
		DBPath:    filepath.Join(appDir, "sonexa.db"),
		CoverDir:  filepath.Join(appDir, "covers"),
		LyricsDir: filepath.Join(appDir, "lyrics"),
	}, nil
}

// Ensure 创建应用目录与封面缓存目录。首次启动时自动完成，不要求用户选择路径。
func (p Paths) Ensure() error {
	for _, dir := range []string{p.AppDir, p.CoverDir, p.LyricsDir} {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", dir, err)
		}
	}
	return nil
}

// WithCoverDir 返回把封面缓存目录替换为 dir 的副本；dir 为空时保持默认。
func (p Paths) WithCoverDir(dir string) Paths {
	if dir != "" {
		p.CoverDir = dir
	}
	return p
}
