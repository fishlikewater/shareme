package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embeddedAssets embed.FS

func Assets() fs.FS {
	assets, err := fs.Sub(embeddedAssets, "dist")
	if err != nil {
		panic("web ui assets directory is missing")
	}
	return assets
}
