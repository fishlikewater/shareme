package config

import (
	"os"
	"path/filepath"
)

type AppConfig struct {
	LocalAPIAddr        string
	AgentTCPPort        int
	DiscoveryUDPPort    int
	DataDir             string
	DeviceName          string
	IdentityFilePath    string
	DefaultDownloadDir  string
	MaxAutoAcceptFileMB int64
}

func Default() AppConfig {
	baseDir, err := os.UserConfigDir()
	if err != nil {
		baseDir = "."
	}

	dataDir := filepath.Join(baseDir, "MessageShare")
	downloadDir := filepath.Join(dataDir, "downloads")
	identityPath := filepath.Join(dataDir, "local-device.json")

	return AppConfig{
		LocalAPIAddr:        "127.0.0.1:19100",
		AgentTCPPort:        19090,
		DiscoveryUDPPort:    19091,
		DataDir:             dataDir,
		DeviceName:          "我的电脑",
		IdentityFilePath:    identityPath,
		DefaultDownloadDir:  downloadDir,
		MaxAutoAcceptFileMB: 512,
	}
}
