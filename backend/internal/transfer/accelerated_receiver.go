package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

	tempPath   string
	file       *os.File
	closing    bool
	finalizing bool

	mu               sync.Mutex
	idle             *sync.Cond
	windows          map[int64]int64
	acked            map[int64]int64
	inFlight         map[int64]bool
	coverage         []bool
	bytesWritten     int64
	onFrameCommitted func(int64)
}

var (
	errAcceleratedReceiverClosed = errors.New("accelerated receiver already closed")
	acceleratedReceiverSyncFile  = func(file *os.File) error { return file.Sync() }
	acceleratedReceiverHashFile  = acceleratedFileSHA256Hex
	acceleratedReceiverWriteAt   = func(file *os.File, payload []byte, offset int64) (int, error) {
		return file.WriteAt(payload, offset)
	}
)

func NewAcceleratedReceiver(dir string, fileName string, totalSize int64, chunkSize int64) (*AcceleratedReceiver, error) {
	if totalSize <= 0 {
		return nil, fmt.Errorf("invalid total size: %d", totalSize)
	}
	if chunkSize <= 0 {
		return nil, fmt.Errorf("invalid chunk size: %d", chunkSize)
	}
	file, safeFileName, tempPath, err := createStagedDownloadFile(dir, fileName, totalSize)
	if err != nil {
		return nil, err
	}

	coverageSize := int((totalSize + chunkSize - 1) / chunkSize)
	receiver := &AcceleratedReceiver{
		dir:       dir,
		fileName:  safeFileName,
		totalSize: totalSize,
		chunkSize: chunkSize,
		tempPath:  tempPath,
		file:      file,
		windows:   make(map[int64]int64),
		acked:     make(map[int64]int64),
		inFlight:  make(map[int64]bool),
		coverage:  make([]bool, coverageSize),
	}
	receiver.idle = sync.NewCond(&receiver.mu)
	return receiver, nil
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
	if r.file == nil || r.finalizing || r.closing {
		r.mu.Unlock()
		return 0, errAcceleratedReceiverClosed
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

	written, err := acceleratedReceiverWriteAt(file, payload, offset)

	var onFrameCommitted func(int64)
	r.mu.Lock()
	delete(r.inFlight, offset)
	if len(r.inFlight) == 0 && r.idle != nil {
		r.idle.Broadcast()
	}
	if err == nil {
		if int64(written) != length {
			r.mu.Unlock()
			return int64(written), io.ErrShortWrite
		}
		if r.closing {
			r.mu.Unlock()
			return int64(written), errAcceleratedReceiverClosed
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
		written, err := r.ReceiveFrame(frame.Offset, frame.Payload)
		if err != nil {
			return err
		}
		if err := r.AcknowledgeFrame(frame.Offset, written); err != nil {
			return err
		}
		if err := WriteAcceleratedAckFrame(conn, AcceleratedAckFrame{
			Offset: frame.Offset,
			Length: written,
		}); err != nil {
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
	if r.file == nil || r.finalizing || r.closing {
		r.mu.Unlock()
		return "", errAcceleratedReceiverClosed
	}
	if len(r.inFlight) > 0 {
		r.mu.Unlock()
		return "", fmt.Errorf("frames still in progress")
	}
	parts := make([]acceleratedWindow, 0, len(r.windows))
	for offset, length := range r.windows {
		parts = append(parts, acceleratedWindow{offset: offset, length: length})
	}
	acked := make(map[int64]int64, len(r.acked))
	for offset, length := range r.acked {
		acked[offset] = length
	}
	coverage := append([]bool(nil), r.coverage...)

	sort.Slice(parts, func(i int, j int) bool {
		return parts[i].offset < parts[j].offset
	})

	nextOffset := int64(0)
	for _, part := range parts {
		if part.offset != nextOffset {
			r.mu.Unlock()
			return "", fmt.Errorf("frame coverage mismatch at offset %d", nextOffset)
		}
		nextOffset += part.length
	}
	if nextOffset != r.totalSize {
		r.mu.Unlock()
		return "", fmt.Errorf("written bytes mismatch: have=%d want=%d", nextOffset, r.totalSize)
	}
	if len(acked) != len(parts) {
		r.mu.Unlock()
		return "", fmt.Errorf("frame ack incomplete: have=%d want=%d", len(acked), len(parts))
	}
	for _, part := range parts {
		ackedLength, ok := acked[part.offset]
		if !ok || ackedLength != part.length {
			r.mu.Unlock()
			return "", fmt.Errorf("frame ack mismatch at offset %d", part.offset)
		}
	}
	for index, covered := range coverage {
		if !covered {
			r.mu.Unlock()
			return "", fmt.Errorf("coverage bitmap incomplete at chunk %d", index)
		}
	}
	r.finalizing = true
	file := r.file
	tempPath := r.tempPath
	r.file = nil
	r.mu.Unlock()

	finalized := false
	defer func() {
		r.mu.Lock()
		r.finalizing = false
		if finalized {
			r.tempPath = ""
		} else {
			r.tempPath = tempPath
		}
		r.mu.Unlock()
	}()

	if expectedSHA256 == "" {
		_ = file.Close()
		return "", fmt.Errorf("file sha256 required")
	}
	if err := acceleratedReceiverSyncFile(file); err != nil {
		_ = file.Close()
		return "", err
	}
	actualHash, err := acceleratedReceiverHashFile(tempPath)
	if err != nil {
		_ = file.Close()
		return "", err
	}
	if actualHash != expectedSHA256 {
		_ = file.Close()
		return "", fmt.Errorf("file sha256 mismatch")
	}
	finalPath, err := commitStagedDownloadFile(file, tempPath, r.dir, r.fileName)
	if err != nil {
		return "", err
	}
	finalized = true
	return finalPath, nil
}

func (r *AcceleratedReceiver) Cleanup() error {
	r.mu.Lock()
	if r.finalizing {
		r.mu.Unlock()
		return errAcceleratedReceiverClosed
	}
	r.closing = true
	for len(r.inFlight) > 0 {
		r.idle.Wait()
	}
	file := r.file
	tempPath := r.tempPath
	r.file = nil
	r.tempPath = ""
	r.closing = false
	r.mu.Unlock()

	if file != nil {
		_ = file.Close()
	}
	if tempPath != "" {
		_ = os.Remove(tempPath)
	}
	return nil
}

func (r *AcceleratedReceiver) AcknowledgeFrame(offset int64, length int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.file == nil || r.finalizing || r.closing {
		return errAcceleratedReceiverClosed
	}
	writtenLength, ok := r.windows[offset]
	if !ok {
		return fmt.Errorf("frame not written at offset %d", offset)
	}
	if writtenLength != length {
		return fmt.Errorf("frame ack length mismatch for offset %d: have=%d want=%d", offset, length, writtenLength)
	}
	r.acked[offset] = length
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
