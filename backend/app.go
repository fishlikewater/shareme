package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"shareme/backend/internal/api"
	appruntime "shareme/backend/internal/app"
	"shareme/backend/internal/config"
	"shareme/backend/internal/desktop"
	runtimehost "shareme/backend/internal/runtime"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type DesktopApp struct {
	mu sync.RWMutex

	ctx         context.Context
	startupErr  error
	cfg         config.AppConfig
	host        desktopHost
	events      *desktop.EventForwarder
	bridge      *desktop.Bridge
	quit        func(context.Context)
	watching    bool
	uiReadyOnce sync.Once
	uiReadyErr  error
}

func NewDesktopApp() (*DesktopApp, error) {
	cfg, err := config.LoadDefault()
	if err != nil {
		return nil, err
	}
	forwarder := desktop.NewEventForwarder(nil, desktop.EventEmitterFunc(func(ctx context.Context, eventName string, payload ...any) {
		wailsruntime.EventsEmit(ctx, eventName, payload...)
	}))
	desktopApp := &DesktopApp{
		events: forwarder,
		quit:   wailsruntime.Quit,
		cfg:    cfg,
	}
	desktopApp.host = runtimehost.NewHost(runtimehost.Options{
		Config: cfg,
		Events: desktopApp.events,
	})
	desktopApp.bridge = desktop.NewBridge(func() desktop.RuntimeCommands {
		if desktopApp.host == nil {
			return nil
		}
		return desktopApp.host.RuntimeService()
	}, desktop.WailsDialogs{})
	return desktopApp, nil
}

func (a *DesktopApp) Startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
	if a.events != nil {
		a.events.SetContext(ctx)
	}

	if err := a.host.Start(ctx); err != nil {
		a.setStartupErr(err)
		log.Printf("start runtime host: %v", err)
		a.quit(ctx)
		return
	}

	a.startWatchingRuntimeErrors(ctx)
}

func (a *DesktopApp) Shutdown(ctx context.Context) {
	if err := a.host.Close(ctx); err != nil {
		log.Printf("shutdown runtime host: %v", err)
	}
}

func (a *DesktopApp) Bootstrap() (DesktopBootstrapSnapshot, error) {
	if err := a.StartupError(); err != nil {
		return DesktopBootstrapSnapshot{}, err
	}

	snapshot, err := a.bridge.Bootstrap(a.commandContext())
	if err != nil {
		return DesktopBootstrapSnapshot{}, err
	}
	return DesktopBootstrapSnapshot{
		BootstrapSnapshot: snapshot,
		EventSeq:          a.lastEventSeq(),
	}, nil
}

func (a *DesktopApp) StartPairing(peerDeviceID string) (appruntime.PairingSnapshot, error) {
	if err := a.StartupError(); err != nil {
		return appruntime.PairingSnapshot{}, err
	}
	return a.bridge.StartPairing(a.commandContext(), peerDeviceID)
}

func (a *DesktopApp) ConfirmPairing(pairingID string) (appruntime.PairingSnapshot, error) {
	if err := a.StartupError(); err != nil {
		return appruntime.PairingSnapshot{}, err
	}
	return a.bridge.ConfirmPairing(a.commandContext(), pairingID)
}

func (a *DesktopApp) SendText(peerDeviceID string, body string) (appruntime.MessageSnapshot, error) {
	if err := a.StartupError(); err != nil {
		return appruntime.MessageSnapshot{}, err
	}
	return a.bridge.SendText(a.commandContext(), peerDeviceID, body)
}

func (a *DesktopApp) SendFile(peerDeviceID string) (appruntime.TransferSnapshot, error) {
	if err := a.StartupError(); err != nil {
		return appruntime.TransferSnapshot{}, err
	}
	return a.bridge.SendFile(a.commandContext(), peerDeviceID)
}

