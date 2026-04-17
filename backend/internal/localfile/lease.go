package localfile

import (
	"errors"
	"time"
)

var (
	ErrPickerCancelled = errors.New("local file picker cancelled")
	ErrLeaseNotFound   = errors.New("local file lease not found")
	ErrLeaseExpired    = errors.New("local file lease expired")
	ErrLeaseInvalid    = errors.New("local file lease invalid")
)

type PickedFile struct {
	Path        string
	DisplayName string
	Size        int64
	ModifiedAt  time.Time
}

type Lease struct {
	LocalFileID string
	Path        string
	DisplayName string
	Size        int64
	ModifiedAt  time.Time
	ExpiresAt   time.Time
}

func (l Lease) Snapshot() Lease {
	l.Path = ""
	return l
}
