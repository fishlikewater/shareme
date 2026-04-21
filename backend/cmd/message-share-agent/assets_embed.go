package main

import "embed"

//go:embed all:frontend
var embeddedFrontendAssets embed.FS
