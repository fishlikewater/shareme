package localfile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"shareme/backend/internal/localfile"
)

func TestManagerPickCreatesLeaseWithoutExposingPath(t *testing.T) {
	fakePicker := localfile.PickerFunc(func(context.Context) (localfile.PickedFile, error) {
		return localfile.PickedFile{
			Path:        `C:\tmp\demo.bin`,
			DisplayName: "demo.bin",
			Size:        128,
			ModifiedAt:  time.Unix(1700000000, 0).UTC(),
		}, nil
	})

	manager := localfile.NewManager(fakePicker, 10*time.Minute, func() time.Time {
		return time.Unix(1700000100, 0).UTC()
	})

	lease, err := manager.Pick(context.Background())
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if lease.LocalFileID == "" {
		t.Fatalf("expected LocalFileID to be set")
	}
	if lease.Path != "" {
		t.Fatalf("expected safe snapshot without path, got %q", lease.Path)
	}
}

func TestManagerResolveRejectsExpiredLease(t *testing.T) {
	manager := localfile.NewManager(localfile.PickerFunc(func(context.Context) (localfile.PickedFile, error) {
		return localfile.PickedFile{
			Path:        `C:\tmp\demo.bin`,
			DisplayName: "demo.bin",
			Size:        64,
			ModifiedAt:  time.Unix(1700000000, 0).UTC(),
		}, nil
	}), time.Minute, func() time.Time {
		return time.Unix(1700000000, 0).UTC()
	})

	lease, err := manager.Pick(context.Background())
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}

	manager.SetNow(func() time.Time {
		return time.Unix(1700003600, 0).UTC()
	})

	if _, err := manager.Resolve(lease.LocalFileID); err == nil {
		t.Fatalf("expected Resolve to reject expired lease")
	}
}

func TestManagerResolveRejectsChangedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.bin")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	manager := localfile.NewManager(localfile.PickerFunc(func(context.Context) (localfile.PickedFile, error) {
		return localfile.PickedFile{
			Path:        path,
			DisplayName: "demo.bin",
			Size:        info.Size(),
			ModifiedAt:  info.ModTime().UTC(),
		}, nil
	}), 10*time.Minute, time.Now)

	lease, err := manager.Pick(context.Background())
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}

	if err := os.WriteFile(path, []byte("newer-content"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := manager.Resolve(lease.LocalFileID); err == nil {
		t.Fatalf("expected Resolve to reject changed file")
	}
}

func TestManagerRegisterPathCreatesLeaseWithoutPicker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.bin")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	manager := localfile.NewManager(nil, 10*time.Minute, func() time.Time {
		return time.Unix(1700000100, 0).UTC()
	})

	lease, err := manager.RegisterPath(path)
	if err != nil {
		t.Fatalf("RegisterPath() error = %v", err)
	}
	if lease.LocalFileID == "" {
		t.Fatalf("expected LocalFileID to be set")
	}
	if lease.Path != "" {
		t.Fatalf("expected safe snapshot without path, got %q", lease.Path)
	}
	if lease.DisplayName != "demo.bin" {
		t.Fatalf("unexpected display name: %q", lease.DisplayName)
	}
	if lease.Size != info.Size() {
		t.Fatalf("unexpected size: %d", lease.Size)
	}
}
