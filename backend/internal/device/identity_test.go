package device

import (
	"path/filepath"
	"testing"
)

func TestEnsureLocalDeviceGeneratesDeviceNameAndKeys(t *testing.T) {
	identityPath := filepath.Join(t.TempDir(), "local-device.json")

	device, err := EnsureLocalDevice(identityPath, "office-device")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if device.DeviceID == "" || device.DeviceName != "office-device" {
		t.Fatalf("unexpected device: %+v", device)
	}
	if device.PublicKeyPEM == "" {
		t.Fatal("expected public key pem")
	}
}

func TestEnsureLocalDeviceReusesPersistedIdentity(t *testing.T) {
	identityPath := filepath.Join(t.TempDir(), "local-device.json")

	first, err := EnsureLocalDevice(identityPath, "office-device")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second, err := EnsureLocalDevice(identityPath, "office-device")
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

func TestEnsureLocalDevicePreservesPersistedDeviceName(t *testing.T) {
	identityPath := filepath.Join(t.TempDir(), "local-device.json")

	device, err := EnsureLocalDevice(identityPath, "default-name")
	if err != nil {
		t.Fatalf("unexpected ensure error: %v", err)
	}

	device.DeviceName = "custom-name"
	if err := persistLocalDevice(identityPath, device); err != nil {
		t.Fatalf("persist custom name: %v", err)
	}

	second, err := EnsureLocalDevice(identityPath, "default-name")
	if err != nil {
		t.Fatalf("unexpected second ensure error: %v", err)
	}

	if second.DeviceID != device.DeviceID {
		t.Fatalf("expected same device id, got %s and %s", device.DeviceID, second.DeviceID)
	}
	if second.DeviceName != "custom-name" {
		t.Fatalf("expected persisted custom device name, got %s", second.DeviceName)
	}

	stored, err := readLocalDevice(identityPath)
	if err != nil {
		t.Fatalf("read stored device: %v", err)
	}
	if stored.DeviceName != "custom-name" {
		t.Fatalf("expected stored custom name, got %s", stored.DeviceName)
	}
}

func TestEnsureLocalDeviceBackfillsMissingPersistedDeviceName(t *testing.T) {
	identityPath := filepath.Join(t.TempDir(), "local-device.json")

	device, err := EnsureLocalDevice(identityPath, "default-name")
	if err != nil {
		t.Fatalf("unexpected ensure error: %v", err)
	}

	device.DeviceName = ""
	if err := persistLocalDevice(identityPath, device); err != nil {
		t.Fatalf("persist blank name: %v", err)
	}

	second, err := EnsureLocalDevice(identityPath, "fallback-name")
	if err != nil {
		t.Fatalf("unexpected second ensure error: %v", err)
	}

	if second.DeviceName != "fallback-name" {
		t.Fatalf("expected backfilled device name, got %s", second.DeviceName)
	}

	stored, err := readLocalDevice(identityPath)
	if err != nil {
		t.Fatalf("read stored device: %v", err)
	}
	if stored.DeviceName != "fallback-name" {
		t.Fatalf("expected stored backfilled name, got %s", stored.DeviceName)
	}
}
