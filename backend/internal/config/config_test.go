package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDefaultDownloadDirPrefersSystemDownloadsWhenUsable(t *testing.T) {
	originalResolver := systemDownloadDirResolver
	originalMkdirAll := downloadDirMkdirAll
	originalCreateTemp := downloadDirCreateTemp
	t.Cleanup(func() {
		systemDownloadDirResolver = originalResolver
		downloadDirMkdirAll = originalMkdirAll
		downloadDirCreateTemp = originalCreateTemp
	})

	systemDir := filepath.Join(t.TempDir(), "Downloads")
	systemDownloadDirResolver = func() (string, error) {
		return systemDir, nil
	}
	downloadDirMkdirAll = os.MkdirAll
	downloadDirCreateTemp = os.CreateTemp

	resolved, err := resolveDefaultDownloadDir(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("expected system downloads dir to resolve, got error: %v", err)
	}
	if resolved != systemDir {
		t.Fatalf("expected system downloads dir %s, got %s", systemDir, resolved)
	}
	if info, err := os.Stat(systemDir); err != nil {
		t.Fatalf("expected system downloads dir to be prepared: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("expected system downloads path to be a directory, got file")
	}
}

func TestResolveDefaultDownloadDirFallsBackWhenSystemDownloadsUnavailable(t *testing.T) {
	originalResolver := systemDownloadDirResolver
	originalMkdirAll := downloadDirMkdirAll
	originalCreateTemp := downloadDirCreateTemp
	t.Cleanup(func() {
		systemDownloadDirResolver = originalResolver
		downloadDirMkdirAll = originalMkdirAll
		downloadDirCreateTemp = originalCreateTemp
	})

	blockedSystemPath := filepath.Join(t.TempDir(), "blocked-downloads")
	if err := os.WriteFile(blockedSystemPath, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("expected blocked system path file to be created: %v", err)
	}
	systemDownloadDirResolver = func() (string, error) {
		return blockedSystemPath, nil
	}
	downloadDirMkdirAll = os.MkdirAll
	downloadDirCreateTemp = os.CreateTemp

	dataDir := filepath.Join(t.TempDir(), "data")
	resolved, err := resolveDefaultDownloadDir(dataDir)
	if err != nil {
		t.Fatalf("expected fallback downloads dir to resolve, got error: %v", err)
	}
	expected := filepath.Join(dataDir, "downloads")
	if resolved != expected {
		t.Fatalf("expected fallback downloads dir %s, got %s", expected, resolved)
	}
	if info, err := os.Stat(expected); err != nil {
		t.Fatalf("expected fallback downloads dir to be prepared: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("expected fallback downloads path to be a directory, got file")
	}
}

func TestResolveDefaultDownloadDirReturnsErrorWhenFallbackUnavailable(t *testing.T) {
	originalResolver := systemDownloadDirResolver
	originalMkdirAll := downloadDirMkdirAll
	originalCreateTemp := downloadDirCreateTemp
	t.Cleanup(func() {
		systemDownloadDirResolver = originalResolver
		downloadDirMkdirAll = originalMkdirAll
		downloadDirCreateTemp = originalCreateTemp
	})

	systemDownloadDirResolver = func() (string, error) {
		return "", errors.New("system downloads unavailable")
	}
	downloadDirMkdirAll = os.MkdirAll
	downloadDirCreateTemp = os.CreateTemp

	blockedDataDir := filepath.Join(t.TempDir(), "blocked-data-dir")
	if err := os.WriteFile(blockedDataDir, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("expected blocked data dir file to be created: %v", err)
	}

	resolved, err := resolveDefaultDownloadDir(blockedDataDir)
	if err == nil {
		t.Fatal("expected fallback resolution to fail")
	}
	if resolved != "" {
		t.Fatalf("expected empty download dir on resolution failure, got %s", resolved)
	}
	if !strings.Contains(err.Error(), "fallback") {
		t.Fatalf("expected fallback error context, got %v", err)
	}
}

func TestResolveGuaranteedDownloadDirUsesTemporaryFallbackWhenCandidatesUnavailable(t *testing.T) {
	originalResolver := systemDownloadDirResolver
	originalMkdirAll := downloadDirMkdirAll
	originalCreateTemp := downloadDirCreateTemp
	originalMkdirTemp := downloadDirMkdirTemp
	t.Cleanup(func() {
		systemDownloadDirResolver = originalResolver
		downloadDirMkdirAll = originalMkdirAll
		downloadDirCreateTemp = originalCreateTemp
		downloadDirMkdirTemp = originalMkdirTemp
	})

	systemDownloadDirResolver = func() (string, error) {
		return "", errors.New("system downloads unavailable")
	}

	tempFallback := filepath.Join(t.TempDir(), "guaranteed-downloads")
	downloadDirMkdirTemp = func(dir string, pattern string) (string, error) {
		if err := os.MkdirAll(tempFallback, 0o755); err != nil {
			return "", err
		}
		return tempFallback, nil
	}
	downloadDirMkdirAll = func(path string, perm os.FileMode) error {
		if filepath.Clean(path) == filepath.Clean(tempFallback) {
			return os.MkdirAll(path, perm)
		}
		return errors.New("blocked")
	}
	downloadDirCreateTemp = func(dir string, pattern string) (*os.File, error) {
		if filepath.Clean(dir) == filepath.Clean(tempFallback) {
			return os.CreateTemp(dir, pattern)
		}
		return nil, errors.New("blocked")
	}

	resolved := resolveGuaranteedDownloadDir(filepath.Join(t.TempDir(), "blocked-data"))
	if filepath.Clean(resolved) != filepath.Clean(tempFallback) {
		t.Fatalf("expected temporary fallback %s, got %s", tempFallback, resolved)
	}
	if info, err := os.Stat(resolved); err != nil {
		t.Fatalf("expected temporary fallback dir to be prepared: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("expected temporary fallback path to be a directory, got file")
	}
}

func TestDefaultConfigSkipsDownloadDirResolutionWhenEnvironmentOverrideExists(t *testing.T) {
	originalResolver := systemDownloadDirResolver
	t.Cleanup(func() {
		systemDownloadDirResolver = originalResolver
	})

	resolverCalls := 0
	overrideDir := filepath.Join(t.TempDir(), "explicit-downloads")
	systemDownloadDirResolver = func() (string, error) {
		resolverCalls++
		return filepath.Join(t.TempDir(), "system-downloads"), nil
	}

	t.Setenv("MESSAGE_SHARE_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("MESSAGE_SHARE_DOWNLOAD_DIR", overrideDir)

	cfg := Default()
	if cfg.DefaultDownloadDir != overrideDir {
		t.Fatalf("expected explicit download dir %s, got %s", overrideDir, cfg.DefaultDownloadDir)
	}
	if resolverCalls != 0 {
		t.Fatalf("expected explicit override to skip default download dir resolution, got %d resolver calls", resolverCalls)
	}
	if info, err := os.Stat(overrideDir); err != nil {
		t.Fatalf("expected explicit download dir to be prepared: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("expected explicit download path to be a directory, got file")
	}
}

func TestDefaultConfigFallsBackToSystemDownloadsWhenExplicitDownloadDirInvalid(t *testing.T) {
	originalResolver := systemDownloadDirResolver
	t.Cleanup(func() {
		systemDownloadDirResolver = originalResolver
	})

	resolverCalls := 0
	systemDir := filepath.Join(t.TempDir(), "system-downloads")
	systemDownloadDirResolver = func() (string, error) {
		resolverCalls++
		return systemDir, nil
	}

	blockedOverride := filepath.Join(t.TempDir(), "blocked-download-dir")
	if err := os.WriteFile(blockedOverride, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("expected blocked explicit download dir file to be created: %v", err)
	}

	t.Setenv("MESSAGE_SHARE_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("MESSAGE_SHARE_DOWNLOAD_DIR", blockedOverride)

	cfg := Default()
	if cfg.DefaultDownloadDir != systemDir {
		t.Fatalf("expected fallback to system downloads %s, got %s", systemDir, cfg.DefaultDownloadDir)
	}
	if resolverCalls != 1 {
		t.Fatalf("expected invalid explicit download dir to probe system downloads once, got %d", resolverCalls)
	}
}

func TestDefaultConfigValidatesExplicitDownloadDirWithoutProbingLowerPriorityDirs(t *testing.T) {
	originalResolver := systemDownloadDirResolver
	t.Cleanup(func() {
		systemDownloadDirResolver = originalResolver
	})

	resolverCalls := 0
	systemDownloadDirResolver = func() (string, error) {
		resolverCalls++
		return filepath.Join(t.TempDir(), "system-downloads"), nil
	}

	explicitDir := filepath.Join(t.TempDir(), "explicit-download-dir")
	t.Setenv("MESSAGE_SHARE_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("MESSAGE_SHARE_DOWNLOAD_DIR", explicitDir)

	cfg := Default()
	if cfg.DefaultDownloadDir != explicitDir {
		t.Fatalf("expected valid explicit download dir %s, got %s", explicitDir, cfg.DefaultDownloadDir)
	}
	if resolverCalls != 0 {
		t.Fatalf("expected valid explicit override to skip lower-priority probes, got %d resolver calls", resolverCalls)
	}
}

func TestDefaultConfigFallsBackToDataDirDownloadsWhenExplicitAndSystemDownloadsInvalid(t *testing.T) {
	originalResolver := systemDownloadDirResolver
	t.Cleanup(func() {
		systemDownloadDirResolver = originalResolver
	})

	resolverCalls := 0
	systemDownloadDirResolver = func() (string, error) {
		resolverCalls++
		return "", errors.New("system downloads unavailable")
	}

	blockedOverride := filepath.Join(t.TempDir(), "blocked-download-dir")
	if err := os.WriteFile(blockedOverride, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("expected blocked explicit download dir file to be created: %v", err)
	}

	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv("MESSAGE_SHARE_DATA_DIR", dataDir)
	t.Setenv("MESSAGE_SHARE_DOWNLOAD_DIR", blockedOverride)

	cfg := Default()
	expected := filepath.Join(dataDir, "downloads")
	if cfg.DefaultDownloadDir != expected {
		t.Fatalf("expected fallback to data dir downloads %s, got %s", expected, cfg.DefaultDownloadDir)
	}
	if resolverCalls != 1 {
		t.Fatalf("expected invalid explicit download dir to probe system downloads once, got %d", resolverCalls)
	}
}

func TestDefaultConfigFallsBackToResolvedDataDirWhenConfiguredDataDirInvalid(t *testing.T) {
	originalResolver := systemDownloadDirResolver
	t.Cleanup(func() {
		systemDownloadDirResolver = originalResolver
	})

	systemDownloadDirResolver = func() (string, error) {
		return "", errors.New("system downloads unavailable")
	}

	blockedDataDir := filepath.Join(t.TempDir(), "blocked-data-dir")
	if err := os.WriteFile(blockedDataDir, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("expected blocked data dir file to be created: %v", err)
	}

	fallbackHome := filepath.Join(t.TempDir(), "fallback-home")
	if err := os.MkdirAll(fallbackHome, 0o755); err != nil {
		t.Fatalf("expected fallback home directory to be created: %v", err)
	}

	blockedConfigBase := filepath.Join(t.TempDir(), "blocked-config-base")
	if err := os.WriteFile(blockedConfigBase, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("expected blocked config base file to be created: %v", err)
	}

	t.Setenv("APPDATA", blockedConfigBase)
	t.Setenv("XDG_CONFIG_HOME", blockedConfigBase)
	t.Setenv("USERPROFILE", fallbackHome)
	t.Setenv("HOME", fallbackHome)
	t.Setenv("MESSAGE_SHARE_DATA_DIR", blockedDataDir)

	cfg := Default()
	expectedDataDir := filepath.Join(fallbackHome, ".message-share")
	expectedDownloadDir := filepath.Join(expectedDataDir, "downloads")
	if cfg.DataDir != expectedDataDir {
		t.Fatalf("expected invalid configured data dir to fall back to %s, got %s", expectedDataDir, cfg.DataDir)
	}
	if cfg.DefaultDownloadDir != expectedDownloadDir {
		t.Fatalf("expected download dir to use resolved data dir fallback %s, got %s", expectedDownloadDir, cfg.DefaultDownloadDir)
	}
}

func TestDefaultConfigUsesFixedPorts(t *testing.T) {
	cfg := Default()
	if cfg.AgentTCPPort != 19090 {
		t.Fatalf("expected tcp port 19090, got %d", cfg.AgentTCPPort)
	}
	if cfg.LocalHTTPPort != 52350 {
		t.Fatalf("expected local http port 52350, got %d", cfg.LocalHTTPPort)
	}
	if cfg.AcceleratedDataPort != 19092 {
		t.Fatalf("expected accelerated data port 19092, got %d", cfg.AcceleratedDataPort)
	}
	if !cfg.AcceleratedEnabled {
		t.Fatal("expected accelerated transfer to be enabled by default")
	}
	if cfg.DiscoveryUDPPort != 19091 {
		t.Fatalf("expected discovery port 19091, got %d", cfg.DiscoveryUDPPort)
	}
	if cfg.DeviceName == "" {
		t.Fatal("expected default device name")
	}
	if cfg.IdentityFilePath == "" {
		t.Fatal("expected identity file path")
	}
}

func TestDefaultConfigAllowsEnvironmentOverrides(t *testing.T) {
	t.Setenv("MESSAGE_SHARE_AGENT_TCP_PORT", "52351")
	t.Setenv("MESSAGE_SHARE_LOCAL_HTTP_PORT", "52354")
	t.Setenv("MESSAGE_SHARE_ACCELERATED_DATA_PORT", "52353")
	t.Setenv("MESSAGE_SHARE_ACCELERATED_ENABLED", "false")
	t.Setenv("MESSAGE_SHARE_DISCOVERY_UDP_PORT", "52352")
	t.Setenv("MESSAGE_SHARE_DISCOVERY_LISTEN_ADDR", "127.0.0.1:52352")
	t.Setenv("MESSAGE_SHARE_DISCOVERY_BROADCAST_ADDR", "127.0.0.1:52362")
	t.Setenv("MESSAGE_SHARE_DATA_DIR", t.TempDir())
	t.Setenv("MESSAGE_SHARE_DEVICE_NAME", "客厅电脑")

	cfg := Default()
	if cfg.AgentTCPPort != 52351 {
		t.Fatalf("expected overridden tcp port, got %d", cfg.AgentTCPPort)
	}
	if cfg.LocalHTTPPort != 52354 {
		t.Fatalf("expected overridden local http port, got %d", cfg.LocalHTTPPort)
	}
	if cfg.AcceleratedDataPort != 52353 {
		t.Fatalf("expected overridden accelerated data port, got %d", cfg.AcceleratedDataPort)
	}
	if cfg.AcceleratedEnabled {
		t.Fatal("expected accelerated transfer to be disabled by override")
	}
	if cfg.DiscoveryUDPPort != 52352 {
		t.Fatalf("expected overridden discovery port, got %d", cfg.DiscoveryUDPPort)
	}
	if cfg.DiscoveryListenAddr != "127.0.0.1:52352" {
		t.Fatalf("expected overridden discovery listen addr, got %s", cfg.DiscoveryListenAddr)
	}
	if cfg.DiscoveryBroadcastAddr != "127.0.0.1:52362" {
		t.Fatalf("expected overridden discovery broadcast addr, got %s", cfg.DiscoveryBroadcastAddr)
	}
	if cfg.DataDir == "" {
		t.Fatal("expected data dir")
	}
	if cfg.DeviceName != "客厅电脑" {
		t.Fatalf("expected overridden device name, got %s", cfg.DeviceName)
	}
	if cfg.IdentityFilePath == "" || cfg.DefaultDownloadDir == "" {
		t.Fatalf("expected derived paths, got %#v", cfg)
	}
}

func TestDefaultConfigUsesMessageShareRootLayout(t *testing.T) {
	originalResolver := systemDownloadDirResolver
	t.Cleanup(func() {
		systemDownloadDirResolver = originalResolver
	})

	systemDownloadDirResolver = func() (string, error) {
		return "", errors.New("system downloads unavailable")
	}

	homeDir := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("expected home directory to be created: %v", err)
	}

	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("HOME", homeDir)

	cfg := Default()
	expectedDataDir := filepath.Join(homeDir, ".message-share")
	if cfg.DataDir != expectedDataDir {
		t.Fatalf("expected default data dir %s, got %s", expectedDataDir, cfg.DataDir)
	}
	if cfg.IdentityFilePath != filepath.Join(expectedDataDir, "local-device.json") {
		t.Fatalf("expected identity path %s, got %s", filepath.Join(expectedDataDir, "local-device.json"), cfg.IdentityFilePath)
	}
	if cfg.DatabasePath != filepath.Join(expectedDataDir, "message-share.db") {
		t.Fatalf("expected database path %s, got %s", filepath.Join(expectedDataDir, "message-share.db"), cfg.DatabasePath)
	}
	if cfg.LogDir != filepath.Join(expectedDataDir, "logs") {
		t.Fatalf("expected log dir %s, got %s", filepath.Join(expectedDataDir, "logs"), cfg.LogDir)
	}
	if cfg.TempDir != filepath.Join(expectedDataDir, "tmp") {
		t.Fatalf("expected temp dir %s, got %s", filepath.Join(expectedDataDir, "tmp"), cfg.TempDir)
	}
	if cfg.DefaultDownloadDir != filepath.Join(expectedDataDir, "downloads") {
		t.Fatalf("expected fallback download dir %s, got %s", filepath.Join(expectedDataDir, "downloads"), cfg.DefaultDownloadDir)
	}
	if _, err := os.Stat(filepath.Join(expectedDataDir, "config.json")); err != nil {
		t.Fatalf("expected config file to be created: %v", err)
	}
}

func TestLoadDefaultReturnsErrorWhenConfigFileInvalid(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "home")
	rootDir := filepath.Join(homeDir, ".message-share")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("expected root dir to be created: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "config.json"), []byte("{invalid-json"), 0o600); err != nil {
		t.Fatalf("expected invalid config to be written: %v", err)
	}

	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("HOME", homeDir)

	_, err := LoadDefault()
	if err == nil {
		t.Fatal("expected invalid config to return error")
	}
	if !strings.Contains(err.Error(), "unmarshal settings file") {
		t.Fatalf("expected settings parse error, got %v", err)
	}
}

func TestLoadDefaultMigratesLegacyDataBeforeReadingNewRoot(t *testing.T) {
	originalResolver := systemDownloadDirResolver
	originalUserConfigDir := legacyUserConfigDir
	originalUserHomeDir := legacyUserHomeDir
	t.Cleanup(func() {
		systemDownloadDirResolver = originalResolver
		legacyUserConfigDir = originalUserConfigDir
		legacyUserHomeDir = originalUserHomeDir
	})

	systemDownloadDirResolver = func() (string, error) {
		return "", errors.New("system downloads unavailable")
	}

	baseDir := t.TempDir()
	homeDir := filepath.Join(baseDir, "home")
	legacyConfigBase := filepath.Join(baseDir, "legacy-config-base")
	legacyRoot := filepath.Join(legacyConfigBase, "MessageShare")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("create home dir: %v", err)
	}
	legacyUserConfigDir = func() (string, error) {
		return legacyConfigBase, nil
	}
	legacyUserHomeDir = func() (string, error) {
		return filepath.Join(baseDir, "unused-home"), nil
	}
	legacy := writeLegacyRuntimeData(t, legacyRoot)

	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("HOME", homeDir)

	cfg, err := LoadDefault()
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}

	newRoot := filepath.Join(homeDir, ".message-share")
	if cfg.DataDir != newRoot {
		t.Fatalf("expected migrated data dir %s, got %s", newRoot, cfg.DataDir)
	}
	if cfg.DeviceName != "legacy-device" {
		t.Fatalf("expected migrated device name, got %s", cfg.DeviceName)
	}
	assertFileContent(t, filepath.Join(newRoot, "config.json"), legacy.configContent)
	assertFileContent(t, filepath.Join(newRoot, "local-device.json"), legacy.identityContent)
	assertFileContent(t, filepath.Join(newRoot, "message-share.db"), legacy.databaseContent)
	assertFileContent(t, filepath.Join(newRoot, migrationMarkerFileName), []byte(legacyRoot))
}
