package main

import (
	"embed"
	"io/fs"
	"log"

	"shareme/backend/internal/frontendassets"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend
var embeddedFrontendAssets embed.FS

func main() {
	desktopApp, err := NewDesktopApp()
	if err != nil {
		log.Fatal(err)
	}
	frontendAssets, err := selectFrontendAssets(embeddedFrontendAssets)
	if err != nil {
		log.Fatal(err)
	}

	if err := wails.Run(&options.App{
		Title:     "shareme",
		Width:     1360,
		Height:    900,
		MinWidth:  1080,
		MinHeight: 720,
		AssetServer: &assetserver.Options{
			Assets: frontendAssets,
		},
		OnStartup:  desktopApp.Startup,
		OnShutdown: desktopApp.Shutdown,
		Bind: []any{
			desktopApp,
		},
	}); err != nil {
		log.Fatal(err)
	}
}

func selectFrontendAssets(assetFS fs.FS) (fs.FS, error) {
	return frontendassets.Select(assetFS)
}
