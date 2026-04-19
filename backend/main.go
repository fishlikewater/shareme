package main

import (
	"embed"
	"io/fs"
	"log"

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
		Title:     "Message Share",
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
	if distAssets, err := fs.Sub(assetFS, "frontend/dist"); err == nil {
		if _, err := fs.Stat(distAssets, "index.html"); err == nil {
			return distAssets, nil
		}
	}

	placeholderAssets, err := fs.Sub(assetFS, "frontend")
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(placeholderAssets, "index.html"); err != nil {
		return nil, err
	}
	return placeholderAssets, nil
}
