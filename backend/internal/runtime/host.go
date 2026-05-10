package runtime

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"shareme/backend/internal/app"
	"shareme/backend/internal/config"
	"shareme/backend/internal/device"
	"shareme/backend/internal/discovery"
	"shareme/backend/internal/localfile"
	"shareme/backend/internal/protocol"
	"shareme/backend/internal/security"
	"shareme/backend/internal/session"
	"shareme/backend/internal/store"
	"shareme/backend/internal/transfer"
)

type Options struct {
	Config config.AppConfig
	Logger *log.Logger
	Now    func() time.Time
	Events app.EventPublisher
}

type Host struct {
	mu sync.Mutex

	cfg    config.AppConfig
	logger *log.Logger
	now    func() time.Time
	events app.EventPublisher
	errs   chan error

	runCancel func()
	started   bool

	store               *store.DB
	registry            *discovery.Registry
	runtimeService      *app.RuntimeService
	discoveryRunner     *discovery.Runner
	peerServer          *http.Server
	acceleratedListener *transfer.AcceleratedListener
}

func NewHost(opts Options) *Host {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}
	return &Host{
		cfg:    opts.Config,
		logger: opts.Logger,
		now:    opts.Now,
		events: opts.Events,
		errs:   make(chan error, 2),
	}
}

func (h *Host) Start(ctx context.Context) error {
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return nil
	}
	runCtx, runCancel := context.WithCancel(ctx)
	h.runCancel = runCancel
	h.started = true
	h.mu.Unlock()

	started := false
	defer func() {
		if !started {
			_ = h.Close(context.Background())
		}
	}()

	dbPath := h.cfg.DatabasePath
	if dbPath == "" {
		dbPath = filepath.Join(h.cfg.DataDir, "shareme.db")
	}
	db, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite store: %w", err)
	}

	localDevice, err := device.EnsureLocalDevice(h.cfg.IdentityFilePath, h.cfg.DeviceName)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("ensure local device: %w", err)
	}
	if err := db.SaveLocalDevice(localDevice); err != nil {
		_ = db.Close()
		return fmt.Errorf("persist local device: %w", err)
	}

	peerCertificate, err := security.BuildTLSCertificate(localDevice)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("build peer tls certificate: %w", err)
	}

	registry := discovery.NewRegistry()
	pairingService := session.NewService()
	localFileManager := localfile.NewManager(localfile.NewPicker(), localfile.DefaultLeaseTTL, h.now)
	peerTransport := protocol.NewHTTPPeerTransport(protocol.HTTPPeerTransportOptions{
		Scheme: "https",
		ClientFactory: func(expectedFingerprint string) *http.Client {
			return protocol.NewPeerHTTPClient(security.NewClientTLSConfig(peerCertificate, expectedFingerprint))
		},
		TransferClientFactory: func(expectedFingerprint string) *http.Client {
			return protocol.NewLANPeerHTTPClient(security.NewClientTLSConfig(peerCertificate, expectedFingerprint))
		},
	})

	var acceleratedListener *transfer.AcceleratedListener
	if h.cfg.AcceleratedEnabled {
		dataListener, err := net.Listen("tcp", fmt.Sprintf(":%d", h.cfg.AcceleratedDataPort))
		if err != nil {
			_ = db.Close()
			return fmt.Errorf("listen accelerated data port: %w", err)
		}
		acceleratedListener = transfer.NewAcceleratedListener(dataListener)
		go func() {
			if err := acceleratedListener.Serve(runCtx); err != nil && !errors.Is(err, net.ErrClosed) {
				h.logger.Printf("accelerated listener stopped with error: %v", err)
				h.reportAsyncError(fmt.Errorf("accelerated listener stopped: %w", err))
			}
		}()
	}

	runtimeService := app.NewRuntimeService(app.RuntimeDeps{
		Config:              h.cfg,
		Store:               db,
		Discovery:           registry,
		Pairings:            pairingService,
		Events:              h.events,
		Transport:           peerTransport,
		LocalFiles:          localFileManager,
		AcceleratedSessions: acceleratedListener,
	})

	discoveryRunner := discovery.NewRunner(discovery.RunnerOptions{
		ListenAddr:    h.cfg.DiscoveryListenAddr,
		BroadcastAddr: h.cfg.DiscoveryBroadcastAddr,
		LocalAnnouncement: discovery.Announcement{
			ProtocolVersion: "1",
			DeviceID:        localDevice.DeviceID,
			DeviceName:      localDevice.DeviceName,
			AgentTCPPort:    h.cfg.AgentTCPPort,
		},
		OnAnnouncement: func(announcement discovery.Announcement, addr string, seenAt time.Time) {
			registry.Upsert(announcement, addr, seenAt)
			h.publishPeerUpdate(runtimeService, announcement.DeviceID)
		},
	})
	if err := discoveryRunner.Start(runCtx); err != nil {
		if acceleratedListener != nil {
			_ = acceleratedListener.Close()
		}
		_ = db.Close()
		return fmt.Errorf("start discovery runner: %w", err)
	}

	peerServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", h.cfg.AgentTCPPort),
		Handler: protocol.NewPeerHTTPServer(runtimeService),
	}
	peerListener, err := net.Listen("tcp", peerServer.Addr)
	if err != nil {
		if err := discoveryRunner.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			h.logger.Printf("close discovery runner after listen failure: %v", err)
		}
		if acceleratedListener != nil {
			_ = acceleratedListener.Close()
		}
		_ = db.Close()
		return fmt.Errorf("listen peer server: %w", err)
	}
	go func() {
		tlsListener := tls.NewListener(peerListener, security.NewServerTLSConfig(peerCertificate))
		if err := peerServer.Serve(tlsListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.logger.Printf("peer server stopped with error: %v", err)
			h.reportAsyncError(fmt.Errorf("peer server stopped: %w", err))
		}
	}()
	go runtimeService.RunHeartbeatLoop(runCtx)

	h.mu.Lock()
	h.store = db
	h.registry = registry
	h.runtimeService = runtimeService
	h.discoveryRunner = discoveryRunner
	h.peerServer = peerServer
	h.acceleratedListener = acceleratedListener
	h.mu.Unlock()

	started = true
	return nil
}

