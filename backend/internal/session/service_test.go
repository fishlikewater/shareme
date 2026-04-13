package session

import "testing"

func TestSendTextMessageCreatesMessageWithPendingStatus(t *testing.T) {
	svc := NewService()
	msg := svc.NewTextMessage("conv-1", "hello")

	if msg.Kind != "text" || msg.Status != "sending" {
		t.Fatalf("unexpected message: %#v", msg)
	}
}

func TestStartPairingStoresPendingSession(t *testing.T) {
	svc := NewService()
	pairing := svc.StartPairing(PairingDraft{
		PairingID:         "pair-1",
		PeerDeviceID:      "peer-1",
		PeerDeviceName:    "office-pc",
		InitiatorNonce:    "nonce-a",
		ResponderNonce:    "nonce-b",
		RemoteFingerprint: "fingerprint-a",
		Initiator:         true,
	})

	if pairing.PeerDeviceID != "peer-1" {
		t.Fatalf("unexpected pairing: %#v", pairing)
	}
	if pairing.Status != PairingStatusPending {
		t.Fatalf("unexpected pairing status: %#v", pairing)
	}
	if len(svc.ListPairings()) != 1 {
		t.Fatalf("expected one pairing, got %#v", svc.ListPairings())
	}
}

func TestPairingBecomesConfirmedAfterLocalAndRemoteConfirm(t *testing.T) {
	svc := NewService()
	pairing := svc.StartPairing(PairingDraft{
		PairingID:         "pair-1",
		PeerDeviceID:      "peer-1",
		PeerDeviceName:    "office-pc",
		InitiatorNonce:    "nonce-a",
		ResponderNonce:    "nonce-b",
		RemoteFingerprint: "fingerprint-a",
		Initiator:         true,
	})

	localConfirmed, err := svc.MarkLocalConfirmed(pairing.PairingID)
	if err != nil {
		t.Fatalf("unexpected local confirm error: %v", err)
	}
	if localConfirmed.Status != PairingStatusPending {
		t.Fatalf("expected pending after local confirm, got %#v", localConfirmed)
	}

	remoteConfirmed, err := svc.MarkRemoteConfirmed(pairing.PairingID)
	if err != nil {
		t.Fatalf("unexpected remote confirm error: %v", err)
	}
	if remoteConfirmed.Status != PairingStatusConfirmed {
		t.Fatalf("expected confirmed pairing, got %#v", remoteConfirmed)
	}
	if !remoteConfirmed.LocalConfirmed || !remoteConfirmed.RemoteConfirmed {
		t.Fatalf("expected both confirmations, got %#v", remoteConfirmed)
	}
}
