//go:build !windows

package config

import "fmt"

func resolvePlatformDownloadDir() (string, error) {
	return "", fmt.Errorf("system downloads dir is unsupported on this platform")
}
