package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"message-share/backend/internal/domain"
)

type Service struct {
	mu       sync.RWMutex
	pairings map[string]PairingSession
	messages []domain.Message
}

func NewService() *Service {
	return &Service{
		pairings: make(map[string]PairingSession),
	}
}

type PairingDraft struct {
	PairingID         string
	PeerDeviceID      string
	PeerDeviceName    string
	InitiatorNonce    string
	ResponderNonce    string
	RemoteFingerprint string
	Initiator         bool
}

func (s *Service) StartPairing(draft PairingDraft) PairingSession {
	s.mu.Lock()
	defer s.mu.Unlock()

	pairingID := draft.PairingID
	if pairingID == "" {
		pairingID = nextID("pair")
	}

	pairing := PairingSession{
		PairingID:         pairingID,
		PeerDeviceID:      draft.PeerDeviceID,
		PeerDeviceName:    draft.PeerDeviceName,
		ShortCode:         BuildPairingCode(draft.InitiatorNonce, draft.ResponderNonce),
		Status:            PairingStatusPending,
		InitiatorNonce:    draft.InitiatorNonce,
		ResponderNonce:    draft.ResponderNonce,
		RemoteFingerprint: draft.RemoteFingerprint,
		Initiator:         draft.Initiator,
	}
	s.pairings[pairing.PairingID] = pairing
	return pairing
}

func (s *Service) ListPairings() []PairingSession {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pairings := make([]PairingSession, 0, len(s.pairings))
	for _, pairing := range s.pairings {
		pairings = append(pairings, pairing)
	}
	return pairings
}

func (s *Service) MarkLocalConfirmed(pairingID string) (PairingSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pairing, ok := s.pairings[pairingID]
	if !ok {
		return PairingSession{}, ErrPairingNotFound
	}

	pairing.LocalConfirmed = true
	pairing.Status = pairingStatus(pairing)
	s.pairings[pairingID] = pairing
	return pairing, nil
}

func (s *Service) MarkRemoteConfirmed(pairingID string) (PairingSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pairing, ok := s.pairings[pairingID]
	if !ok {
		return PairingSession{}, ErrPairingNotFound
	}

	pairing.RemoteConfirmed = true
	pairing.Status = pairingStatus(pairing)
	s.pairings[pairingID] = pairing
	return pairing, nil
}

func (s *Service) GetPairing(pairingID string) (PairingSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pairing, ok := s.pairings[pairingID]
	return pairing, ok
}

func (s *Service) NewTextMessage(conversationID string, body string) domain.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	message := domain.Message{
		MessageID:      nextID("msg"),
		ConversationID: conversationID,
		Kind:           "text",
		Body:           body,
		Status:         "sending",
		CreatedAt:      time.Now().UTC(),
	}
	s.messages = append(s.messages, message)
	return message
}

func nextID(prefix string) string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return prefix + "-fallback"
	}

	return prefix + "-" + hex.EncodeToString(bytes)
}

func pairingStatus(pairing PairingSession) PairingStatus {
	if pairing.LocalConfirmed && pairing.RemoteConfirmed {
		return PairingStatusConfirmed
	}
	return PairingStatusPending
}
