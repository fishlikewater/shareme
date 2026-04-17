package app

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"message-share/backend/internal/config"
	"message-share/backend/internal/discovery"
	"message-share/backend/internal/domain"
	"message-share/backend/internal/localfile"
	"message-share/backend/internal/protocol"
	"message-share/backend/internal/session"
	"message-share/backend/internal/transfer"
)

func TestPrepareAcceleratedTransferRegistersIncomingSession(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("open app test store: %v", err)
	}
	defer db.Close()

	cfg := config.Default()
	cfg.DefaultDownloadDir = t.TempDir()
	cfg.AcceleratedEnabled = true
	cfg.AcceleratedDataPort = 19092

	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-prepare",
		DeviceName:        "peer-prepare",
		PinnedFingerprint: "fingerprint-prepare",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registrar := &capturingAcceleratedRegistrar{}
	svc := NewRuntimeService(RuntimeDeps{
		Config:              cfg,
		Store:               db,
		Discovery:           discovery.NewRegistry(),
		Pairings:            session.NewService(),
		AcceleratedSessions: registrar,
	})

	response, err := svc.PrepareAcceleratedTransfer(context.Background(), protocol.AcceleratedPrepareRequest{
		TransferID:     "transfer-prepare",
		MessageID:      "msg-prepare",
		SenderDeviceID: "peer-prepare",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello world")),
	})
	if err != nil {
		t.Fatalf("PrepareAcceleratedTransfer() error = %v", err)
	}
	t.Cleanup(func() {
		_ = registrar.registration.Receiver.Cleanup()
		svc.deleteIncomingAcceleratedSession(response.SessionID)
		svc.transfers.Finish("transfer-prepare")
	})
	if response.SessionID == "" || response.TransferToken == "" || response.DataPort != 19092 {
		t.Fatalf("unexpected accelerated prepare response: %#v", response)
	}
	if registrar.registration.SessionID != response.SessionID {
		t.Fatalf("expected registrar to receive session %q, got %#v", response.SessionID, registrar.registration)
	}
	transfers, err := db.ListTransfers()
	if err != nil {
		t.Fatalf("ListTransfers() error = %v", err)
	}
	if len(transfers) != 1 || transfers[0].TransferID != "transfer-prepare" || transfers[0].State != transfer.StateReceiving {
		t.Fatalf("unexpected persisted transfers: %#v", transfers)
	}
}

