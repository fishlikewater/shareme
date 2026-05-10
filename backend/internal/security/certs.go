package security

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"shareme/backend/internal/domain"
)

type PinnedPeer struct {
	DeviceID    string `json:"deviceId"`
	Fingerprint string `json:"fingerprint"`
}

func BuildPinnedPeer(deviceID string, publicKeyMaterial string) PinnedPeer {
	sum := sha256.Sum256([]byte(publicKeyMaterial))
	return PinnedPeer{
		DeviceID:    deviceID,
		Fingerprint: hex.EncodeToString(sum[:]),
	}
}

func BuildTLSCertificate(device domain.LocalDevice) (tls.Certificate, error) {
	publicBlock, _ := pem.Decode([]byte(device.PublicKeyPEM))
	if publicBlock == nil {
		return tls.Certificate{}, fmt.Errorf("decode public key pem")
	}

	publicKey, err := x509.ParsePKIXPublicKey(publicBlock.Bytes)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parse public key: %w", err)
	}

	privateBlock, _ := pem.Decode([]byte(device.PrivateKeyPEM))
	if privateBlock == nil {
		return tls.Certificate{}, fmt.Errorf("decode private key pem")
	}

	privateKeyAny, err := x509.ParsePKCS8PrivateKey(privateBlock.Bytes)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parse private key: %w", err)
	}

	privateKey, ok := privateKeyAny.(ed25519.PrivateKey)
	if !ok {
		return tls.Certificate{}, fmt.Errorf("unexpected private key type")
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate serial number: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkixName(device.DeviceName),
		NotBefore:    time.Now().Add(-time.Hour).UTC(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour).UTC(),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create certificate: %w", err)
	}

	return tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		[]byte(device.PrivateKeyPEM),
	)
}

func FingerprintLeafDER(der []byte) (string, error) {
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return "", err
	}

	publicKeyDER, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil {
		return "", err
	}

	publicKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyDER})
	return BuildPinnedPeer("", string(publicKeyPEM)).Fingerprint, nil
}

func NewServerTLSConfig(certificate tls.Certificate) *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAnyClientCert,
	}
}

func NewClientTLSConfig(certificate tls.Certificate, expectedFingerprint string) *tls.Config {
	config := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{certificate},
		InsecureSkipVerify: true,
	}
	if expectedFingerprint == "" {
		return config
	}

	config.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return fmt.Errorf("peer certificate missing")
		}

		fingerprint, err := FingerprintLeafDER(state.PeerCertificates[0].Raw)
		if err != nil {
			return err
		}
		if fingerprint != expectedFingerprint {
			return fmt.Errorf("peer fingerprint mismatch")
		}
		return nil
	}
	return config
}

func pkixName(commonName string) pkix.Name {
	return pkix.Name{
		CommonName: commonName,
	}
}
