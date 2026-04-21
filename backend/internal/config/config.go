package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type AppConfig struct {
	AgentTCPPort           int
	LocalHTTPPort          int
	AcceleratedDataPort    int
	AcceleratedEnabled     bool
	DiscoveryUDPPort       int
	DiscoveryListenAddr    string
	DiscoveryBroadcastAddr string
	DataDir                string
	DatabasePath           string
	LogDir                 string
	TempDir                string
	DeviceName             string
	IdentityFilePath       string
	DefaultDownloadDir     string
	MaxAutoAcceptFileMB    int64
}

func LoadDefault() (AppConfig, error) {
	defaultLayout, err := ResolveDefaultLayout()
	if err != nil {
		return AppConfig{}, err
	}
	dataDir := defaultLayout.RootDir
	if value := strings.TrimSpace(os.Getenv("MESSAGE_SHARE_DATA_DIR")); value != "" {
		if err := ensureDownloadDirUsable(value); err == nil {
			dataDir = value
		}
	}
	if filepath.Clean(dataDir) == filepath.Clean(defaultLayout.RootDir) {
		if err := MigrateLegacyData(MigrationOptions{
			LegacyDirs: LegacyDataDirCandidates(),
			NewRootDir: defaultLayout.RootDir,
		}); err != nil {
			return AppConfig{}, err
		}
	}

	layout := ResolveLayout(dataDir)
	settings, err := LoadSettings(layout.RootDir)
	if err != nil {
		return AppConfig{}, err
	}

	cfg := AppConfig{
		AgentTCPPort:        19090,
		LocalHTTPPort:       52350,
		AcceleratedDataPort: 19092,
		AcceleratedEnabled:  true,
		DiscoveryUDPPort:    19091,
		DataDir:             layout.RootDir,
		DatabasePath:        layout.DatabasePath,
		LogDir:              layout.LogDir,
		TempDir:             layout.TempDir,
		DeviceName:          settings.DeviceName,
		IdentityFilePath:    layout.IdentityFilePath,
		MaxAutoAcceptFileMB: settings.MaxAutoAcceptFileMB,
	}

	if value, ok := lookupEnvInt("MESSAGE_SHARE_AGENT_TCP_PORT"); ok {
		cfg.AgentTCPPort = value
	}
	if value, ok := lookupEnvInt("MESSAGE_SHARE_LOCAL_HTTP_PORT"); ok {
		cfg.LocalHTTPPort = value
	}
	if value, ok := lookupEnvInt("MESSAGE_SHARE_ACCELERATED_DATA_PORT"); ok {
		cfg.AcceleratedDataPort = value
	}
	if value, ok := lookupEnvBool("MESSAGE_SHARE_ACCELERATED_ENABLED"); ok {
		cfg.AcceleratedEnabled = value
	}
	if value, ok := lookupEnvInt("MESSAGE_SHARE_DISCOVERY_UDP_PORT"); ok {
		cfg.DiscoveryUDPPort = value
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

	cfg.DefaultDownloadDir = resolveConfiguredDownloadDir(settings.DownloadDir, cfg.DataDir)

	if value := strings.TrimSpace(os.Getenv("MESSAGE_SHARE_IDENTITY_FILE")); value != "" {
		cfg.IdentityFilePath = value
	}

	return cfg, nil
}

func Default() AppConfig {
	cfg, err := LoadDefault()
	if err != nil {
		panic(err)
	}
	return cfg
}

func resolveDefaultDataDir() string {
	dataDir, err := loadDefaultDataDir()
	if err != nil {
		panic(err)
	}
	return dataDir
}

func loadDefaultDataDir() (string, error) {
	layout, err := ResolveDefaultLayout()
	if err != nil {
		return "", err
	}
	return layout.RootDir, nil
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

func lookupEnvBool(key string) (bool, bool) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return false, false
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, false
	}
	return parsed, true
}