func TestSendAcceleratedFileFallsBackWithSameTransferID(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("open app test store: %v", err)
	}
	defer db.Close()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "sender",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-fallback",
		DeviceName:        "peer-fallback",
		PinnedFingerprint: "fingerprint-fallback",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-fallback",
		DeviceName:      "peer-fallback",
		AgentTCPPort:    19090,
	}, "192.168.1.20:19090", time.Now().UTC())

	dir := t.TempDir()
	path := filepath.Join(dir, "demo.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := os.Truncate(path, multipartThreshold+1024); err != nil {
		t.Fatalf("Truncate() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	transport := &fakeAcceleratedPeerTransport{
		fakePeerTransport: &fakePeerTransport{
			fileResponse: protocol.FileTransferResponse{
				TransferID: "transfer-fallback",
				State:      transfer.StateDone,
			},
		},
		prepareErr: errors.New("prepare failed"),
	}
	svc := NewRuntimeService(RuntimeDeps{
		Config: config.AppConfig{
			AgentTCPPort:        19090,
			AcceleratedEnabled:  true,
			AcceleratedDataPort: 19092,
			DefaultDownloadDir:  t.TempDir(),
		},
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Transport: transport,
		LocalFiles: fakeLocalFileResolver{
			resolveLease: localfile.Lease{
				LocalFileID: "lf-1",
				Path:        path,
				DisplayName: "demo.bin",
				Size:        info.Size(),
				ModifiedAt:  info.ModTime().UTC(),
				ExpiresAt:   time.Now().Add(time.Hour),
			},
		},
	})

	snapshot, err := svc.SendAcceleratedFile(context.Background(), "peer-fallback", "lf-1")
	if err != nil {
		t.Fatalf("SendAcceleratedFile() error = %v", err)
	}
	if snapshot.State != transfer.StateDone {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if transport.prepareRequest.TransferID == "" {
		t.Fatal("expected prepare request to be recorded")
	}
	if transport.fileRequest.TransferID != transport.prepareRequest.TransferID {
		t.Fatalf("expected fallback to reuse transfer id, prepare=%q file=%q", transport.prepareRequest.TransferID, transport.fileRequest.TransferID)
	}
	if transport.fileRequest.MessageID != transport.prepareRequest.MessageID {
		t.Fatalf("expected fallback to reuse message id, prepare=%q file=%q", transport.prepareRequest.MessageID, transport.fileRequest.MessageID)
	}
}

func TestSendAcceleratedFileUsesStandardPathWhenLeaseIsNotEligible(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("open app test store: %v", err)
	}
	defer db.Close()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-standard",
		DeviceName:    "sender",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-standard",
		DeviceName:        "peer-standard",
		PinnedFingerprint: "fingerprint-standard",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-standard",
		DeviceName:      "peer-standard",
		AgentTCPPort:    19090,
	}, "192.168.1.30:19090", time.Now().UTC())

	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	content := []byte("hello standard path")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write small file: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat small file: %v", err)
	}
	if info.Size() >= multipartThreshold {
		t.Fatalf("expected test file smaller than threshold, got %d", info.Size())
	}

	transport := &fakeAcceleratedPeerTransport{
		fakePeerTransport: &fakePeerTransport{
			fileResponse: protocol.FileTransferResponse{
				TransferID: "transfer-standard",
				State:      transfer.StateDone,
			},
		},
	}
	svc := NewRuntimeService(RuntimeDeps{
		Config: config.AppConfig{
			AgentTCPPort:        19090,
			AcceleratedEnabled:  true,
			AcceleratedDataPort: 19092,
			DefaultDownloadDir:  t.TempDir(),
		},
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Transport: transport,
		LocalFiles: fakeLocalFileResolver{
			resolveLease: localfile.Lease{
				LocalFileID: "lf-small",
				Path:        path,
				DisplayName: "notes.txt",
				Size:        info.Size(),
				ModifiedAt:  info.ModTime().UTC(),
				ExpiresAt:   time.Now().Add(time.Hour),
			},
		},
	})

	snapshot, err := svc.SendAcceleratedFile(context.Background(), "peer-standard", "lf-small")
	if err != nil {
		t.Fatalf("SendAcceleratedFile() error = %v", err)
	}
	if snapshot.State != transfer.StateDone {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if transport.prepareRequest.TransferID != "" {
		t.Fatalf("expected non-eligible file to skip accelerated prepare, got %#v", transport.prepareRequest)
	}
	if transport.fileRequest.TransferID == "" || transport.fileRequest.MessageID == "" {
		t.Fatalf("expected standard path request to carry unified ids, got %#v", transport.fileRequest)
	}
}

