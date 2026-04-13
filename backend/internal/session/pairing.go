package session

import (
	"crypto/sha256"
	"errors"
	"fmt"
)

type PairingStatus string

const (
	PairingStatusPending   PairingStatus = "pending"
	PairingStatusConfirmed PairingStatus = "confirmed"
	PairingStatusRejected  PairingStatus = "rejected"
)

type PairingSession struct {
	PairingID         string        `json:"pairingId"`
	PeerDeviceID      string        `json:"peerDeviceId"`
	PeerDeviceName    string        `json:"peerDeviceName"`
	ShortCode         string        `json:"shortCode"`
	Status            PairingStatus `json:"status"`
	InitiatorNonce    string        `json:"initiatorNonce"`
	ResponderNonce    string        `json:"responderNonce"`
	RemoteFingerprint string        `json:"remoteFingerprint"`
	Initiator         bool          `json:"initiator"`
	LocalConfirmed    bool          `json:"localConfirmed"`
	RemoteConfirmed   bool          `json:"remoteConfirmed"`
}

var ErrPairingNotFound = errors.New("pairing not found")

func BuildPairingCode(localNonce string, remoteNonce string) string {
	sum := sha256.Sum256([]byte(localNonce + ":" + remoteNonce))
	value := int(sum[0])<<8 | int(sum[1])
	return fmt.Sprintf("%06d", value%1000000)
}
