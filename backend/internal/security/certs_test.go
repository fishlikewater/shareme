package security

import (
	"path/filepath"
	"testing"

	"shareme/backend/internal/device"
)

func TestBuildPinnedPeerUsesStableFingerprint(t *testing.T) {
	peerA := BuildPinnedPeer("peer-1", "public-key")
	peerB := BuildPinnedPeer("peer-1", "public-key")

	if peerA.Fingerprint == "" {
		t.Fatal("expected fingerprint")
	}
	if peerA.Fingerprint != peerB.Fingerprint {
		t.Fatalf("expected stable fingerprint, got %s and %s", peerA.Fingerprint, peerB.Fingerprint)
	}
}

func TestBuildTLSCertificateUsesSamePublicKeyFingerprint(t *testing.T) {
	localDevice, err := device.EnsureLocalDevice(filepath.Join(t.TempDir(), "local-device.json"), "my-pc")
	if err != nil {
		t.Fatalf("unexpected local device error: %v", err)
	}

	certificate, err := BuildTLSCertificate(localDevice)
	if err != nil {
		t.Fatalf("unexpected build certificate error: %v", err)
	}
	if len(certificate.Certificate) == 0 {
		t.Fatal("expected leaf certificate")
	}

	fingerprint, err := FingerprintLeafDER(certificate.Certificate[0])
	if err != nil {
		t.Fatalf("unexpected fingerprint error: %v", err)
	}

	expected := BuildPinnedPeer(localDevice.DeviceID, localDevice.PublicKeyPEM).Fingerprint
	if fingerprint != expected {
		t.Fatalf("expected same fingerprint, got %s and %s", fingerprint, expected)
	}
}
