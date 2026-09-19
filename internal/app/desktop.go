package app

import (
	"io/fs"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// DesktopOptions 装配桌面外壳与应用绑定。
// 用包级函数接线，避免为了 main 包访问而导出 App 的生命周期/HTTP 方法，
// 这些内部方法不应变成可从前端调用的 Wails 绑定。
func DesktopOptions(version string, assets fs.FS) *options.App {
	app := NewApp(version)
	return &options.App{
		Title:     "Sonexa",
		Width:     1280,
		Height:    860,
		MinWidth:  1024,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: app.coverAssetHandler(),
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:       []interface{}{app},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
	}
}
