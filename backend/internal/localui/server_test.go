package localui

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"shareme/backend/internal/api"
	appruntime "shareme/backend/internal/app"
)

type fakeCommands struct{}

func (fakeCommands) Bootstrap() (appruntime.BootstrapSnapshot, error) {
	return appruntime.BootstrapSnapshot{
		LocalDeviceName: "office-pc",
		Health: map[string]any{
			"status":        "ok",
			"discovery":     "broadcast-ok",
			"localAPIReady": true,
			"agentPort":     19090,
		},
		Peers:         []appruntime.PeerSnapshot{},
		Pairings:      []appruntime.PairingSnapshot{},
		Conversations: []appruntime.ConversationSnapshot{},
		Messages:      []appruntime.MessageSnapshot{},
		Transfers:     []appruntime.TransferSnapshot{},
	}, nil
}

func (fakeCommands) StartPairing(context.Context, string) (appruntime.PairingSnapshot, error) {
	return appruntime.PairingSnapshot{}, nil
}

func (fakeCommands) ConfirmPairing(context.Context, string) (appruntime.PairingSnapshot, error) {
	return appruntime.PairingSnapshot{}, nil
}

func (fakeCommands) SendTextMessage(context.Context, string, string) (appruntime.MessageSnapshot, error) {
	return appruntime.MessageSnapshot{}, nil
}

func (fakeCommands) SendFile(context.Context, string, string, int64, io.Reader) (appruntime.TransferSnapshot, error) {
	return appruntime.TransferSnapshot{}, nil
}

func (fakeCommands) PickLocalFile(context.Context) (appruntime.LocalFileSnapshot, error) {
	return appruntime.LocalFileSnapshot{}, nil
}

func (fakeCommands) SendAcceleratedFile(context.Context, string, string) (appruntime.TransferSnapshot, error) {
	return appruntime.TransferSnapshot{}, nil
}

func (fakeCommands) ListMessageHistory(context.Context, string, string) (appruntime.MessageHistoryPageSnapshot, error) {
	return appruntime.MessageHistoryPageSnapshot{}, nil
}

func TestBootstrapRejectsNonLoopback(t *testing.T) {
	service := NewService(func() RuntimeCommands { return fakeCommands{} }, api.NewEventBus())
	server := NewServer(ServiceDeps{Service: service})

	req := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	req.RemoteAddr = "192.168.1.10:45678"
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestBootstrapReturnsSnapshotAndEventSeq(t *testing.T) {
	bus := api.NewEventBus()
	bus.Publish("peer.updated", map[string]any{"deviceId": "peer-1"})

	service := NewService(func() RuntimeCommands { return fakeCommands{} }, bus)
	server := NewServer(ServiceDeps{Service: service})

	req := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	req.RemoteAddr = "127.0.0.1:45678"
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		LocalDeviceName string `json:"localDeviceName"`
		EventSeq        int64  `json:"eventSeq"`
	}
	if err := json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.LocalDeviceName != "office-pc" {
		t.Fatalf("expected office-pc, got %q", payload.LocalDeviceName)
	}
	if payload.EventSeq != 1 {
		t.Fatalf("expected event seq 1, got %d", payload.EventSeq)
	}
}

type routingCommands struct {
	startPairingCalls []string
	confirmCalls      []string
	sendTextCalls     []struct {
		PeerDeviceID string
		Body         string
	}
	acceleratedCalls []struct {
		PeerDeviceID string
		LocalFileID  string
	}
	historyCalls []struct {
		ConversationID string
		BeforeCursor   string
	}
}

func (c *routingCommands) Bootstrap() (appruntime.BootstrapSnapshot, error) {
	return fakeCommands{}.Bootstrap()
}

func (c *routingCommands) StartPairing(_ context.Context, peerDeviceID string) (appruntime.PairingSnapshot, error) {
	c.startPairingCalls = append(c.startPairingCalls, peerDeviceID)
	return appruntime.PairingSnapshot{PairingID: "pair-1", PeerDeviceID: peerDeviceID}, nil
}

func (c *routingCommands) ConfirmPairing(_ context.Context, pairingID string) (appruntime.PairingSnapshot, error) {
	c.confirmCalls = append(c.confirmCalls, pairingID)
	return appruntime.PairingSnapshot{PairingID: pairingID, Status: "confirmed"}, nil
}

func (c *routingCommands) SendTextMessage(_ context.Context, peerDeviceID string, body string) (appruntime.MessageSnapshot, error) {
	c.sendTextCalls = append(c.sendTextCalls, struct {
		PeerDeviceID string
		Body         string
	}{PeerDeviceID: peerDeviceID, Body: body})
	return appruntime.MessageSnapshot{MessageID: "msg-1", Body: body}, nil
}

func (c *routingCommands) SendFile(context.Context, string, string, int64, io.Reader) (appruntime.TransferSnapshot, error) {
	return appruntime.TransferSnapshot{}, nil
}

func (c *routingCommands) PickLocalFile(context.Context) (appruntime.LocalFileSnapshot, error) {
	return appruntime.LocalFileSnapshot{}, nil
}

