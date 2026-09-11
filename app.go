package main

import (
	"context"
	"log"
	"net/http"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/store"
)

// App 是 Wails 的绑定层：只做参数转换与事件发射，业务逻辑在 internal/ 各包中。
type App struct {
	ctx      context.Context
	version  string
	paths    config.Paths
	settings *config.Store
	store    *store.Store
}

// NewApp 构造绑定层实例。真正的初始化在 startup 中完成。
func NewApp(version string) *App {
	return &App{version: version}
}

// startup 在 Wails 启动时调用：建立数据目录、载入设置、打开数据库并应用迁移。
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	paths, err := config.DefaultPaths()
	if err != nil {
		log.Printf("解析数据目录失败: %v", err)
		return
	}
	if err := paths.Ensure(); err != nil {
		log.Printf("创建数据目录失败: %v", err)
		return
	}
	a.paths = paths

	settings, err := config.NewStore(paths.AppDir)
	if err != nil {
		log.Printf("载入设置失败: %v", err)
		return
	}
	a.settings = settings
	// 用户改过封面目录时以其为准。
	a.paths = paths.WithCoverDir(settings.Get().Cache.CoverDir)
	if err := a.paths.Ensure(); err != nil {
		log.Printf("创建封面缓存目录失败: %v", err)
	}

	st, err := store.Open(a.paths.DBPath)
	if err != nil {
		log.Printf("打开数据库失败: %v", err)
		return
	}
	a.store = st
}

// shutdown 在应用退出时释放资源。
func (a *App) shutdown(ctx context.Context) {
	if a.store != nil {
		if err := a.store.Close(); err != nil {
			log.Printf("关闭数据库失败: %v", err)
		}
	}
}

// CoverAssetHandler 返回供 Wails AssetServer 使用的封面 handler。
func (a *App) CoverAssetHandler() http.Handler {
	return covers.AssetHandler()
}

// Version 返回应用版本号，供「关于」页显示。
func (a *App) Version() string { return a.version }
