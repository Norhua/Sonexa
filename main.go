package main

import (
	"embed"
	"log"

	"github.com/norhua/sonexa/internal/app"
	"github.com/wailsapp/wails/v2"
)

//go:embed all:frontend/dist
var assets embed.FS

// Version 是应用版本号，构成 MusicBrainz 要求的 User-Agent 的一部分。
const Version = "0.1.0"

func main() {
	if err := wails.Run(app.DesktopOptions(Version, assets)); err != nil {
		log.Println("Error:", err.Error())
	}
}
