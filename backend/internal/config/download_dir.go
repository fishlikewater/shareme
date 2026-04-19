package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	systemDownloadDirResolver = resolveSystemDownloadDir
	downloadDirMkdirAll       = os.MkdirAll
	downloadDirMkdirTemp      = os.MkdirTemp
	downloadDirCreateTemp     = func(dir string, pattern string) (*os.File, error) {
		return os.CreateTemp(dir, pattern)
	}
)

func resolveDefaultDownloadDir(dataDir string) (string, error) {
	var systemErr error
	if systemDir, err := systemDownloadDirResolver(); err == nil {
		if err := ensureDownloadDirUsable(systemDir); err == nil {
			return systemDir, nil
		}
		systemErr = fmt.Errorf("system downloads dir %q is not usable: %w", systemDir, err)
	} else {
		systemErr = fmt.Errorf("system downloads dir is unavailable: %w", err)
	}

	fallbackDir := filepath.Join(dataDir, "downloads")
	if err := ensureDownloadDirUsable(fallbackDir); err != nil {
		if systemErr != nil {
			return "", fmt.Errorf("%v; fallback downloads dir %q is not usable: %w", systemErr, fallbackDir, err)
		}
		return "", fmt.Errorf("fallback downloads dir %q is not usable: %w", fallbackDir, err)
	}
	return fallbackDir, nil
}

func resolveGuaranteedDownloadDir(dataDir string) string {
	if resolved, err := resolveDefaultDownloadDir(dataDir); err == nil {
		return resolved
	}

	if layout, err := ResolveDefaultLayout(); err == nil {
		fallbackDir := layout.DownloadsDir
		if filepath.Clean(fallbackDir) != filepath.Clean(filepath.Join(dataDir, "downloads")) {
			if err := ensureDownloadDirUsable(fallbackDir); err == nil {
				return fallbackDir
			}
		}
	}

	tempDir, err := downloadDirMkdirTemp("", "message-share-downloads-*")
	if err == nil {
		if ensureErr := ensureDownloadDirUsable(tempDir); ensureErr == nil {
			return tempDir
		}
		_ = os.RemoveAll(tempDir)
	}

	panic(fmt.Errorf("resolve guaranteed download dir: no usable download dir available"))
}

func resolveConfiguredDownloadDir(configured string, dataDir string) string {
	if value := strings.TrimSpace(os.Getenv("MESSAGE_SHARE_DOWNLOAD_DIR")); value != "" {
		configured = value
	}

	if value := strings.TrimSpace(configured); value != "" {
		if err := ensureDownloadDirUsable(value); err == nil {
			return value
		}
	}

	if resolved, err := resolveDefaultDownloadDir(dataDir); err == nil {
		return resolved
	}

	return resolveGuaranteedDownloadDir(dataDir)
}

func resolveSystemDownloadDir() (string, error) {
	dir, err := resolvePlatformDownloadDir()
	if err != nil {
		return "", err
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("system downloads dir is empty")
	}
	return dir, nil
}

func ensureDownloadDirUsable(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("download dir is empty")
	}
	if err := downloadDirMkdirAll(dir, 0o755); err != nil {
		return err
	}

	tempFile, err := downloadDirCreateTemp(dir, ".message-share-write-check-*.tmp")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Remove(tempPath); err != nil {
		return err
	}
	return nil
}
