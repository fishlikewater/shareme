//go:build !windows

package config

import "os"

func resolvePlatformDownloadDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return resolveUnixDownloadDir(
		homeDir,
		os.Getenv("XDG_DOWNLOAD_DIR"),
		os.Getenv("XDG_CONFIG_HOME"),
		os.ReadFile,
	)
}