func (h *Host) Close(ctx context.Context) error {
	h.mu.Lock()
	runCancel := h.runCancel
	peerServer := h.peerServer
	acceleratedListener := h.acceleratedListener
	discoveryRunner := h.discoveryRunner
	db := h.store

	h.runCancel = nil
	h.peerServer = nil
	h.acceleratedListener = nil
	h.discoveryRunner = nil
	h.runtimeService = nil
	h.registry = nil
	h.store = nil
	h.started = false
	h.mu.Unlock()

	if runCancel != nil {
		runCancel()
	}

	var errs []error
	if peerServer != nil {
		if err := peerServer.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, err)
		}
	}
	if acceleratedListener != nil {
		if err := acceleratedListener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	}
	if discoveryRunner != nil {
		if err := discoveryRunner.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	}
	if db != nil {
		if err := db.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (h *Host) RuntimeService() *app.RuntimeService {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.runtimeService
}

func (h *Host) Errors() <-chan error {
	return h.errs
}

func (h *Host) publishPeerUpdate(service *app.RuntimeService, deviceID string) {
	if h.events == nil || service == nil {
		return
	}

	snapshot, err := service.Bootstrap()
	if err != nil {
		h.logger.Printf("build bootstrap after discovery update: %v", err)
		return
	}

	for _, peer := range snapshot.Peers {
		if peer.DeviceID == deviceID {
			h.events.Publish("peer.updated", peer)
			h.events.Publish("health.updated", map[string]any{
				"status":        "ok",
				"localAPIReady": false,
				"agentPort":     h.cfg.AgentTCPPort,
				"discovery":     "broadcast-ok",
			})
			return
		}
	}
}

func (h *Host) reportAsyncError(err error) {
	if err == nil {
		return
	}

	select {
	case h.errs <- err:
	default:
	}
}
