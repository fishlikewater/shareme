package localfile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const DefaultLeaseTTL = 15 * time.Minute

type Manager struct {
	mu     sync.RWMutex
	picker Picker
	ttl    time.Duration
	now    func() time.Time
	leases map[string]Lease
}

func NewManager(picker Picker, ttl time.Duration, now func() time.Time) *Manager {
	if ttl <= 0 {
		ttl = DefaultLeaseTTL
	}
	if now == nil {
		now = time.Now
	}
	return &Manager{
		picker: picker,
		ttl:    ttl,
		now:    now,
		leases: make(map[string]Lease),
	}
}

func (m *Manager) SetNow(now func() time.Time) {
	if now == nil {
		now = time.Now
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = now
}

func (m *Manager) Pick(ctx context.Context) (Lease, error) {
	if m.picker == nil {
		return Lease{}, fmt.Errorf("local file picker not configured")
	}
	picked, err := m.picker.Pick(ctx)
	if err != nil {
		return Lease{}, err
	}
	return m.registerPickedFile(picked)
}

func (m *Manager) RegisterPath(path string) (Lease, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Lease{}, fmt.Errorf("stat local file: %w", err)
	}

	return m.registerPickedFile(PickedFile{
		Path:        path,
		DisplayName: filepath.Base(path),
		Size:        info.Size(),
		ModifiedAt:  info.ModTime().UTC(),
	})
}

func (m *Manager) registerPickedFile(picked PickedFile) (Lease, error) {
	if picked.Path == "" {
		return Lease{}, ErrLeaseInvalid
	}
	if picked.DisplayName == "" {
		picked.DisplayName = filepath.Base(picked.Path)
	}
	if picked.Size < 0 {
		return Lease{}, ErrLeaseInvalid
	}

	lease := Lease{
		LocalFileID: newLeaseID(),
		Path:        picked.Path,
		DisplayName: picked.DisplayName,
		Size:        picked.Size,
		ModifiedAt:  picked.ModifiedAt.UTC(),
		ExpiresAt:   m.now().UTC().Add(m.ttl),
	}

	m.mu.Lock()
	m.leases[lease.LocalFileID] = lease
	m.mu.Unlock()
	return lease.Snapshot(), nil
}

func (m *Manager) Resolve(localFileID string) (Lease, error) {
	m.mu.RLock()
	lease, ok := m.leases[localFileID]
	m.mu.RUnlock()
	if !ok {
		return Lease{}, ErrLeaseNotFound
	}
	if m.now().UTC().After(lease.ExpiresAt) {
		return Lease{}, ErrLeaseExpired
	}
	info, err := os.Stat(lease.Path)
	if err != nil {
		return Lease{}, ErrLeaseInvalid
	}
	if info.Size() != lease.Size || !info.ModTime().UTC().Equal(lease.ModifiedAt.UTC()) {
		return Lease{}, ErrLeaseInvalid
	}
	return lease, nil
}

func newLeaseID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("lf-%d", time.Now().UnixNano())
	}
	return "lf-" + hex.EncodeToString(raw[:])
}
