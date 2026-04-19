package device

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"message-share/backend/internal/domain"
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

func TestEnsureLocalDeviceSyncsConfiguredDeviceNameToPersistedIdentity(t *testing.T) {
	identityPath := filepath.Join(t.TempDir(), "local-device.json")

	device, err := EnsureLocalDevice(identityPath, "default-name")
	if err != nil {
		t.Fatalf("unexpected ensure error: %v", err)
	}

	device.DeviceName = "custom-name"
	if err := persistLocalDevice(identityPath, device); err != nil {
		t.Fatalf("persist custom name: %v", err)
	}

	second, err := EnsureLocalDevice(identityPath, "configured-name")
	if err != nil {
		t.Fatalf("unexpected second ensure error: %v", err)
	}

	if second.DeviceID != device.DeviceID {
		t.Fatalf("expected same device id, got %s and %s", device.DeviceID, second.DeviceID)
	}
	if second.DeviceName != "configured-name" {
		t.Fatalf("expected configured device name to be synced, got %s", second.DeviceName)
	}

	stored, err := readLocalDevice(identityPath)
	if err != nil {
		t.Fatalf("read stored device: %v", err)
	}
	if stored.DeviceName != "configured-name" {
		t.Fatalf("expected stored device name to be updated, got %s", stored.DeviceName)
	}
}

func TestEnsureLocalDeviceReturnsErrorWhenPersistedIdentityMissingDeviceName(t *testing.T) {
	identityPath := filepath.Join(t.TempDir(), "local-device.json")

	device, err := EnsureLocalDevice(identityPath, "default-name")
	if err != nil {
		t.Fatalf("unexpected ensure error: %v", err)
	}

	device.DeviceName = ""
	if err := persistLocalDevice(identityPath, device); err != nil {
		t.Fatalf("persist blank name: %v", err)
	}

	_, err = EnsureLocalDevice(identityPath, "fallback-name")
	if err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "device") {
			t.Fatalf("expected invalid device-name error, got %v", err)
		}
		return
	}
	t.Fatal("expected missing device name to return error")
}

func TestEnsureLocalDeviceReturnsErrorWhenIdentityFileIsInvalid(t *testing.T) {
	identityPath := filepath.Join(t.TempDir(), "local-device.json")
	if err := os.WriteFile(identityPath, []byte("{invalid-json"), 0o600); err != nil {
		t.Fatalf("write invalid identity file: %v", err)
	}

	_, err := EnsureLocalDevice(identityPath, "office-device")
	if err == nil {
		t.Fatal("expected invalid identity file to return error")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("expected invalid identity error, got %v", err)
	}
}

func TestEnsureLocalDeviceReturnsErrorWhenIdentityStructureIncomplete(t *testing.T) {
	identityPath := filepath.Join(t.TempDir(), "local-device.json")

	first, err := EnsureLocalDevice(identityPath, "office-device")
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*testing.T, *domain.LocalDevice)
	}{
		{
			name: "missing device id",
			mutate: func(t *testing.T, device *domain.LocalDevice) {
				device.DeviceID = ""
			},
		},
		{
			name: "missing public key pem",
			mutate: func(t *testing.T, device *domain.LocalDevice) {
				device.PublicKeyPEM = ""
			},
		},
		{
			name: "missing private key pem",
			mutate: func(t *testing.T, device *domain.LocalDevice) {
				device.PrivateKeyPEM = ""
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			device := first
			tc.mutate(t, &device)
			if err := persistLocalDevice(identityPath, device); err != nil {
				t.Fatalf("persist invalid identity: %v", err)
			}

			_, err := EnsureLocalDevice(identityPath, "office-device")
			if err == nil {
				t.Fatal("expected incomplete identity to return error")
			}
		})
	}
}
