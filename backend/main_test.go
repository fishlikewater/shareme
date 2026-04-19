package main

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestSelectFrontendAssetsPrefersBuiltDist(t *testing.T) {
	assets, err := selectFrontendAssets(fstest.MapFS{
		"frontend/index.html": {
			Data: []byte("placeholder"),
		},
		"frontend/dist/index.html": {
			Data: []byte("built"),
		},
		"frontend/dist/assets/app.js": {
			Data: []byte("console.log('ready')"),
		},
	})
	if err != nil {
		t.Fatalf("selectFrontendAssets() error = %v", err)
	}

	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatalf("ReadFile(index.html) error = %v", err)
	}
	if string(index) != "built" {
		t.Fatalf("expected built frontend assets, got %q", string(index))
	}
}

func TestSelectFrontendAssetsFallsBackToPlaceholderWhenDistMissing(t *testing.T) {
	assets, err := selectFrontendAssets(fstest.MapFS{
		"frontend/index.html": {
			Data: []byte("placeholder"),
		},
	})
	if err != nil {
		t.Fatalf("selectFrontendAssets() error = %v", err)
	}

	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatalf("ReadFile(index.html) error = %v", err)
	}
	if string(index) != "placeholder" {
		t.Fatalf("expected placeholder frontend assets, got %q", string(index))
	}
}
