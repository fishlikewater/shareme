package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type AppConfig struct {
	LocalAPIAddr           string
	AgentTCPPort           int
	DiscoveryUDPPort       int
	DiscoveryListenAddr    string
	DiscoveryBroadcastAddr string
	DataDir                string
	DeviceName             string
	IdentityFilePath       string
	DefaultDownloadDir     string
	MaxAutoAcceptFileMB    int64
}

func Default() AppConfig {
	cfg := AppConfig{
		LocalAPIAddr:        "127.0.0.1:19100",
		AgentTCPPort:        19090,
		DiscoveryUDPPort:    19091,
		DataDir:             resolveDefaultDataDir(),
		DeviceName:          "本机设备",
		MaxAutoAcceptFileMB: 512,
	}

	if value := strings.TrimSpace(os.Getenv("MESSAGE_SHARE_LOCAL_API_ADDR")); value != "" {
		cfg.LocalAPIAddr = value
	}
	if value, ok := lookupEnvInt("MESSAGE_SHARE_AGENT_TCP_PORT"); ok {
		cfg.AgentTCPPort = value
	}
	if value, ok := lookupEnvInt("MESSAGE_SHARE_DISCOVERY_UDP_PORT"); ok {
		cfg.DiscoveryUDPPort = value
	}
	if value := strings.TrimSpace(os.Getenv("MESSAGE_SHARE_DATA_DIR")); value != "" {
		cfg.DataDir = value
	}
	if value := strings.TrimSpace(os.Getenv("MESSAGE_SHARE_DEVICE_NAME")); value != "" {
		cfg.DeviceName = value
	}

	cfg.DiscoveryListenAddr = ":" + strconv.Itoa(cfg.DiscoveryUDPPort)
	cfg.DiscoveryBroadcastAddr = "255.255.255.255:" + strconv.Itoa(cfg.DiscoveryUDPPort)
	if value := strings.TrimSpace(os.Getenv("MESSAGE_SHARE_DISCOVERY_LISTEN_ADDR")); value != "" {
		cfg.DiscoveryListenAddr = value
	}
	if value := strings.TrimSpace(os.Getenv("MESSAGE_SHARE_DISCOVERY_BROADCAST_ADDR")); value != "" {
		cfg.DiscoveryBroadcastAddr = value
	}

	cfg.IdentityFilePath = filepath.Join(cfg.DataDir, "local-device.json")
	cfg.DefaultDownloadDir = filepath.Join(cfg.DataDir, "downloads")

	if value := strings.TrimSpace(os.Getenv("MESSAGE_SHARE_IDENTITY_FILE")); value != "" {
		cfg.IdentityFilePath = value
	}
	if value := strings.TrimSpace(os.Getenv("MESSAGE_SHARE_DOWNLOAD_DIR")); value != "" {
		cfg.DefaultDownloadDir = value
	}

	return cfg
}

func resolveDefaultDataDir() string {
	for _, candidate := range defaultDataDirCandidates() {
		if candidate == "" {
			continue
		}
		if err := os.MkdirAll(candidate, 0o755); err == nil {
			return candidate
		}
	}

	return filepath.Join(".", "MessageShare")
}

func defaultDataDirCandidates() []string {
	candidates := make([]string, 0, 3)
	seen := make(map[string]struct{})

	addCandidate := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		candidates = append(candidates, value)
	}

	if baseDir, err := os.UserConfigDir(); err == nil {
		addCandidate(filepath.Join(baseDir, "MessageShare"))
	}
	if homeDir, err := os.UserHomeDir(); err == nil {
		addCandidate(filepath.Join(homeDir, "MessageShare"))
	}
	addCandidate(filepath.Join(os.TempDir(), "MessageShare"))

	return candidates
}

func lookupEnvInt(key string) (int, bool) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return 0, false
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return parsed, true
}
