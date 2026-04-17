package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"message-share/backend/internal/api"
	"message-share/backend/internal/app"
	"message-share/backend/internal/config"
	"message-share/backend/internal/device"
	"message-share/backend/internal/discovery"
	"message-share/backend/internal/localfile"
	"message-share/backend/internal/protocol"
	"message-share/backend/internal/security"
	"message-share/backend/internal/session"
	"message-share/backend/internal/store"
	"message-share/backend/internal/transfer"
	"message-share/backend/internal/webui"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := config.Default()
	dbPath := filepath.Join(cfg.DataDir, "message-share.db")
	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open sqlite store: %v", err)
	}
	defer db.Close()

	localDevice, err := device.EnsureLocalDevice(cfg.IdentityFilePath, cfg.DeviceName)
	if err != nil {
		log.Fatalf("ensure local device: %v", err)
	}
	if err := db.SaveLocalDevice(localDevice); err != nil {
		log.Fatalf("persist local device: %v", err)
	}

	peerCertificate, err := security.BuildTLSCertificate(localDevice)
	if err != nil {
		log.Fatalf("build peer tls certificate: %v", err)
	}

	registry := discovery.NewRegistry()
	eventBus := api.NewEventBus()
	pairingService := session.NewService()
	localFileManager := localfile.NewManager(localfile.NewPicker(), localfile.DefaultLeaseTTL, time.Now)
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
	acceleratedListenerErrors := make(chan error, 1)
	if cfg.AcceleratedEnabled {
		dataListener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.AcceleratedDataPort))
		if err != nil {
			log.Fatalf("listen accelerated data port: %v", err)
		}
		acceleratedListener = transfer.NewAcceleratedListener(dataListener)
		go func() {
			if err := acceleratedListener.Serve(ctx); err != nil && err != net.ErrClosed {
				acceleratedListenerErrors <- err
			}
		}()
	}

	runtimeService := app.NewRuntimeService(app.RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: registry,
		Pairings:  pairingService,
		Events: app.EventPublisherFunc(func(kind string, payload any) {
			eventBus.Publish(kind, payload)
		}),
		Transport:           peerTransport,
		LocalFiles:          localFileManager,
		AcceleratedSessions: acceleratedListener,
	})

	discoveryRunner := discovery.NewRunner(discovery.RunnerOptions{
		ListenAddr:    cfg.DiscoveryListenAddr,
		BroadcastAddr: cfg.DiscoveryBroadcastAddr,
		LocalAnnouncement: discovery.Announcement{
			ProtocolVersion: "1",
			DeviceID:        localDevice.DeviceID,
			DeviceName:      localDevice.DeviceName,
			AgentTCPPort:    cfg.AgentTCPPort,
		},
		OnAnnouncement: func(announcement discovery.Announcement, addr string, seenAt time.Time) {
			registry.Upsert(announcement, addr, seenAt)
			publishPeerUpdate(runtimeService, eventBus, announcement.DeviceID, cfg.AgentTCPPort)
		},
	})
	if err := discoveryRunner.Start(ctx); err != nil {
		log.Fatalf("start discovery runner: %v", err)
	}

	peerServer := &http.Server{
		Addr:      fmt.Sprintf(":%d", cfg.AgentTCPPort),
		Handler:   protocol.NewPeerHTTPServer(runtimeService),
		TLSConfig: security.NewServerTLSConfig(peerCertificate),
	}
	peerServerErrors := make(chan error, 1)
	go func() {
		if err := peerServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			peerServerErrors <- err
		}
	}()
	go runtimeService.RunHeartbeatLoop(ctx)

	localAPI := api.NewHTTPServer(runtimeService, eventBus, webui.Assets())
	localServer := &http.Server{
		Addr:    cfg.LocalAPIAddr,
		Handler: localAPI.Handler(),
	}
	localServerErrors := make(chan error, 1)
	log.Printf("Message Share agent bootstrap on %s", cfg.LocalAPIAddr)
	go func() {
		if err := localServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			localServerErrors <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-peerServerErrors:
		log.Fatalf("peer server stopped with error: %v", err)
	case err := <-localServerErrors:
		log.Fatalf("local api server stopped with error: %v", err)
	case err := <-acceleratedListenerErrors:
		log.Fatalf("accelerated listener stopped with error: %v", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := localServer.Shutdown(shutdownCtx); err != nil && err != http.ErrServerClosed {
		log.Printf("shutdown local api server: %v", err)
	}
	if err := peerServer.Shutdown(shutdownCtx); err != nil && err != http.ErrServerClosed {
		log.Printf("shutdown peer server: %v", err)
	}
	if acceleratedListener != nil {
		if err := acceleratedListener.Close(); err != nil {
			log.Printf("shutdown accelerated listener: %v", err)
		}
	}
	if err := discoveryRunner.Close(); err != nil {
		log.Printf("close discovery runner: %v", err)
	}
}

func publishPeerUpdate(service *app.RuntimeService, eventBus *api.EventBus, deviceID string, agentPort int) {
	snapshot, err := service.Bootstrap()
	if err != nil {
		log.Printf("build bootstrap after discovery update: %v", err)
		return
	}

	for _, peer := range snapshot.Peers {
		if peer.DeviceID == deviceID {
			eventBus.Publish("peer.updated", peer)
			eventBus.Publish("health.updated", map[string]any{
				"status":        "ok",
				"localAPIReady": true,
				"agentPort":     agentPort,
				"discovery":     "broadcast-ok",
			})
			return
		}
	}
}
