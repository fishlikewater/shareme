package session

import "testing"

func TestPairingCodeIsStableForSameHandshakeInput(t *testing.T) {
	codeA := BuildPairingCode("nonce-a", "nonce-b")
	codeB := BuildPairingCode("nonce-a", "nonce-b")

	if codeA != codeB || len(codeA) != 6 {
		t.Fatalf("unexpected pairing code: %s %s", codeA, codeB)
	}
}
