package config

import "testing"

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
