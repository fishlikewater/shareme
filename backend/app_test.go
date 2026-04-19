package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"message-share/backend/internal/api"
	appruntime "message-share/backend/internal/app"
	"message-share/backend/internal/config"
	"message-share/backend/internal/desktop"
)

type fakeDesktopHost struct {
	startErr     error
	closeErr     error
	runtimeSvc   *appruntime.RuntimeService
	errorsStream chan error
}

func (h *fakeDesktopHost) Start(context.Context) error {
	return h.startErr
}

func (h *fakeDesktopHost) Close(context.Context) error {
	return h.closeErr
}

func (h *fakeDesktopHost) RuntimeService() *appruntime.RuntimeService {
	return h.runtimeSvc
}

func (h *fakeDesktopHost) Errors() <-chan error {
	return h.errorsStream
}

func TestDesktopAppStartupQuitsWhenRuntimeHostReportsAsyncError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	host := &fakeDesktopHost{
		errorsStream: make(chan error, 1),
	}
	quitCalled := make(chan struct{}, 1)
	app := &DesktopApp{
		host: host,
		quit: func(context.Context) {
			quitCalled <- struct{}{}
		},
	}

	app.Startup(ctx)
	host.errorsStream <- errors.New("peer server stopped")

	select {
	case <-quitCalled:
	case <-time.After(time.Second):
		t.Fatal("expected desktop app to quit after async runtime host error")
	}

	if err := app.StartupError(); err == nil {
		t.Fatal("expected startup error to be recorded after async runtime host error")
	}
}

func TestNewDesktopAppReturnsConfigError(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "home")
	rootDir := filepath.Join(homeDir, ".message-share")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("expected root dir to be created: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "config.json"), []byte("{invalid-json"), 0o600); err != nil {
		t.Fatalf("expected invalid config to be written: %v", err)
	}

	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("HOME", homeDir)

	if _, err := NewDesktopApp(); err == nil {
		t.Fatal("expected constructor to return config error")
	}
}

func TestDesktopAppBootstrapKeepsEventSeqCompatibility(t *testing.T) {
	forwarder := desktop.NewEventForwarder(api.NewEventBus(), nil)
	forwarder.Publish("peer.updated", map[string]any{"peerDeviceId": "peer-1"})

	app := &DesktopApp{
		events: forwarder,
		bridge: desktop.NewBridge(func() desktop.RuntimeCommands {
			return &desktopBridgeRuntimeStub{
				bootstrap: appruntime.BootstrapSnapshot{LocalDeviceName: "office-pc"},
			}
		}, nil),
	}

	snapshot, err := app.Bootstrap()
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if snapshot.LocalDeviceName != "office-pc" {
		t.Fatalf("unexpected bootstrap snapshot: %#v", snapshot)
	}
	if snapshot.EventSeq != 1 {
		t.Fatalf("expected EventSeq to reflect forwarded desktop events, got %d", snapshot.EventSeq)
	}
}

func TestDesktopAppReplayEventsReturnsBacklogAfterSeq(t *testing.T) {
	bus := api.NewEventBus()
	forwarder := desktop.NewEventForwarder(bus, nil)
	forwarder.Publish("peer.updated", map[string]any{"peerDeviceId": "peer-1"})
	forwarder.Publish("transfer.updated", map[string]any{"transferId": "tx-1"})

	app := &DesktopApp{
		events: forwarder,
	}

	events, err := app.ReplayEvents(1)
	if err != nil {
		t.Fatalf("ReplayEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 replayed event, got %d", len(events))
	}
	if events[0].EventSeq != 2 || events[0].Kind != "transfer.updated" {
		t.Fatalf("unexpected replayed event: %#v", events[0])
	}
}

func TestDesktopAppUiReadyWritesMarkerWhenConfigured(t *testing.T) {
	markerPath := filepath.Join(t.TempDir(), "ui-ready", "marker.txt")
	t.Setenv("MESSAGE_SHARE_UI_READY_MARKER", markerPath)

	app := &DesktopApp{
		cfg: config.AppConfig{
			AgentTCPPort:        52350,
			AcceleratedDataPort: 52351,
			DiscoveryUDPPort:    52352,
		},
	}
	if err := app.UiReady(); err != nil {
		t.Fatalf("UiReady() error = %v", err)
	}
	if err := app.UiReady(); err != nil {
		t.Fatalf("UiReady() second call error = %v", err)
	}

	content, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	marker := string(content)
	if !strings.Contains(marker, "readyAt=") {
		t.Fatalf("expected readyAt marker, got %q", marker)
	}
	if !strings.Contains(marker, "agentTcpPort=52350") {
		t.Fatalf("expected agent port marker, got %q", marker)
	}
	if !strings.Contains(marker, "acceleratedDataPort=52351") {
		t.Fatalf("expected accelerated port marker, got %q", marker)
	}
	if !strings.Contains(marker, "discoveryUdpPort=52352") {
		t.Fatalf("expected discovery port marker, got %q", marker)
	}
}

type desktopBridgeRuntimeStub struct {
	bootstrap appruntime.BootstrapSnapshot
}

func (s *desktopBridgeRuntimeStub) Bootstrap() (appruntime.BootstrapSnapshot, error) {
	return s.bootstrap, nil
}

func (s *desktopBridgeRuntimeStub) StartPairing(context.Context, string) (appruntime.PairingSnapshot, error) {
	return appruntime.PairingSnapshot{}, nil
}

func (s *desktopBridgeRuntimeStub) ConfirmPairing(context.Context, string) (appruntime.PairingSnapshot, error) {
	return appruntime.PairingSnapshot{}, nil
}

func (s *desktopBridgeRuntimeStub) SendTextMessage(context.Context, string, string) (appruntime.MessageSnapshot, error) {
	return appruntime.MessageSnapshot{}, nil
}

func (s *desktopBridgeRuntimeStub) SendFile(context.Context, string, string, int64, io.Reader) (appruntime.TransferSnapshot, error) {
	return appruntime.TransferSnapshot{}, nil
}

func (s *desktopBridgeRuntimeStub) RegisterLocalFile(context.Context, string) (appruntime.LocalFileSnapshot, error) {
	return appruntime.LocalFileSnapshot{}, nil
}

func (s *desktopBridgeRuntimeStub) SendAcceleratedFile(context.Context, string, string) (appruntime.TransferSnapshot, error) {
	return appruntime.TransferSnapshot{}, nil
}

func (s *desktopBridgeRuntimeStub) ListMessageHistory(context.Context, string, string) (appruntime.MessageHistoryPageSnapshot, error) {
	return appruntime.MessageHistoryPageSnapshot{}, nil
}
