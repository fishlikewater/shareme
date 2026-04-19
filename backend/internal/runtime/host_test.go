package runtime

import (
	"context"
	"io"
	"log"
	"net"
	"path/filepath"
	"testing"
	"time"

	"message-share/backend/internal/config"
)

func TestHostStartAndClose(t *testing.T) {
	cfg := newTestConfig(t)
	host := NewHost(Options{
		Config: cfg,
		Logger: log.New(io.Discard, "", 0),
		Now: func() time.Time {
			return time.Unix(1713398400, 0).UTC()
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	if err := host.Start(ctx); err != nil {
		cancel()
		t.Fatalf("start host: %v", err)
	}
	if host.RuntimeService() == nil {
		cancel()
		t.Fatal("expected runtime service to be initialized")
	}

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := host.Close(shutdownCtx); err != nil {
		t.Fatalf("close host: %v", err)
	}
}

func TestHostStartFailsWhenPeerPortIsUnavailable(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen occupied port: %v", err)
	}
	defer listener.Close()

	cfg := newTestConfig(t)
	cfg.AgentTCPPort = listener.Addr().(*net.TCPAddr).Port

	host := NewHost(Options{
		Config: cfg,
		Logger: log.New(io.Discard, "", 0),
		Now: func() time.Time {
			return time.Unix(1713398400, 0).UTC()
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := host.Start(ctx); err == nil {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = host.Close(shutdownCtx)
		t.Fatal("expected start to fail when peer port is unavailable")
	}
}

func newTestConfig(t *testing.T) config.AppConfig {
	t.Helper()

	dataDir := t.TempDir()
	return config.AppConfig{
		AgentTCPPort:           0,
		AcceleratedDataPort:    0,
		AcceleratedEnabled:     true,
		DiscoveryUDPPort:       0,
		DiscoveryListenAddr:    "127.0.0.1:0",
		DiscoveryBroadcastAddr: "",
		DataDir:                dataDir,
		DatabasePath:           filepath.Join(dataDir, "message-share.db"),
		DeviceName:             "测试设备",
		IdentityFilePath:       filepath.Join(dataDir, "local-device.json"),
		DefaultDownloadDir:     filepath.Join(dataDir, "downloads"),
		MaxAutoAcceptFileMB:    512,
	}
}
