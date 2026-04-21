package frontendassets

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestSelectPrefersBuiltDist(t *testing.T) {
	assets, err := Select(fstest.MapFS{
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
		t.Fatalf("Select() error = %v", err)
	}

	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatalf("ReadFile(index.html) error = %v", err)
	}
	if string(index) != "built" {
		t.Fatalf("expected built frontend assets, got %q", string(index))
	}
}

func TestSelectFallsBackToPlaceholderWhenDistMissing(t *testing.T) {
	assets, err := Select(fstest.MapFS{
		"frontend/index.html": {
			Data: []byte("placeholder"),
		},
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}

	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatalf("ReadFile(index.html) error = %v", err)
	}
	if string(index) != "placeholder" {
		t.Fatalf("expected placeholder frontend assets, got %q", string(index))
	}
}
