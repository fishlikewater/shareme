package device

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"message-share/backend/internal/domain"
)

func EnsureLocalDevice(identityFilePath string, name string) (domain.LocalDevice, error) {
	if existing, err := readLocalDevice(identityFilePath); err == nil {
		if existing.DeviceName != name {
			existing.DeviceName = name
			if err := persistLocalDevice(identityFilePath, existing); err != nil {
				return domain.LocalDevice{}, err
			}
		}
		return existing, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return domain.LocalDevice{}, err
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

func ValidateLocalDeviceFile(identityFilePath string) error {
	_, err := readLocalDevice(identityFilePath)
	return err
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
	if err := validateLocalDevice(device); err != nil {
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

func validateLocalDevice(device domain.LocalDevice) error {
	if strings.TrimSpace(device.DeviceID) == "" {
		return errors.New("invalid local device: device id is required")
	}
	if strings.TrimSpace(device.DeviceName) == "" {
		return errors.New("invalid local device: device name is required")
	}
	if err := validatePublicKeyPEM(device.PublicKeyPEM); err != nil {
		return err
	}
	if err := validatePrivateKeyPEM(device.PrivateKeyPEM); err != nil {
		return err
	}
	return nil
}

func validatePublicKeyPEM(value string) error {
	block, _ := pem.Decode([]byte(strings.TrimSpace(value)))
	if block == nil {
		return errors.New("invalid local device: public key pem is required")
	}
	publicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("invalid local device: parse public key pem: %w", err)
	}
	if _, ok := publicKey.(ed25519.PublicKey); !ok {
		return errors.New("invalid local device: public key pem must contain ed25519 key")
	}
	return nil
}

func validatePrivateKeyPEM(value string) error {
	block, _ := pem.Decode([]byte(strings.TrimSpace(value)))
	if block == nil {
		return errors.New("invalid local device: private key pem is required")
	}
	privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("invalid local device: parse private key pem: %w", err)
	}
	if _, ok := privateKey.(ed25519.PrivateKey); !ok {
		return errors.New("invalid local device: private key pem must contain ed25519 key")
	}
	return nil
}
