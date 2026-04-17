package app

import (
	"context"
	"errors"
	"testing"

	"message-share/backend/internal/localfile"
)

type fakeLocalFileResolver struct {
	pickLease    localfile.Lease
	pickErr      error
	resolveLease localfile.Lease
	resolveErr   error
}

func (f fakeLocalFileResolver) Pick(context.Context) (localfile.Lease, error) {
	if f.pickErr != nil {
		return localfile.Lease{}, f.pickErr
	}
	return f.pickLease, nil
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