func TestCompleteAcceleratedTransferCommitsIncomingFile(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("open app test store: %v", err)
	}
	defer db.Close()

	cfg := config.Default()
	cfg.DefaultDownloadDir = t.TempDir()
	cfg.AcceleratedEnabled = true
	cfg.AcceleratedDataPort = 19092

	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-complete",
		DeviceName:        "peer-complete",
		PinnedFingerprint: "fingerprint-complete",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registrar := &capturingAcceleratedRegistrar{}
	svc := NewRuntimeService(RuntimeDeps{
		Config:              cfg,
		Store:               db,
		Discovery:           discovery.NewRegistry(),
		Pairings:            session.NewService(),
		AcceleratedSessions: registrar,
	})

	response, err := svc.PrepareAcceleratedTransfer(context.Background(), protocol.AcceleratedPrepareRequest{
		TransferID:     "transfer-complete",
		MessageID:      "msg-complete",
		SenderDeviceID: "peer-complete",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello world")),
	})
	if err != nil {
		t.Fatalf("PrepareAcceleratedTransfer() error = %v", err)
	}
	t.Cleanup(func() {
		_ = registrar.registration.Receiver.Cleanup()
		svc.deleteIncomingAcceleratedSession(response.SessionID)
		svc.transfers.Finish("transfer-complete")
	})
	if _, err := registrar.registration.Receiver.ReceiveFrame(0, []byte("hello ")); err != nil {
		t.Fatalf("ReceiveFrame() first chunk error = %v", err)
	}
	if _, err := registrar.registration.Receiver.ReceiveFrame(6, []byte("world")); err != nil {
		t.Fatalf("ReceiveFrame() second chunk error = %v", err)
	}

	completeResponse, err := svc.CompleteAcceleratedTransfer(context.Background(), protocol.AcceleratedCompleteRequest{
		SessionID:  response.SessionID,
		TransferID: "transfer-complete",
		FileSHA256: appSessionTestSHA256Hex([]byte("hello world")),
	})
	if err != nil {
		t.Fatalf("CompleteAcceleratedTransfer() error = %v", err)
	}
	if completeResponse.State != transfer.StateDone {
		t.Fatalf("unexpected complete response: %#v", completeResponse)
	}
	files, err := filepath.Glob(filepath.Join(cfg.DefaultDownloadDir, "hello*.txt"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected committed file, got %#v", files)
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "hello world" {
		t.Fatalf("unexpected committed content: %q", string(content))
	}
}

func TestCompleteAcceleratedTransferRejectsMissingFileSHA256(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("open app test store: %v", err)
	}
	defer db.Close()

	cfg := config.Default()
	cfg.DefaultDownloadDir = t.TempDir()
	cfg.AcceleratedEnabled = true
	cfg.AcceleratedDataPort = 19092

	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-empty-sha",
		DeviceName:        "peer-empty-sha",
		PinnedFingerprint: "fingerprint-empty-sha",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registrar := &capturingAcceleratedRegistrar{}
	svc := NewRuntimeService(RuntimeDeps{
		Config:              cfg,
		Store:               db,
		Discovery:           discovery.NewRegistry(),
		Pairings:            session.NewService(),
		AcceleratedSessions: registrar,
	})

	response, err := svc.PrepareAcceleratedTransfer(context.Background(), protocol.AcceleratedPrepareRequest{
		TransferID:     "transfer-empty-sha",
		MessageID:      "msg-empty-sha",
		SenderDeviceID: "peer-empty-sha",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello world")),
	})
	if err != nil {
		t.Fatalf("PrepareAcceleratedTransfer() error = %v", err)
	}
	t.Cleanup(func() {
		_ = registrar.registration.Receiver.Cleanup()
		svc.deleteIncomingAcceleratedSession(response.SessionID)
		svc.transfers.Finish("transfer-empty-sha")
	})
	if _, err := registrar.registration.Receiver.ReceiveFrame(0, []byte("hello ")); err != nil {
		t.Fatalf("ReceiveFrame() first chunk error = %v", err)
	}
	if _, err := registrar.registration.Receiver.ReceiveFrame(6, []byte("world")); err != nil {
		t.Fatalf("ReceiveFrame() second chunk error = %v", err)
	}

	if _, err := svc.CompleteAcceleratedTransfer(context.Background(), protocol.AcceleratedCompleteRequest{
		SessionID:  response.SessionID,
		TransferID: "transfer-empty-sha",
		FileSHA256: "",
	}); err == nil {
		t.Fatal("expected missing file sha256 to fail")
	}

	files, err := filepath.Glob(filepath.Join(cfg.DefaultDownloadDir, "hello*.txt"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no committed file, got %#v", files)
	}
	transfers, err := db.ListTransfers()
	if err != nil {
		t.Fatalf("ListTransfers() error = %v", err)
	}
	if len(transfers) != 1 || transfers[0].State != transfer.StateFailed {
		t.Fatalf("expected failed transfer snapshot, got %#v", transfers)
	}
}

func TestSendAcceleratedFileDoesNotFallbackOnCompleteFailure(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("open app test store: %v", err)
	}
	defer db.Close()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-complete-fail",
		DeviceName:    "sender",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-complete-fail",
		DeviceName:        "peer-complete-fail",
		PinnedFingerprint: "fingerprint-complete-fail",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-complete-fail",
		DeviceName:      "peer-complete-fail",
		AgentTCPPort:    19090,
	}, "127.0.0.1:19090", time.Now().UTC())

	dir := t.TempDir()
	path := filepath.Join(dir, "demo.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := os.Truncate(path, multipartThreshold+1024); err != nil {
		t.Fatalf("Truncate() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	transport := &fakeAcceleratedPeerTransport{
		fakePeerTransport: &fakePeerTransport{
			fileResponse: protocol.FileTransferResponse{
				TransferID: "transfer-should-not-fallback",
				State:      transfer.StateDone,
			},
		},
		prepareResponse: protocol.AcceleratedPrepareResponse{
			SessionID:      "session-complete-fail",
			TransferToken:  "token-complete-fail",
			DataPort:       19092,
			ChunkSize:      8 << 20,
			InitialStripes: 1,
			MaxStripes:     1,
		},
		completeErr: errors.New("file sha256 mismatch"),
	}
	sender := &fakeAcceleratedSender{}
	svc := NewRuntimeService(RuntimeDeps{
		Config: config.AppConfig{
			AgentTCPPort:        19090,
			AcceleratedEnabled:  true,
			AcceleratedDataPort: 19092,
			DefaultDownloadDir:  t.TempDir(),
		},
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Transport: transport,
		LocalFiles: fakeLocalFileResolver{
			resolveLease: localfile.Lease{
				LocalFileID: "lf-1",
				Path:        path,
				DisplayName: "demo.bin",
				Size:        info.Size(),
				ModifiedAt:  info.ModTime().UTC(),
				ExpiresAt:   time.Now().Add(time.Hour),
			},
		},
		AcceleratedSenderFactory: func(discovery.PeerRecord) AcceleratedFileSender {
			return sender
		},
	})

	if _, err := svc.SendAcceleratedFile(context.Background(), "peer-complete-fail", "lf-1"); err == nil {
		t.Fatal("expected complete failure to propagate")
	}
	if transport.fileRequest.TransferID != "" {
		t.Fatalf("expected complete failure to skip fallback, got %#v", transport.fileRequest)
	}
	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if len(bootstrap.Transfers) != 1 || bootstrap.Transfers[0].State != transfer.StateFailed {
		t.Fatalf("expected failed transfer snapshot, got %#v", bootstrap.Transfers)
	}
}

