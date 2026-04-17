package app

import (
	"context"
	"fmt"
	"io"
)

type StaticService struct {
	snapshot BootstrapSnapshot
}

func NewStaticService(snapshot BootstrapSnapshot) Service {
	return StaticService{snapshot: snapshot}
}

func (s StaticService) Bootstrap() (BootstrapSnapshot, error) {
	return s.snapshot, nil
}

func (s StaticService) StartPairing(_ context.Context, _ string) (PairingSnapshot, error) {
	return PairingSnapshot{}, fmt.Errorf("pairing not supported by static service")
}

func (s StaticService) ConfirmPairing(_ context.Context, _ string) (PairingSnapshot, error) {
	return PairingSnapshot{}, fmt.Errorf("pairing not supported by static service")
}

func (s StaticService) SendTextMessage(_ context.Context, _ string, _ string) (MessageSnapshot, error) {
	return MessageSnapshot{}, fmt.Errorf("text messaging not supported by static service")
}

func (s StaticService) SendFile(_ context.Context, _ string, _ string, _ int64, _ io.Reader) (TransferSnapshot, error) {
	return TransferSnapshot{}, fmt.Errorf("file transfer not supported by static service")
}

func (s StaticService) PickLocalFile(_ context.Context) (LocalFileSnapshot, error) {
	return LocalFileSnapshot{}, fmt.Errorf("local file picking not supported by static service")
}

func (s StaticService) SendAcceleratedFile(_ context.Context, _ string, _ string) (TransferSnapshot, error) {
	return TransferSnapshot{}, fmt.Errorf("accelerated file transfer not supported by static service")
}
