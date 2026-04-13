package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigUsesLocalhostAndFixedPorts(t *testing.T) {
	cfg := Default()
	if cfg.LocalAPIAddr != "127.0.0.1:19100" {
		t.Fatalf("expected localhost api addr, got %s", cfg.LocalAPIAddr)
	}
	if cfg.AgentTCPPort != 19090 {
		t.Fatalf("expected tcp port 19090, got %d", cfg.AgentTCPPort)
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
	t.Setenv("MESSAGE_SHARE_LOCAL_API_ADDR", "127.0.0.1:52350")
	t.Setenv("MESSAGE_SHARE_AGENT_TCP_PORT", "52351")
	t.Setenv("MESSAGE_SHARE_DISCOVERY_UDP_PORT", "52352")
	t.Setenv("MESSAGE_SHARE_DISCOVERY_LISTEN_ADDR", "127.0.0.1:52352")
	t.Setenv("MESSAGE_SHARE_DISCOVERY_BROADCAST_ADDR", "127.0.0.1:52362")
	t.Setenv("MESSAGE_SHARE_DATA_DIR", t.TempDir())
	t.Setenv("MESSAGE_SHARE_DEVICE_NAME", "客厅电脑")

	cfg := Default()
	if cfg.LocalAPIAddr != "127.0.0.1:52350" {
		t.Fatalf("expected overridden api addr, got %s", cfg.LocalAPIAddr)
	}
	if cfg.AgentTCPPort != 52351 {
		t.Fatalf("expected overridden tcp port, got %d", cfg.AgentTCPPort)
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

func TestDefaultConfigFallsBackWhenUserConfigDirCannotBeCreated(t *testing.T) {
	brokenConfigBase := filepath.Join(t.TempDir(), "broken-config-base")
	if err := os.WriteFile(brokenConfigBase, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("expected broken config base file to be created: %v", err)
	}

	fallbackHome := filepath.Join(t.TempDir(), "fallback-home")
	if err := os.MkdirAll(fallbackHome, 0o755); err != nil {
		t.Fatalf("expected fallback home directory to be created: %v", err)
	}

	t.Setenv("APPDATA", brokenConfigBase)
	t.Setenv("XDG_CONFIG_HOME", brokenConfigBase)
	t.Setenv("USERPROFILE", fallbackHome)
	t.Setenv("HOME", fallbackHome)

	cfg := Default()
	expectedDataDir := filepath.Join(fallbackHome, "MessageShare")
	if cfg.DataDir != expectedDataDir {
		t.Fatalf("expected fallback data dir %s, got %s", expectedDataDir, cfg.DataDir)
	}
	if !strings.HasPrefix(cfg.IdentityFilePath, expectedDataDir) {
		t.Fatalf("expected identity path to use fallback data dir, got %s", cfg.IdentityFilePath)
	}
	if !strings.HasPrefix(cfg.DefaultDownloadDir, expectedDataDir) {
		t.Fatalf("expected download dir to use fallback data dir, got %s", cfg.DefaultDownloadDir)
	}
}
