package desktop

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"message-share/backend/internal/app"
	"message-share/backend/internal/localfile"
)

type fakeRuntimeCommands struct {
	bootstrapFunc          func() (app.BootstrapSnapshot, error)
	startPairingFunc       func(context.Context, string) (app.PairingSnapshot, error)
	confirmPairingFunc     func(context.Context, string) (app.PairingSnapshot, error)
	sendTextFunc           func(context.Context, string, string) (app.MessageSnapshot, error)
	sendFileFunc           func(context.Context, string, string, int64, io.Reader) (app.TransferSnapshot, error)
	registerLocalFileFunc  func(context.Context, string) (app.LocalFileSnapshot, error)
	sendAcceleratedFunc    func(context.Context, string, string) (app.TransferSnapshot, error)
	listMessageHistoryFunc func(context.Context, string, string) (app.MessageHistoryPageSnapshot, error)
}

func (f *fakeRuntimeCommands) Bootstrap() (app.BootstrapSnapshot, error) {
	return f.bootstrapFunc()
}

func (f *fakeRuntimeCommands) StartPairing(ctx context.Context, peerDeviceID string) (app.PairingSnapshot, error) {
	return f.startPairingFunc(ctx, peerDeviceID)
}

func (f *fakeRuntimeCommands) ConfirmPairing(ctx context.Context, pairingID string) (app.PairingSnapshot, error) {
	return f.confirmPairingFunc(ctx, pairingID)
}

func (f *fakeRuntimeCommands) SendTextMessage(ctx context.Context, peerDeviceID string, body string) (app.MessageSnapshot, error) {
	return f.sendTextFunc(ctx, peerDeviceID, body)
}

func (f *fakeRuntimeCommands) SendFile(ctx context.Context, peerDeviceID string, fileName string, fileSize int64, content io.Reader) (app.TransferSnapshot, error) {
	return f.sendFileFunc(ctx, peerDeviceID, fileName, fileSize, content)
}

func (f *fakeRuntimeCommands) RegisterLocalFile(ctx context.Context, path string) (app.LocalFileSnapshot, error) {
	return f.registerLocalFileFunc(ctx, path)
}

func (f *fakeRuntimeCommands) SendAcceleratedFile(ctx context.Context, peerDeviceID string, localFileID string) (app.TransferSnapshot, error) {
	return f.sendAcceleratedFunc(ctx, peerDeviceID, localFileID)
}

func (f *fakeRuntimeCommands) ListMessageHistory(ctx context.Context, conversationID string, beforeCursor string) (app.MessageHistoryPageSnapshot, error) {
	return f.listMessageHistoryFunc(ctx, conversationID, beforeCursor)
}

type fakeDialogs struct {
	openFileResult string
	openFileErr    error
}

func (f fakeDialogs) OpenFile(context.Context) (string, error) {
	if f.openFileErr != nil {
		return "", f.openFileErr
	}
	return f.openFileResult, nil
}

func TestBridgePassThroughCommandsReturnSnapshots(t *testing.T) {
	ctx := context.Background()
	bridge := NewBridge(func() RuntimeCommands {
		return &fakeRuntimeCommands{
			bootstrapFunc: func() (app.BootstrapSnapshot, error) {
				return app.BootstrapSnapshot{LocalDeviceName: "office-pc"}, nil
			},
			startPairingFunc: func(_ context.Context, peerDeviceID string) (app.PairingSnapshot, error) {
				if peerDeviceID != "peer-1" {
					t.Fatalf("unexpected peer id: %s", peerDeviceID)
				}
				return app.PairingSnapshot{PairingID: "pair-1"}, nil
			},
			confirmPairingFunc: func(_ context.Context, pairingID string) (app.PairingSnapshot, error) {
				if pairingID != "pair-1" {
					t.Fatalf("unexpected pairing id: %s", pairingID)
				}
				return app.PairingSnapshot{PairingID: pairingID, Status: "confirmed"}, nil
			},
			sendTextFunc: func(_ context.Context, peerDeviceID string, body string) (app.MessageSnapshot, error) {
				if peerDeviceID != "peer-1" || body != "hello" {
					t.Fatalf("unexpected send text args: %s %s", peerDeviceID, body)
				}
				return app.MessageSnapshot{MessageID: "msg-1", Body: body}, nil
			},
			registerLocalFileFunc: func(_ context.Context, path string) (app.LocalFileSnapshot, error) {
				if path != "/tmp/demo.bin" {
					t.Fatalf("unexpected local file path: %s", path)
				}
				return app.LocalFileSnapshot{LocalFileID: "lf-1", DisplayName: "demo.bin"}, nil
			},
			sendAcceleratedFunc: func(_ context.Context, peerDeviceID string, localFileID string) (app.TransferSnapshot, error) {
				if peerDeviceID != "peer-1" || localFileID != "lf-1" {
					t.Fatalf("unexpected accelerated args: %s %s", peerDeviceID, localFileID)
				}
				return app.TransferSnapshot{TransferID: "tx-1"}, nil
			},
			listMessageHistoryFunc: func(_ context.Context, conversationID string, beforeCursor string) (app.MessageHistoryPageSnapshot, error) {
				if conversationID != "conv-1" || beforeCursor != "cursor-1" {
					t.Fatalf("unexpected history args: %s %s", conversationID, beforeCursor)
				}
				return app.MessageHistoryPageSnapshot{ConversationID: conversationID}, nil
			},
		}
	}, fakeDialogs{openFileResult: "/tmp/demo.bin"})

	if snapshot, err := bridge.Bootstrap(ctx); err != nil || snapshot.LocalDeviceName != "office-pc" {
		t.Fatalf("Bootstrap() = %#v, %v", snapshot, err)
	}
	if pairing, err := bridge.StartPairing(ctx, "peer-1"); err != nil || pairing.PairingID != "pair-1" {
		t.Fatalf("StartPairing() = %#v, %v", pairing, err)
	}
	if pairing, err := bridge.ConfirmPairing(ctx, "pair-1"); err != nil || pairing.Status != "confirmed" {
		t.Fatalf("ConfirmPairing() = %#v, %v", pairing, err)
	}
	if message, err := bridge.SendText(ctx, "peer-1", "hello"); err != nil || message.MessageID != "msg-1" {
		t.Fatalf("SendText() = %#v, %v", message, err)
	}
	if snapshot, err := bridge.PickLocalFile(ctx); err != nil || snapshot.LocalFileID != "lf-1" {
		t.Fatalf("PickLocalFile() = %#v, %v", snapshot, err)
	}
	if transfer, err := bridge.SendAcceleratedFile(ctx, "peer-1", "lf-1"); err != nil || transfer.TransferID != "tx-1" {
		t.Fatalf("SendAcceleratedFile() = %#v, %v", transfer, err)
	}
	if page, err := bridge.ListMessageHistory(ctx, "conv-1", "cursor-1"); err != nil || page.ConversationID != "conv-1" {
		t.Fatalf("ListMessageHistory() = %#v, %v", page, err)
	}
}

