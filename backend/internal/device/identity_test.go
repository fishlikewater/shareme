package device

import (
	"path/filepath"
	"testing"

	"message-share/backend/internal/config"
)

func TestEnsureLocalDeviceGeneratesDeviceNameAndKeys(t *testing.T) {
	cfg := config.Default()

	dev, err := EnsureLocalDevice(cfg.DataDir, "办公室电脑")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if dev.DeviceID == "" || dev.DeviceName != "办公室电脑" {
		t.Fatalf("unexpected device: %+v", dev)
	}

	if dev.PublicKeyPEM == "" {
		t.Fatal("expected public key pem")
	}
}

func TestEnsureLocalDeviceReusesPersistedIdentity(t *testing.T) {
	baseDir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = baseDir
	cfg.IdentityFilePath = filepath.Join(baseDir, "local-device.json")

	first, err := EnsureLocalDevice(cfg.IdentityFilePath, "办公室电脑")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second, err := EnsureLocalDevice(cfg.IdentityFilePath, "办公室电脑")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first.DeviceID != second.DeviceID {
		t.Fatalf("expected stable device id, got %s and %s", first.DeviceID, second.DeviceID)
	}
	if first.PublicKeyPEM != second.PublicKeyPEM {
		t.Fatal("expected stable public key pem")
	}
}