func TestSendAcceleratedFileSuccessfulPathKeepsUnifiedIDs(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("open app test store: %v", err)
	}
	defer db.Close()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-success",
		DeviceName:    "sender",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-success",
		DeviceName:        "peer-success",
		PinnedFingerprint: "fingerprint-success",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-success",
		DeviceName:      "peer-success",
		AgentTCPPort:    19090,
	}, "127.0.0.1:19090", time.Now().UTC())

	dir := t.TempDir()
	path := filepath.Join(dir, "archive.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := os.Truncate(path, multipartThreshold+2048); err != nil {
		t.Fatalf("Truncate() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	transport := &fakeAcceleratedPeerTransport{
		fakePeerTransport: &fakePeerTransport{},
		prepareResponse: protocol.AcceleratedPrepareResponse{
			SessionID:      "session-success",
			TransferToken:  "token-success",
			DataPort:       19092,
			ChunkSize:      8 << 20,
			InitialStripes: 1,
			MaxStripes:     4,
		},
		completeResponse: protocol.AcceleratedCompleteResponse{
			TransferID: "transfer-success",
			State:      transfer.StateDone,
		},
	}
	sender := &fakeAcceleratedSender{}
	svc := NewRuntimeService(RuntimeDeps{
		Config: config.AppConfig{
			AgentTCPPort:        19090,
			AcceleratedEnabled:  true,
			AcceleratedDataPort: 19092,
			DefaultDownloadDir:  t.TempDir(),
		},
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Transport: transport,
		LocalFiles: fakeLocalFileResolver{
			resolveLease: localfile.Lease{
				LocalFileID: "lf-success",
				Path:        path,
				DisplayName: "archive.bin",
				Size:        info.Size(),
				ModifiedAt:  info.ModTime().UTC(),
				ExpiresAt:   time.Now().Add(time.Hour),
			},
		},
		AcceleratedSenderFactory: func(discovery.PeerRecord) AcceleratedFileSender {
			return sender
		},
	})

	snapshot, err := svc.SendAcceleratedFile(context.Background(), "peer-success", "lf-success")
	if err != nil {
		t.Fatalf("SendAcceleratedFile() error = %v", err)
	}
	if snapshot.State != transfer.StateDone {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if transport.prepareRequest.TransferID == "" || transport.prepareRequest.MessageID == "" {
		t.Fatalf("expected accelerated prepare request to carry unified ids, got %#v", transport.prepareRequest)
	}
	if snapshot.TransferID != transport.prepareRequest.TransferID {
		t.Fatalf("expected snapshot transfer id %q, got %q", transport.prepareRequest.TransferID, snapshot.TransferID)
	}
	if snapshot.MessageID != transport.prepareRequest.MessageID {
		t.Fatalf("expected snapshot message id %q, got %q", transport.prepareRequest.MessageID, snapshot.MessageID)
	}
	if transport.completeRequest.TransferID != transport.prepareRequest.TransferID {
		t.Fatalf("expected complete request to reuse transfer id, prepare=%q complete=%q", transport.prepareRequest.TransferID, transport.completeRequest.TransferID)
	}
	if transport.completeRequest.SessionID != transport.prepareResponse.SessionID {
		t.Fatalf("expected complete request to reuse session id %q, got %q", transport.prepareResponse.SessionID, transport.completeRequest.SessionID)
	}
	if sender.lastPrepare.SessionID != transport.prepareResponse.SessionID {
		t.Fatalf("expected sender to receive prepare response session %q, got %#v", transport.prepareResponse.SessionID, sender.lastPrepare)
	}
	if transport.fileRequest.TransferID != "" {
		t.Fatalf("expected successful accelerated path to skip fallback, got %#v", transport.fileRequest)
	}
}

func TestSendAcceleratedFileLoopbackIntegrationCompletesWithoutFallback(t *testing.T) {
	senderDB, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("open sender store: %v", err)
	}
	defer senderDB.Close()
	receiverDB, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("open receiver store: %v", err)
	}
	defer receiverDB.Close()

	senderDeviceID := "sender-loopback"
	receiverDeviceID := "receiver-loopback"
	fingerprint := "fingerprint-loopback"

	if err := senderDB.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      senderDeviceID,
		DeviceName:    "sender",
		PublicKeyPEM:  "sender-public",
		PrivateKeyPEM: "sender-private",
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save sender local device: %v", err)
	}
	if err := senderDB.UpsertTrustedPeer(domain.Peer{
		DeviceID:          receiverDeviceID,
		DeviceName:        "receiver",
		PinnedFingerprint: fingerprint,
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert sender trusted peer: %v", err)
	}
	if err := receiverDB.UpsertTrustedPeer(domain.Peer{
		DeviceID:          senderDeviceID,
		DeviceName:        "sender",
		PinnedFingerprint: fingerprint,
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert receiver trusted peer: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen accelerated tcp: %v", err)
	}
	dataPort := listener.Addr().(*net.TCPAddr).Port
	acceleratedListener := transfer.NewAcceleratedListener(listener)
	serveCtx, cancelServe := context.WithCancel(context.Background())
	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- acceleratedListener.Serve(serveCtx)
	}()
	t.Cleanup(func() {
		cancelServe()
		_ = acceleratedListener.Close()
		select {
		case err := <-serveErrCh:
			if err != nil {
				t.Errorf("accelerated listener exited with error: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("accelerated listener did not stop")
		}
	})

	receiverCfg := config.Default()
	receiverCfg.DefaultDownloadDir = t.TempDir()
	receiverCfg.AcceleratedEnabled = true
	receiverCfg.AcceleratedDataPort = dataPort
	receiverCfg.AgentTCPPort = 19091
	receiverService := NewRuntimeService(RuntimeDeps{
		Config:              receiverCfg,
		Store:               receiverDB,
		Discovery:           discovery.NewRegistry(),
		Pairings:            session.NewService(),
		AcceleratedSessions: acceleratedListener,
	})

	senderRegistry := discovery.NewRegistry()
	senderRegistry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        receiverDeviceID,
		DeviceName:      "receiver",
		AgentTCPPort:    receiverCfg.AgentTCPPort,
	}, net.JoinHostPort("127.0.0.1", strconv.Itoa(receiverCfg.AgentTCPPort)), time.Now().UTC())

	transport := &loopbackAcceleratedPeerTransport{
		fakePeerTransport: &fakePeerTransport{},
		receiver:          receiverService,
	}

	sourceDir := t.TempDir()
	sourcePath := filepath.Join(sourceDir, "loopback.bin")
	sourceFile, err := os.Create(sourcePath)
	if err != nil {
		t.Fatalf("create source file: %v", err)
	}
	if err := sourceFile.Close(); err != nil {
		t.Fatalf("close source file: %v", err)
	}
	if err := os.Truncate(sourcePath, multipartThreshold+1024); err != nil {
		t.Fatalf("truncate source file: %v", err)
	}
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatalf("stat source file: %v", err)
	}

	senderService := NewRuntimeService(RuntimeDeps{
		Config: config.AppConfig{
			AgentTCPPort:        19090,
			AcceleratedEnabled:  true,
			AcceleratedDataPort: 19092,
			DefaultDownloadDir:  t.TempDir(),
		},
		Store:     senderDB,
		Discovery: senderRegistry,
		Pairings:  session.NewService(),
		Transport: transport,
		LocalFiles: fakeLocalFileResolver{
			resolveLease: localfile.Lease{
				LocalFileID: "lf-loopback",
				Path:        sourcePath,
				DisplayName: "loopback.bin",
				Size:        sourceInfo.Size(),
				ModifiedAt:  sourceInfo.ModTime().UTC(),
				ExpiresAt:   time.Now().Add(time.Hour),
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	snapshot, err := senderService.SendAcceleratedFile(ctx, receiverDeviceID, "lf-loopback")
	if err != nil {
		t.Fatalf("SendAcceleratedFile() error = %v", err)
	}
	if snapshot.State != transfer.StateDone {
		t.Fatalf("expected done snapshot, got %#v", snapshot)
	}
	if transport.sendFileCalls != 0 {
		t.Fatalf("expected no standard fallback during loopback integration, got %d fallback calls", transport.sendFileCalls)
	}
	if transport.prepareCalls != 1 || transport.completeCalls != 1 {
		t.Fatalf("expected one prepare and one complete call, got prepare=%d complete=%d", transport.prepareCalls, transport.completeCalls)
	}

	files, err := filepath.Glob(filepath.Join(receiverCfg.DefaultDownloadDir, "loopback*.bin"))
	if err != nil {
		t.Fatalf("glob receiver files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one committed receiver file, got %#v", files)
	}
	receivedInfo, err := os.Stat(files[0])
	if err != nil {
		t.Fatalf("stat receiver file: %v", err)
	}
	if receivedInfo.Size() != sourceInfo.Size() {
		t.Fatalf("expected receiver file size %d, got %d", sourceInfo.Size(), receivedInfo.Size())
	}
}

type capturingAcceleratedRegistrar struct {
	registration transfer.AcceleratedSessionRegistration
}

func (c *capturingAcceleratedRegistrar) Register(registration transfer.AcceleratedSessionRegistration) {
	c.registration = registration
}

type fakeAcceleratedPeerTransport struct {
	*fakePeerTransport
	preparePeer      discovery.PeerRecord
	prepareRequest   protocol.AcceleratedPrepareRequest
	prepareResponse  protocol.AcceleratedPrepareResponse
	prepareErr       error
	filePeer         discovery.PeerRecord
	fileRequest      protocol.FileTransferRequest
	completePeer     discovery.PeerRecord
	completeRequest  protocol.AcceleratedCompleteRequest
	completeResponse protocol.AcceleratedCompleteResponse
	completeErr      error
}

func (f *fakeAcceleratedPeerTransport) PrepareAcceleratedTransfer(
	_ context.Context,
	peer discovery.PeerRecord,
	request protocol.AcceleratedPrepareRequest,
) (protocol.AcceleratedPrepareResponse, error) {
	f.preparePeer = peer
	f.prepareRequest = request
	if f.prepareErr != nil {
		return protocol.AcceleratedPrepareResponse{}, f.prepareErr
	}
	return f.prepareResponse, nil
}

func (f *fakeAcceleratedPeerTransport) CompleteAcceleratedTransfer(
	_ context.Context,
	peer discovery.PeerRecord,
	request protocol.AcceleratedCompleteRequest,
) (protocol.AcceleratedCompleteResponse, error) {
	f.completePeer = peer
	f.completeRequest = request
	if f.completeErr != nil {
		return protocol.AcceleratedCompleteResponse{}, f.completeErr
	}
	return f.completeResponse, nil
}

func (f *fakeAcceleratedPeerTransport) SendFile(
	ctx context.Context,
	peer discovery.PeerRecord,
	request protocol.FileTransferRequest,
	content io.Reader,
) (protocol.FileTransferResponse, error) {
	f.filePeer = peer
	f.fileRequest = request
	if f.fakePeerTransport != nil {
		return f.fakePeerTransport.SendFile(ctx, peer, request, content)
	}
	_, _ = io.ReadAll(content)
	return protocol.FileTransferResponse{
		TransferID: request.TransferID,
		State:      transfer.StateDone,
	}, nil
}

type fakeAcceleratedSender struct {
	lastPrepare      protocol.AcceleratedPrepareResponse
	onChunkCommitted func(int64)
	committedBytes   int64
	sendErr          error
}

func (f *fakeAcceleratedSender) SetOnChunkCommitted(onChunkCommitted func(int64)) {
	f.onChunkCommitted = onChunkCommitted
}

func (f *fakeAcceleratedSender) Send(
	_ context.Context,
	_ io.ReaderAt,
	_ int64,
	prepare protocol.AcceleratedPrepareResponse,
) error {
	f.lastPrepare = prepare
	if f.committedBytes > 0 && f.onChunkCommitted != nil {
		f.onChunkCommitted(f.committedBytes)
	}
	return f.sendErr
}

type loopbackAcceleratedPeerTransport struct {
	*fakePeerTransport
	receiver      *RuntimeService
	prepareCalls  int
	completeCalls int
}

func (t *loopbackAcceleratedPeerTransport) PrepareAcceleratedTransfer(
	ctx context.Context,
	_ discovery.PeerRecord,
	request protocol.AcceleratedPrepareRequest,
) (protocol.AcceleratedPrepareResponse, error) {
	t.prepareCalls++
	return t.receiver.PrepareAcceleratedTransfer(ctx, request)
}

func (t *loopbackAcceleratedPeerTransport) CompleteAcceleratedTransfer(
	ctx context.Context,
	_ discovery.PeerRecord,
	request protocol.AcceleratedCompleteRequest,
) (protocol.AcceleratedCompleteResponse, error) {
	t.completeCalls++
	return t.receiver.CompleteAcceleratedTransfer(ctx, request)
}