func (a *DesktopApp) SendFilePath(peerDeviceID string, path string) (appruntime.TransferSnapshot, error) {
	if err := a.StartupError(); err != nil {
		return appruntime.TransferSnapshot{}, err
	}
	return a.bridge.SendFilePath(a.commandContext(), peerDeviceID, path)
}

func (a *DesktopApp) PickLocalFile() (appruntime.LocalFileSnapshot, error) {
	if err := a.StartupError(); err != nil {
		return appruntime.LocalFileSnapshot{}, err
	}
	return a.bridge.PickLocalFile(a.commandContext())
}

func (a *DesktopApp) SendAcceleratedFile(peerDeviceID string, localFileID string) (appruntime.TransferSnapshot, error) {
	if err := a.StartupError(); err != nil {
		return appruntime.TransferSnapshot{}, err
	}
	return a.bridge.SendAcceleratedFile(a.commandContext(), peerDeviceID, localFileID)
}

func (a *DesktopApp) ListMessageHistory(conversationID string, beforeCursor string) (appruntime.MessageHistoryPageSnapshot, error) {
	if err := a.StartupError(); err != nil {
		return appruntime.MessageHistoryPageSnapshot{}, err
	}
	return a.bridge.ListMessageHistory(a.commandContext(), conversationID, beforeCursor)
}

func (a *DesktopApp) ReplayEvents(afterSeq int64) ([]api.Event, error) {
	if err := a.StartupError(); err != nil {
		return nil, err
	}
	if a.events == nil {
		return []api.Event{}, nil
	}
	return a.events.Since(afterSeq), nil
}

func (a *DesktopApp) UiReady() error {
	if err := a.StartupError(); err != nil {
		return err
	}

	a.uiReadyOnce.Do(func() {
		a.uiReadyErr = writeUIReadyMarker(uiReadyMarkerPayload{
			ReadyAt:             time.Now().UTC().Format(time.RFC3339Nano),
			AgentTCPPort:        a.cfg.AgentTCPPort,
			AcceleratedDataPort: a.cfg.AcceleratedDataPort,
			DiscoveryUDPPort:    a.cfg.DiscoveryUDPPort,
		})
	})
	return a.uiReadyErr
}

func (a *DesktopApp) StartupError() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.startupErr
}

func (a *DesktopApp) setStartupErr(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.startupErr = err
}

type DesktopBootstrapSnapshot struct {
	appruntime.BootstrapSnapshot
	EventSeq int64 `json:"eventSeq"`
}

type desktopHost interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
	RuntimeService() *appruntime.RuntimeService
	Errors() <-chan error
}

func (a *DesktopApp) startWatchingRuntimeErrors(ctx context.Context) {
	a.mu.Lock()
	if a.watching {
		a.mu.Unlock()
		return
	}
	a.watching = true
	a.mu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
			return
		case err := <-a.host.Errors():
			if err == nil {
				return
			}
			a.setStartupErr(err)
			log.Printf("runtime host async error: %v", err)
			a.quit(ctx)
		}
	}()
}

func (a *DesktopApp) commandContext() context.Context {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *DesktopApp) lastEventSeq() int64 {
	if a.events == nil {
		return 0
	}
	return a.events.LastSeq()
}

type uiReadyMarkerPayload struct {
	ReadyAt             string
	AgentTCPPort        int
	AcceleratedDataPort int
	DiscoveryUDPPort    int
}

func writeUIReadyMarker(payload uiReadyMarkerPayload) error {
	markerPath := strings.TrimSpace(os.Getenv("SHAREME_UI_READY_MARKER"))
	if markerPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		return err
	}
	content := strings.Join([]string{
		fmt.Sprintf("readyAt=%s", payload.ReadyAt),
		fmt.Sprintf("agentTcpPort=%d", payload.AgentTCPPort),
		fmt.Sprintf("acceleratedDataPort=%d", payload.AcceleratedDataPort),
		fmt.Sprintf("discoveryUdpPort=%d", payload.DiscoveryUDPPort),
		"",
	}, "\n")
	return os.WriteFile(markerPath, []byte(content), 0o600)
}
