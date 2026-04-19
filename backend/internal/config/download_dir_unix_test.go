package config

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestResolveUnixDownloadDirUsesEnvironmentOverride(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "home", "alice")
	configHome := filepath.Join(homeDir, ".config")

	resolved, err := resolveUnixDownloadDir(homeDir, "$HOME/Inbox", configHome, func(path string) ([]byte, error) {
		t.Fatalf("expected XDG user dirs file not to be read, got %s", path)
		return nil, nil
	})
	if err != nil {
		t.Fatalf("expected environment override to resolve, got error: %v", err)
	}

	expected := filepath.Join(homeDir, "Inbox")
	if filepath.Clean(resolved) != filepath.Clean(expected) {
		t.Fatalf("expected environment override %s, got %s", expected, resolved)
	}
}

func TestResolveUnixDownloadDirUsesUserDirsFile(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "home", "alice")
	configHome := filepath.Join(homeDir, ".config")
	expectedUserDirsPath := filepath.Join(configHome, "user-dirs.dirs")

	resolved, err := resolveUnixDownloadDir(homeDir, "", configHome, func(path string) ([]byte, error) {
		if filepath.Clean(path) != filepath.Clean(expectedUserDirsPath) {
			t.Fatalf("expected user dirs file %s, got %s", expectedUserDirsPath, path)
		}
		return []byte("# comment\nXDG_DOWNLOAD_DIR=\"$HOME/Downloads\"\n"), nil
	})
	if err != nil {
		t.Fatalf("expected user dirs file to resolve, got error: %v", err)
	}

	expected := filepath.Join(homeDir, "Downloads")
	if filepath.Clean(resolved) != filepath.Clean(expected) {
		t.Fatalf("expected user dirs download path %s, got %s", expected, resolved)
	}
}

func TestResolveUnixDownloadDirFallsBackToHomeDownloads(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "home", "alice")
	configHome := filepath.Join(homeDir, ".config")

	resolved, err := resolveUnixDownloadDir(homeDir, "", configHome, func(path string) ([]byte, error) {
		return nil, errors.New("missing")
	})
	if err != nil {
		t.Fatalf("expected fallback downloads dir to resolve, got error: %v", err)
	}

	expected := filepath.Join(homeDir, "Downloads")
	if filepath.Clean(resolved) != filepath.Clean(expected) {
		t.Fatalf("expected fallback downloads dir %s, got %s", expected, resolved)
	}
}

func TestResolveUnixDownloadDirRejectsEmptyHome(t *testing.T) {
	_, err := resolveUnixDownloadDir("", "", "", func(path string) ([]byte, error) {
		t.Fatalf("expected no file read for empty home dir, got %s", path)
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected empty home dir to return error")
	}
}
