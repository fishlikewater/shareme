package desktop

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"message-share/backend/internal/app"
	"message-share/backend/internal/localfile"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type RuntimeCommands interface {
	Bootstrap() (app.BootstrapSnapshot, error)
	StartPairing(ctx context.Context, peerDeviceID string) (app.PairingSnapshot, error)
	ConfirmPairing(ctx context.Context, pairingID string) (app.PairingSnapshot, error)
	SendTextMessage(ctx context.Context, peerDeviceID string, body string) (app.MessageSnapshot, error)
	SendFile(ctx context.Context, peerDeviceID string, fileName string, fileSize int64, content io.Reader) (app.TransferSnapshot, error)
	RegisterLocalFile(ctx context.Context, path string) (app.LocalFileSnapshot, error)
	SendAcceleratedFile(ctx context.Context, peerDeviceID string, localFileID string) (app.TransferSnapshot, error)
	ListMessageHistory(ctx context.Context, conversationID string, beforeCursor string) (app.MessageHistoryPageSnapshot, error)
}

type ServiceResolver func() RuntimeCommands

type Dialogs interface {
	OpenFile(ctx context.Context) (string, error)
}

type WailsDialogs struct{}

func (WailsDialogs) OpenFile(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("desktop context not initialized")
	}
	return wailsruntime.OpenFileDialog(ctx, wailsruntime.OpenDialogOptions{})
}

type Bridge struct {
	resolveService ServiceResolver
	dialogs        Dialogs
}

func NewBridge(resolveService ServiceResolver, dialogs Dialogs) *Bridge {
	if dialogs == nil {
		dialogs = WailsDialogs{}
	}
	return &Bridge{
		resolveService: resolveService,
		dialogs:        dialogs,
	}
}

func (b *Bridge) Bootstrap(_ context.Context) (app.BootstrapSnapshot, error) {
	service, err := b.service()
	if err != nil {
		return app.BootstrapSnapshot{}, err
	}
	return service.Bootstrap()
}

func (b *Bridge) StartPairing(ctx context.Context, peerDeviceID string) (app.PairingSnapshot, error) {
	service, err := b.service()
	if err != nil {
		return app.PairingSnapshot{}, err
	}
	return service.StartPairing(ctx, peerDeviceID)
}

func (b *Bridge) ConfirmPairing(ctx context.Context, pairingID string) (app.PairingSnapshot, error) {
	service, err := b.service()
	if err != nil {
		return app.PairingSnapshot{}, err
	}
	return service.ConfirmPairing(ctx, pairingID)
}

func (b *Bridge) SendText(ctx context.Context, peerDeviceID string, body string) (app.MessageSnapshot, error) {
	service, err := b.service()
	if err != nil {
		return app.MessageSnapshot{}, err
	}
	return service.SendTextMessage(ctx, peerDeviceID, body)
}

func (b *Bridge) SendFile(ctx context.Context, peerDeviceID string) (app.TransferSnapshot, error) {
	service, err := b.service()
	if err != nil {
		return app.TransferSnapshot{}, err
	}

	path, err := b.openFilePath(ctx)
	if err != nil {
		return app.TransferSnapshot{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return app.TransferSnapshot{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return app.TransferSnapshot{}, err
	}
	return service.SendFile(ctx, peerDeviceID, info.Name(), info.Size(), file)
}

func (b *Bridge) PickLocalFile(ctx context.Context) (app.LocalFileSnapshot, error) {
	service, err := b.service()
	if err != nil {
		return app.LocalFileSnapshot{}, err
	}

	path, err := b.openFilePath(ctx)
	if err != nil {
		return app.LocalFileSnapshot{}, err
	}
	return service.RegisterLocalFile(ctx, path)
}

func (b *Bridge) SendAcceleratedFile(ctx context.Context, peerDeviceID string, localFileID string) (app.TransferSnapshot, error) {
	service, err := b.service()
	if err != nil {
		return app.TransferSnapshot{}, err
	}
	return service.SendAcceleratedFile(ctx, peerDeviceID, localFileID)
}

func (b *Bridge) ListMessageHistory(ctx context.Context, conversationID string, beforeCursor string) (app.MessageHistoryPageSnapshot, error) {
	service, err := b.service()
	if err != nil {
		return app.MessageHistoryPageSnapshot{}, err
	}
	return service.ListMessageHistory(ctx, conversationID, beforeCursor)
}

func (b *Bridge) service() (RuntimeCommands, error) {
	if b.resolveService == nil {
		return nil, fmt.Errorf("runtime host not started")
	}
	service := b.resolveService()
	if service == nil {
		return nil, fmt.Errorf("runtime host not started")
	}
	return service, nil
}

func (b *Bridge) openFilePath(ctx context.Context) (string, error) {
	path, err := b.dialogs.OpenFile(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", localfile.ErrPickerCancelled
	}
	return path, nil
}
