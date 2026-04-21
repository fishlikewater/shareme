package localui

import (
	"context"
	"fmt"
	"io"

	"message-share/backend/internal/api"
	appruntime "message-share/backend/internal/app"
)

type RuntimeCommands interface {
	Bootstrap() (appruntime.BootstrapSnapshot, error)
	StartPairing(ctx context.Context, peerDeviceID string) (appruntime.PairingSnapshot, error)
	ConfirmPairing(ctx context.Context, pairingID string) (appruntime.PairingSnapshot, error)
	SendTextMessage(ctx context.Context, peerDeviceID string, body string) (appruntime.MessageSnapshot, error)
	SendFile(ctx context.Context, peerDeviceID string, fileName string, fileSize int64, content io.Reader) (appruntime.TransferSnapshot, error)
	PickLocalFile(ctx context.Context) (appruntime.LocalFileSnapshot, error)
	SendAcceleratedFile(ctx context.Context, peerDeviceID string, localFileID string) (appruntime.TransferSnapshot, error)
	ListMessageHistory(ctx context.Context, conversationID string, beforeCursor string) (appruntime.MessageHistoryPageSnapshot, error)
}

type Service struct {
	resolveRuntime func() RuntimeCommands
	bus            *api.EventBus
}

func NewService(resolveRuntime func() RuntimeCommands, bus *api.EventBus) *Service {
	if bus == nil {
		bus = api.NewEventBus()
	}
	return &Service{
		resolveRuntime: resolveRuntime,
		bus:            bus,
	}
}

func (s *Service) Bootstrap() (appruntime.BootstrapSnapshot, error) {
	runtime, err := s.runtime()
	if err != nil {
		return appruntime.BootstrapSnapshot{}, err
	}
	return runtime.Bootstrap()
}

func (s *Service) StartPairing(ctx context.Context, peerDeviceID string) (appruntime.PairingSnapshot, error) {
	runtime, err := s.runtime()
	if err != nil {
		return appruntime.PairingSnapshot{}, err
	}
	return runtime.StartPairing(ctx, peerDeviceID)
}

func (s *Service) ConfirmPairing(ctx context.Context, pairingID string) (appruntime.PairingSnapshot, error) {
	runtime, err := s.runtime()
	if err != nil {
		return appruntime.PairingSnapshot{}, err
	}
	return runtime.ConfirmPairing(ctx, pairingID)
}

func (s *Service) SendText(ctx context.Context, peerDeviceID string, body string) (appruntime.MessageSnapshot, error) {
	runtime, err := s.runtime()
	if err != nil {
		return appruntime.MessageSnapshot{}, err
	}
	return runtime.SendTextMessage(ctx, peerDeviceID, body)
}

func (s *Service) SendFile(ctx context.Context, peerDeviceID string, fileName string, fileSize int64, content io.Reader) (appruntime.TransferSnapshot, error) {
	runtime, err := s.runtime()
	if err != nil {
		return appruntime.TransferSnapshot{}, err
	}
	return runtime.SendFile(ctx, peerDeviceID, fileName, fileSize, content)
}

func (s *Service) PickLocalFile(ctx context.Context) (appruntime.LocalFileSnapshot, error) {
	runtime, err := s.runtime()
	if err != nil {
		return appruntime.LocalFileSnapshot{}, err
	}
	return runtime.PickLocalFile(ctx)
}

func (s *Service) SendAcceleratedFile(ctx context.Context, peerDeviceID string, localFileID string) (appruntime.TransferSnapshot, error) {
	runtime, err := s.runtime()
	if err != nil {
		return appruntime.TransferSnapshot{}, err
	}
	return runtime.SendAcceleratedFile(ctx, peerDeviceID, localFileID)
}

func (s *Service) ListMessageHistory(ctx context.Context, conversationID string, beforeCursor string) (appruntime.MessageHistoryPageSnapshot, error) {
	runtime, err := s.runtime()
	if err != nil {
		return appruntime.MessageHistoryPageSnapshot{}, err
	}
	return runtime.ListMessageHistory(ctx, conversationID, beforeCursor)
}

func (s *Service) EventSeq() int64 {
	if s == nil || s.bus == nil {
		return 0
	}
	return s.bus.LastSeq()
}

func (s *Service) Subscribe(afterSeq int64) ([]api.Event, <-chan api.Event, func()) {
	if s == nil || s.bus == nil {
		stream := make(chan api.Event)
		close(stream)
		return nil, stream, func() {}
	}
	return s.bus.Subscribe(afterSeq)
}

func (s *Service) runtime() (RuntimeCommands, error) {
	if s == nil || s.resolveRuntime == nil {
		return nil, fmt.Errorf("runtime host not started")
	}
	runtime := s.resolveRuntime()
	if runtime == nil {
		return nil, fmt.Errorf("runtime host not started")
	}
	return runtime, nil
}
