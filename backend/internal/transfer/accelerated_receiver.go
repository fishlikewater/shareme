package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"sync"
)

type acceleratedWindow struct {
	offset int64
	length int64
}

type AcceleratedReceiver struct {
	dir       string
	fileName  string
	totalSize int64
	chunkSize int64

	tempPath string
	file     *os.File

	mu               sync.Mutex
	windows          map[int64]int64
	inFlight         map[int64]bool
	coverage         []bool
	bytesWritten     int64
	onFrameCommitted func(int64)
}

func NewAcceleratedReceiver(dir string, fileName string, totalSize int64, chunkSize int64) (*AcceleratedReceiver, error) {
	if totalSize <= 0 {
		return nil, fmt.Errorf("invalid total size: %d", totalSize)
	}
	if chunkSize <= 0 {
		return nil, fmt.Errorf("invalid chunk size: %d", chunkSize)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	safeFileName := sanitizeFileName(fileName)
	file, err := os.CreateTemp(dir, safeFileName+".*.part")
	if err != nil {
		return nil, err
	}
	if err := file.Truncate(totalSize); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, err
	}

	coverageSize := int((totalSize + chunkSize - 1) / chunkSize)
	return &AcceleratedReceiver{
		dir:       dir,
		fileName:  safeFileName,
		totalSize: totalSize,
		chunkSize: chunkSize,
		tempPath:  file.Name(),
		file:      file,
		windows:   make(map[int64]int64),
		inFlight:  make(map[int64]bool),
		coverage:  make([]bool, coverageSize),
	}, nil
}

func (r *AcceleratedReceiver) SetOnFrameCommitted(onFrameCommitted func(int64)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onFrameCommitted = onFrameCommitted
}

func (r *AcceleratedReceiver) ReceiveFrame(offset int64, payload []byte) (int64, error) {
	if offset < 0 {
		return 0, fmt.Errorf("invalid frame offset: %d", offset)
	}
	if len(payload) == 0 {
		return 0, fmt.Errorf("invalid frame payload length: %d", len(payload))
	}
	length := int64(len(payload))
	if offset+length > r.totalSize {
		return 0, fmt.Errorf("frame exceeds total size: offset=%d length=%d total=%d", offset, length, r.totalSize)
	}

	r.mu.Lock()
	if r.file == nil {
		r.mu.Unlock()
		return 0, fmt.Errorf("accelerated receiver already closed")
	}
	if completedLength, exists := r.windows[offset]; exists {
		r.mu.Unlock()
		if completedLength != length {
			return 0, fmt.Errorf("frame length mismatch for offset %d: have=%d want=%d", offset, completedLength, length)
		}
		return completedLength, nil
	}
	if r.inFlight[offset] {
		r.mu.Unlock()
		return 0, fmt.Errorf("frame already in progress at offset %d", offset)
	}
	r.inFlight[offset] = true
	file := r.file
	r.mu.Unlock()

	written, err := file.WriteAt(payload, offset)

	var onFrameCommitted func(int64)
	r.mu.Lock()
	delete(r.inFlight, offset)
	if err == nil {
		if int64(written) != length {
			r.mu.Unlock()
			return int64(written), io.ErrShortWrite
		}
		r.windows[offset] = length
		r.bytesWritten += length
		r.markCovered(offset, length)
		onFrameCommitted = r.onFrameCommitted
	}
	r.mu.Unlock()
	if err == nil && onFrameCommitted != nil {
		onFrameCommitted(length)
	}

	return int64(written), err
}

func (r *AcceleratedReceiver) ServeLane(_ context.Context, _ int, conn net.Conn) error {
	for {
		frame, err := ReadAcceleratedDataFrame(conn)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if _, err := r.ReceiveFrame(frame.Offset, frame.Payload); err != nil {
			return err
		}
	}
}

func (r *AcceleratedReceiver) BytesReceived() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bytesWritten
}

func (r *AcceleratedReceiver) Complete(expectedSHA256 string) (string, error) {
	r.mu.Lock()
	if len(r.inFlight) > 0 {
		r.mu.Unlock()
		return "", fmt.Errorf("frames still in progress")
	}
	parts := make([]acceleratedWindow, 0, len(r.windows))
	for offset, length := range r.windows {
		parts = append(parts, acceleratedWindow{offset: offset, length: length})
	}
	coverage := append([]bool(nil), r.coverage...)
	file := r.file
	tempPath := r.tempPath
	r.mu.Unlock()

	sort.Slice(parts, func(i int, j int) bool {
		return parts[i].offset < parts[j].offset
	})

	nextOffset := int64(0)
	for _, part := range parts {
		if part.offset != nextOffset {
			return "", fmt.Errorf("frame coverage mismatch at offset %d", nextOffset)
		}
		nextOffset += part.length
	}
	if nextOffset != r.totalSize {
		return "", fmt.Errorf("written bytes mismatch: have=%d want=%d", nextOffset, r.totalSize)
	}
	for index, covered := range coverage {
		if !covered {
			return "", fmt.Errorf("coverage bitmap incomplete at chunk %d", index)
		}
	}

	if expectedSHA256 == "" {
		return "", fmt.Errorf("file sha256 required")
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	actualHash, err := acceleratedFileSHA256Hex(tempPath)
	if err != nil {
		return "", err
	}
	if actualHash != expectedSHA256 {
		return "", fmt.Errorf("file sha256 mismatch")
	}
	if err := file.Close(); err != nil {
		return "", err
	}

	finalPath, err := nextAvailablePath(r.dir, r.fileName)
	if err != nil {
		return "", err
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return "", err
	}

	r.mu.Lock()
	r.file = nil
	r.tempPath = ""
	r.mu.Unlock()

	return finalPath, nil
}

func (r *AcceleratedReceiver) Cleanup() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}
	if r.tempPath != "" {
		_ = os.Remove(r.tempPath)
		r.tempPath = ""
	}
	return nil
}

func (r *AcceleratedReceiver) markCovered(offset int64, length int64) {
	start := int(offset / r.chunkSize)
	end := int((offset + length - 1) / r.chunkSize)
	if start < 0 {
		start = 0
	}
	if end >= len(r.coverage) {
		end = len(r.coverage) - 1
	}
	for index := start; index <= end; index++ {
		r.coverage[index] = true
	}
}

func acceleratedFileSHA256Hex(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
