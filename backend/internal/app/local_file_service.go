package app

import (
	"context"
	"fmt"

	"message-share/backend/internal/localfile"
)

type LocalFileSnapshot struct {
	LocalFileID         string `json:"localFileId"`
	DisplayName         string `json:"displayName"`
	Size                int64  `json:"size"`
	AcceleratedEligible bool   `json:"acceleratedEligible"`
}

type LocalFileResolver interface {
	Pick(ctx context.Context) (localfile.Lease, error)
	RegisterPath(path string) (localfile.Lease, error)
	Resolve(localFileID string) (localfile.Lease, error)
}

func (s *RuntimeService) PickLocalFile(ctx context.Context) (LocalFileSnapshot, error) {
	if s.localFiles == nil {
		return LocalFileSnapshot{}, fmt.Errorf("local file picker not configured")
	}
	lease, err := s.localFiles.Pick(ctx)
	if err != nil {
		return LocalFileSnapshot{}, err
	}
	return snapshotLocalFileLease(lease), nil
}

func (s *RuntimeService) RegisterLocalFile(_ context.Context, path string) (LocalFileSnapshot, error) {
	if s.localFiles == nil {
		return LocalFileSnapshot{}, fmt.Errorf("local file picker not configured")
	}
	lease, err := s.localFiles.RegisterPath(path)
	if err != nil {
		return LocalFileSnapshot{}, err
	}
	return snapshotLocalFileLease(lease), nil
}

func snapshotLocalFileLease(lease localfile.Lease) LocalFileSnapshot {
	return LocalFileSnapshot{
		LocalFileID:         lease.LocalFileID,
		DisplayName:         lease.DisplayName,
		Size:                lease.Size,
		AcceleratedEligible: lease.Size >= multipartThreshold,
	}
}
