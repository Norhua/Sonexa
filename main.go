package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

// Version 是应用版本号，构成 MusicBrainz 要求的 User-Agent 的一部分（见 internal/provider）。
const Version = "0.1.0"

func main() {
	app := NewApp(Version)

	err := wails.Run(&options.App{
		Title:     "Sonexa",
		Width:     1280,
		Height:    860,
		MinWidth:  1024,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
			// 封面字节经 AssetServer 以本地 URL 供 <img> 加载，不经 IPC 桥回传。
			Handler: app.CoverAssetHandler(),
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:       []interface{}{app},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
	})
	if err != nil {
		log.Println("Error:", err.Error())
	}
}
