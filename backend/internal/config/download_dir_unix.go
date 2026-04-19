package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolveUnixDownloadDir(
	homeDir string,
	xdgDownloadDir string,
	xdgConfigHome string,
	readFile func(path string) ([]byte, error),
) (string, error) {
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		return "", fmt.Errorf("user home dir is empty")
	}

	if resolved, ok := normalizeUserDirsPath(xdgDownloadDir, homeDir); ok {
		return resolved, nil
	}

	if readFile == nil {
		readFile = os.ReadFile
	}

	configHome := strings.TrimSpace(xdgConfigHome)
	if configHome == "" {
		configHome = filepath.Join(homeDir, ".config")
	}
	configPath := filepath.Join(configHome, "user-dirs.dirs")
	if data, err := readFile(configPath); err == nil {
		if resolved, ok := parseUserDirsDownloadDir(string(data), homeDir); ok {
			return resolved, nil
		}
	}

	return filepath.Join(homeDir, "Downloads"), nil
}

func parseUserDirsDownloadDir(contents string, homeDir string) (string, bool) {
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "XDG_DOWNLOAD_DIR=") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "XDG_DOWNLOAD_DIR="))
		return normalizeUserDirsPath(value, homeDir)
	}
	return "", false
}

func normalizeUserDirsPath(value string, homeDir string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}

	value = strings.Trim(value, `"'`)
	if value == "" {
		return "", false
	}

	value = strings.ReplaceAll(value, "${HOME}", homeDir)
	value = strings.ReplaceAll(value, "$HOME", homeDir)
	if strings.HasPrefix(value, "~") {
		value = filepath.Join(homeDir, strings.TrimPrefix(value, "~"))
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(homeDir, value)
	}
	value = filepath.Clean(value)
	if value == "" {
		return "", false
	}
	return value, true
}
