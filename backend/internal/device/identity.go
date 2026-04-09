package device

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"time"

	"message-share/backend/internal/domain"
)

func EnsureLocalDevice(_ string, name string) (domain.LocalDevice, error) {
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

	return domain.LocalDevice{
		DeviceID:      randomDeviceID(),
		DeviceName:    name,
		PublicKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyDER})),
		PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})),
		CreatedAt:     time.Now().UTC(),
	}, nil
}

func randomDeviceID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "device-fallback"
	}

	return hex.EncodeToString(bytes)
}