func TestBridgeSendFileUsesNativeDialogAndStreamsLocalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	bridge := NewBridge(func() RuntimeCommands {
		return &fakeRuntimeCommands{
			sendFileFunc: func(_ context.Context, peerDeviceID string, fileName string, fileSize int64, content io.Reader) (app.TransferSnapshot, error) {
				if peerDeviceID != "peer-1" {
					t.Fatalf("unexpected peer id: %s", peerDeviceID)
				}
				if fileName != "demo.txt" || fileSize != 5 {
					t.Fatalf("unexpected file metadata: %s %d", fileName, fileSize)
				}
				if _, ok := content.(*os.File); !ok {
					t.Fatalf("expected direct os.File reader, got %T", content)
				}
				body, err := io.ReadAll(content)
				if err != nil {
					t.Fatalf("ReadAll() error = %v", err)
				}
				if string(body) != "hello" {
					t.Fatalf("unexpected body: %q", string(body))
				}
				return app.TransferSnapshot{TransferID: "tx-file-1", FileName: fileName}, nil
			},
		}
	}, fakeDialogs{openFileResult: path})

	transfer, err := bridge.SendFile(context.Background(), "peer-1")
	if err != nil {
		t.Fatalf("SendFile() error = %v", err)
	}
	if transfer.TransferID != "tx-file-1" {
		t.Fatalf("unexpected transfer: %#v", transfer)
	}
}

func TestBridgePropagatesErrors(t *testing.T) {
	serviceErr := errors.New("send failed")
	bridge := NewBridge(func() RuntimeCommands {
		return &fakeRuntimeCommands{
			sendTextFunc: func(context.Context, string, string) (app.MessageSnapshot, error) {
				return app.MessageSnapshot{}, serviceErr
			},
		}
	}, fakeDialogs{})

	if _, err := bridge.SendText(context.Background(), "peer-1", "hello"); !errors.Is(err, serviceErr) {
		t.Fatalf("expected SendText() to propagate service error, got %v", err)
	}
}

func TestBridgeSendFilePropagatesDialogError(t *testing.T) {
	dialogErr := errors.New("dialog failed")
	bridge := NewBridge(func() RuntimeCommands {
		return &fakeRuntimeCommands{}
	}, fakeDialogs{openFileErr: dialogErr})

	if _, err := bridge.SendFile(context.Background(), "peer-1"); !errors.Is(err, dialogErr) {
		t.Fatalf("expected SendFile() to propagate dialog error, got %v", err)
	}
}

func TestBridgeSendFilePropagatesRuntimeError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	runtimeErr := errors.New("send failed")
	bridge := NewBridge(func() RuntimeCommands {
		return &fakeRuntimeCommands{
			sendFileFunc: func(context.Context, string, string, int64, io.Reader) (app.TransferSnapshot, error) {
				return app.TransferSnapshot{}, runtimeErr
			},
		}
	}, fakeDialogs{openFileResult: path})

	if _, err := bridge.SendFile(context.Background(), "peer-1"); !errors.Is(err, runtimeErr) {
		t.Fatalf("expected SendFile() to propagate runtime error, got %v", err)
	}
}

func TestBridgePickLocalFileTreatsEmptyPathAsCancelled(t *testing.T) {
	bridge := NewBridge(func() RuntimeCommands {
		return &fakeRuntimeCommands{}
	}, fakeDialogs{})

	if _, err := bridge.PickLocalFile(context.Background()); !errors.Is(err, localfile.ErrPickerCancelled) {
		t.Fatalf("expected PickLocalFile() to treat empty path as cancellation, got %v", err)
	}
}

func TestBridgePickLocalFilePropagatesRuntimeError(t *testing.T) {
	runtimeErr := errors.New("register failed")
	bridge := NewBridge(func() RuntimeCommands {
		return &fakeRuntimeCommands{
			registerLocalFileFunc: func(context.Context, string) (app.LocalFileSnapshot, error) {
				return app.LocalFileSnapshot{}, runtimeErr
			},
		}
	}, fakeDialogs{openFileResult: "/tmp/demo.bin"})

	if _, err := bridge.PickLocalFile(context.Background()); !errors.Is(err, runtimeErr) {
		t.Fatalf("expected PickLocalFile() to propagate runtime error, got %v", err)
	}
}
