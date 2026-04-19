package app

import (
	"context"
	"errors"
	"testing"

	"message-share/backend/internal/localfile"
)

type fakeLocalFileResolver struct {
	pickLease       localfile.Lease
	pickErr         error
	registerLease   localfile.Lease
	registerErr     error
	registerPath    string
	resolveLease    localfile.Lease
	resolveErr      error
}

func (f fakeLocalFileResolver) Pick(context.Context) (localfile.Lease, error) {
	if f.pickErr != nil {
		return localfile.Lease{}, f.pickErr
	}
	return f.pickLease, nil
}

func (f fakeLocalFileResolver) RegisterPath(path string) (localfile.Lease, error) {
	if f.registerErr != nil {
		return localfile.Lease{}, f.registerErr
	}
	lease := f.registerLease
	lease.Path = path
	return lease, nil
}

func (f fakeLocalFileResolver) Resolve(localFileID string) (localfile.Lease, error) {
	if f.resolveErr != nil {
		return localfile.Lease{}, f.resolveErr
	}
	lease := f.resolveLease
	lease.LocalFileID = localFileID
	return lease, nil
}

func TestPickLocalFileReturnsSafeSnapshot(t *testing.T) {
	service := NewRuntimeService(RuntimeDeps{
		LocalFiles: fakeLocalFileResolver{
			pickLease: localfile.Lease{
				LocalFileID: "lf-1",
				DisplayName: "demo.bin",
				Size:        multipartThreshold + 1,
			},
		},
	})

	snapshot, err := service.PickLocalFile(context.Background())
	if err != nil {
		t.Fatalf("PickLocalFile() error = %v", err)
	}
	if snapshot.LocalFileID != "lf-1" || snapshot.DisplayName != "demo.bin" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if !snapshot.AcceleratedEligible {
		t.Fatalf("expected accelerated eligibility for file size above threshold")
	}
}

func TestPickLocalFileRejectsMissingResolver(t *testing.T) {
	service := NewRuntimeService(RuntimeDeps{})
	if _, err := service.PickLocalFile(context.Background()); err == nil {
		t.Fatalf("expected missing resolver error")
	}
}

func TestPickLocalFilePropagatesResolverError(t *testing.T) {
	service := NewRuntimeService(RuntimeDeps{
		LocalFiles: fakeLocalFileResolver{pickErr: errors.New("picker failed")},
	})

	if _, err := service.PickLocalFile(context.Background()); err == nil {
		t.Fatalf("expected PickLocalFile to propagate resolver error")
	}
}

func TestRegisterLocalFileReturnsSafeSnapshot(t *testing.T) {
	service := NewRuntimeService(RuntimeDeps{
		LocalFiles: fakeLocalFileResolver{
			registerLease: localfile.Lease{
				LocalFileID: "lf-2",
				DisplayName: "clip.mp4",
				Size:        multipartThreshold + 1,
			},
		},
	})

	snapshot, err := service.RegisterLocalFile(context.Background(), `C:\tmp\clip.mp4`)
	if err != nil {
		t.Fatalf("RegisterLocalFile() error = %v", err)
	}
	if snapshot.LocalFileID != "lf-2" || snapshot.DisplayName != "clip.mp4" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if !snapshot.AcceleratedEligible {
		t.Fatalf("expected accelerated eligibility for file size above threshold")
	}
}

func TestRegisterLocalFileRejectsMissingResolver(t *testing.T) {
	service := NewRuntimeService(RuntimeDeps{})
	if _, err := service.RegisterLocalFile(context.Background(), `C:\tmp\clip.mp4`); err == nil {
		t.Fatalf("expected missing resolver error")
	}
}

func TestRegisterLocalFilePropagatesResolverError(t *testing.T) {
	service := NewRuntimeService(RuntimeDeps{
		LocalFiles: fakeLocalFileResolver{registerErr: errors.New("register failed")},
	})

	if _, err := service.RegisterLocalFile(context.Background(), `C:\tmp\clip.mp4`); err == nil {
		t.Fatalf("expected RegisterLocalFile to propagate resolver error")
	}
}
