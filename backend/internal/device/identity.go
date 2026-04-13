package device

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"time"

	"message-share/backend/internal/domain"
)

func EnsureLocalDevice(identityFilePath string, name string) (domain.LocalDevice, error) {
	if existing, err := readLocalDevice(identityFilePath); err == nil {
		if existing.DeviceName == "" && name != "" {
			existing.DeviceName = name
			if err := persistLocalDevice(identityFilePath, existing); err != nil {
				return domain.LocalDevice{}, err
			}
		}
		return existing, nil
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return domain.LocalDevice{}, err
	}

	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return domain.LocalDevice{}, err
	}

	publicKeyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return domain.LocalDevice{}, err
	}

	device := domain.LocalDevice{
		DeviceID:      randomDeviceID(),
		DeviceName:    name,
		PublicKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyDER})),
		PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})),
		CreatedAt:     time.Now().UTC(),
	}

	if err := persistLocalDevice(identityFilePath, device); err != nil {
		return domain.LocalDevice{}, err
	}

	return device, nil
}

func randomDeviceID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "device-fallback"
	}

	return hex.EncodeToString(bytes)
}

func readLocalDevice(identityFilePath string) (domain.LocalDevice, error) {
	content, err := os.ReadFile(identityFilePath)
	if err != nil {
		return domain.LocalDevice{}, err
	}

	var device domain.LocalDevice
	if err := json.Unmarshal(content, &device); err != nil {
		return domain.LocalDevice{}, err
	}

	return device, nil
}

func persistLocalDevice(identityFilePath string, device domain.LocalDevice) error {
	if err := os.MkdirAll(filepath.Dir(identityFilePath), 0o755); err != nil {
		return err
	}

	content, err := json.MarshalIndent(device, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(identityFilePath, content, 0o600)
}