func (c *routingCommands) SendAcceleratedFile(_ context.Context, peerDeviceID string, localFileID string) (appruntime.TransferSnapshot, error) {
	c.acceleratedCalls = append(c.acceleratedCalls, struct {
		PeerDeviceID string
		LocalFileID  string
	}{PeerDeviceID: peerDeviceID, LocalFileID: localFileID})
	return appruntime.TransferSnapshot{TransferID: "tx-acc-1"}, nil
}

func (c *routingCommands) ListMessageHistory(_ context.Context, conversationID string, beforeCursor string) (appruntime.MessageHistoryPageSnapshot, error) {
	c.historyCalls = append(c.historyCalls, struct {
		ConversationID string
		BeforeCursor   string
	}{ConversationID: conversationID, BeforeCursor: beforeCursor})
	return appruntime.MessageHistoryPageSnapshot{
		ConversationID: conversationID,
		Messages: []appruntime.MessageSnapshot{
			{MessageID: "msg-older"},
		},
		HasMore:    false,
		NextCursor: "",
	}, nil
}

func TestCommandRoutesMapToRuntimeService(t *testing.T) {
	testCases := []struct {
		Name       string
		Method     string
		Path       string
		Body       string
		Check      func(t *testing.T, commands *routingCommands)
		WantStatus int
	}{
		{
			Name:       "start pairing",
			Method:     http.MethodPost,
			Path:       "/api/pairings",
			Body:       `{"peerDeviceId":"peer-1"}`,
			WantStatus: http.StatusOK,
			Check: func(t *testing.T, commands *routingCommands) {
				t.Helper()
				if len(commands.startPairingCalls) != 1 || commands.startPairingCalls[0] != "peer-1" {
					t.Fatalf("unexpected start pairing calls: %#v", commands.startPairingCalls)
				}
			},
		},
		{
			Name:       "confirm pairing",
			Method:     http.MethodPost,
			Path:       "/api/pairings/pair-1/confirm",
			WantStatus: http.StatusOK,
			Check: func(t *testing.T, commands *routingCommands) {
				t.Helper()
				if len(commands.confirmCalls) != 1 || commands.confirmCalls[0] != "pair-1" {
					t.Fatalf("unexpected confirm calls: %#v", commands.confirmCalls)
				}
			},
		},
		{
			Name:       "send text",
			Method:     http.MethodPost,
			Path:       "/api/peers/peer-1/messages/text",
			Body:       `{"body":"hello"}`,
			WantStatus: http.StatusOK,
			Check: func(t *testing.T, commands *routingCommands) {
				t.Helper()
				if len(commands.sendTextCalls) != 1 {
					t.Fatalf("unexpected send text calls: %#v", commands.sendTextCalls)
				}
				if commands.sendTextCalls[0].PeerDeviceID != "peer-1" || commands.sendTextCalls[0].Body != "hello" {
					t.Fatalf("unexpected send text payload: %#v", commands.sendTextCalls[0])
				}
			},
		},
		{
			Name:       "accelerated file",
			Method:     http.MethodPost,
			Path:       "/api/peers/peer-1/transfers/accelerated",
			Body:       `{"localFileId":"lf-1"}`,
			WantStatus: http.StatusOK,
			Check: func(t *testing.T, commands *routingCommands) {
				t.Helper()
				if len(commands.acceleratedCalls) != 1 {
					t.Fatalf("unexpected accelerated calls: %#v", commands.acceleratedCalls)
				}
				if commands.acceleratedCalls[0].PeerDeviceID != "peer-1" || commands.acceleratedCalls[0].LocalFileID != "lf-1" {
					t.Fatalf("unexpected accelerated payload: %#v", commands.acceleratedCalls[0])
				}
			},
		},
		{
			Name:       "history",
			Method:     http.MethodGet,
			Path:       "/api/conversations/conv-1/messages?beforeCursor=cursor-1",
			WantStatus: http.StatusOK,
			Check: func(t *testing.T, commands *routingCommands) {
				t.Helper()
				if len(commands.historyCalls) != 1 {
					t.Fatalf("unexpected history calls: %#v", commands.historyCalls)
				}
				if commands.historyCalls[0].ConversationID != "conv-1" || commands.historyCalls[0].BeforeCursor != "cursor-1" {
					t.Fatalf("unexpected history payload: %#v", commands.historyCalls[0])
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			commands := &routingCommands{}
			service := NewService(func() RuntimeCommands { return commands }, api.NewEventBus())
			server := NewServer(ServiceDeps{Service: service})

			var body io.Reader
			if strings.TrimSpace(tc.Body) != "" {
				body = strings.NewReader(tc.Body)
			}
			req := httptest.NewRequest(tc.Method, tc.Path, body)
			req.RemoteAddr = "127.0.0.1:45678"
			if strings.TrimSpace(tc.Body) != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()

			server.Handler().ServeHTTP(rec, req)

			if rec.Code != tc.WantStatus {
				t.Fatalf("expected %d, got %d body=%s", tc.WantStatus, rec.Code, rec.Body.String())
			}
			tc.Check(t, commands)
		})
	}
}
