package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultRootDirName = ".shareme"

type Layout struct {
	RootDir          string
	ConfigFilePath   string
	IdentityFilePath string
	DatabasePath     string
	LogDir           string
	TempDir          string
	DownloadsDir     string
}

func ResolveDefaultLayout() (Layout, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, fmt.Errorf("resolve user home dir: %w", err)
	}
	return ResolveLayout(filepath.Join(homeDir, defaultRootDirName)), nil
}

func ResolveLayout(rootDir string) Layout {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		rootDir = defaultRootDirName
	}
	rootDir = filepath.Clean(rootDir)

	return Layout{
		RootDir:          rootDir,
		ConfigFilePath:   filepath.Join(rootDir, "config.json"),
		IdentityFilePath: filepath.Join(rootDir, "local-device.json"),
		DatabasePath:     filepath.Join(rootDir, "shareme.db"),
		LogDir:           filepath.Join(rootDir, "logs"),
		TempDir:          filepath.Join(rootDir, "tmp"),
		DownloadsDir:     filepath.Join(rootDir, "downloads"),
	}
}

func ensureLayout(layout Layout) error {
	for _, dir := range []string{
		layout.RootDir,
		layout.DownloadsDir,
		layout.LogDir,
		layout.TempDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create layout dir %q: %w", dir, err)
		}
	}
	return nil
}
