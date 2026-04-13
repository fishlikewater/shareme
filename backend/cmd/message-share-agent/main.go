package main

import (
	"context"
	"fmt"
	"log"
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
	"message-share/backend/internal/protocol"
	"message-share/backend/internal/security"
	"message-share/backend/internal/session"
	"message-share/backend/internal/store"
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
	peerTransport := protocol.NewHTTPPeerTransport(protocol.HTTPPeerTransportOptions{
		Scheme: "https",
		ClientFactory: func(expectedFingerprint string) *http.Client {
			return &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: security.NewClientTLSConfig(peerCertificate, expectedFingerprint),
				},
			}
		},
	})
	runtimeService := app.NewRuntimeService(app.RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: registry,
		Pairings:  pairingService,
		Events: app.EventPublisherFunc(func(kind string, payload any) {
			eventBus.Publish(kind, payload)
		}),
		Transport: peerTransport,
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
	defer discoveryRunner.Close()

	peerServer := &http.Server{
		Addr:      fmt.Sprintf(":%d", cfg.AgentTCPPort),
		Handler:   protocol.NewPeerHTTPServer(runtimeService),
		TLSConfig: security.NewServerTLSConfig(peerCertificate),
	}
	go func() {
		if err := peerServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Printf("peer server stopped with error: %v", err)
		}
	}()
	defer peerServer.Shutdown(context.Background())

	server := api.NewHTTPServer(runtimeService, eventBus, webui.Assets())
	log.Printf("Message Share agent bootstrap on %s", cfg.LocalAPIAddr)
	log.Fatal(http.ListenAndServe(cfg.LocalAPIAddr, server.Handler()))
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
