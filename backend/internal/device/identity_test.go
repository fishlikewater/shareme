package device

import (
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
