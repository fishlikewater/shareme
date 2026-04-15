package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
)

type SessionReceiver struct {
	dir          string
	fileName     string
	totalSize    int64
	tempPath     string
	file         *os.File
	completed    map[int]CompletedPart
	inFlight     map[int]bool
	bytesWritten int64
	mu           sync.Mutex
}

var (
	ErrPartAlreadyCompleted   = errors.New("transfer part already completed")
	ErrPartAlreadyInProgress  = errors.New("transfer part already in progress")
	sessionReceiverHashFile   = fileSHA256Hex
	sessionReceiverSyncFile   = func(file *os.File) error { return file.Sync() }
	sessionReceiverBufferPool = sync.Pool{
		New: func() any {
			return make([]byte, sessionCopyBufferSize)
		},
	}
)

func NewSessionReceiver(dir string, fileName string, totalSize int64) (*SessionReceiver, error) {
	if totalSize <= 0 {
		return nil, fmt.Errorf("invalid total size: %d", totalSize)
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

	return &SessionReceiver{
		dir:       dir,
		fileName:  safeFileName,
		totalSize: totalSize,
		tempPath:  file.Name(),
		file:      file,
		completed: make(map[int]CompletedPart),
		inFlight:  make(map[int]bool),
	}, nil
}

func (r *SessionReceiver) WritePart(partIndex int, offset int64, length int64, content io.Reader) (int64, error) {
	if partIndex < 0 {
		return 0, fmt.Errorf("invalid part index: %d", partIndex)
	}
	if offset < 0 || length <= 0 {
		return 0, fmt.Errorf("invalid part window: offset=%d length=%d", offset, length)
	}
	if offset+length > r.totalSize {
		return 0, fmt.Errorf("part exceeds total size: offset=%d length=%d total=%d", offset, length, r.totalSize)
	}

	r.mu.Lock()
	if r.file == nil {
		r.mu.Unlock()
		return 0, fmt.Errorf("session receiver already closed")
	}
	if completed, exists := r.completed[partIndex]; exists {
		r.mu.Unlock()
		if completed.Offset != offset || completed.Length != length {
			return 0, fmt.Errorf("part %d window mismatch: offset=%d length=%d", partIndex, offset, length)
		}
		return completed.Length, fmt.Errorf("%w: part %d", ErrPartAlreadyCompleted, partIndex)
	}
	if r.inFlight[partIndex] {
		r.mu.Unlock()
		return 0, fmt.Errorf("%w: part %d", ErrPartAlreadyInProgress, partIndex)
	}
	r.inFlight[partIndex] = true
	file := r.file
	r.mu.Unlock()

	written, writeErr := writeReaderAt(file, offset, length, content)

	r.mu.Lock()
	delete(r.inFlight, partIndex)
	if writeErr == nil {
		r.completed[partIndex] = CompletedPart{
			PartIndex: partIndex,
			Offset:    offset,
			Length:    written,
		}
		r.bytesWritten += written
	}
	r.mu.Unlock()

	if writeErr != nil {
		return written, writeErr
	}
	return written, nil
}

func (r *SessionReceiver) BytesReceived() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bytesWritten
}

func (r *SessionReceiver) Complete(expectedPartCount int, expectedSHA256 string) (string, error) {
	r.mu.Lock()
	if len(r.inFlight) > 0 {
		r.mu.Unlock()
		return "", fmt.Errorf("parts still in progress")
	}
	if expectedPartCount <= 0 {
		r.mu.Unlock()
		return "", fmt.Errorf("invalid expected part count: %d", expectedPartCount)
	}
	if len(r.completed) != expectedPartCount {
		r.mu.Unlock()
		return "", fmt.Errorf("incomplete parts: have=%d want=%d", len(r.completed), expectedPartCount)
	}
	parts := make([]CompletedPart, 0, len(r.completed))
	for _, part := range r.completed {
		parts = append(parts, part)
	}
	file := r.file
	tempPath := r.tempPath
	r.mu.Unlock()

	sort.Slice(parts, func(i int, j int) bool {
		if parts[i].Offset == parts[j].Offset {
			return parts[i].PartIndex < parts[j].PartIndex
		}
		return parts[i].Offset < parts[j].Offset
	})
	nextOffset := int64(0)
	for _, part := range parts {
		if part.Offset != nextOffset {
			return "", fmt.Errorf("part coverage mismatch at offset %d", nextOffset)
		}
		nextOffset += part.Length
	}
	if nextOffset != r.totalSize {
		return "", fmt.Errorf("written bytes mismatch: have=%d want=%d", nextOffset, r.totalSize)
	}

	if expectedSHA256 != "" {
		if err := sessionReceiverSyncFile(file); err != nil {
			return "", err
		}
		actualHash, err := sessionReceiverHashFile(tempPath)
		if err != nil {
			return "", err
		}
		if actualHash != expectedSHA256 {
			return "", fmt.Errorf("file sha256 mismatch")
		}
	}
	if err := file.Close(); err != nil {
		return "", err
	}

	finalPath, err := nextAvailablePath(r.dir, r.fileName)
	if err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}

	r.mu.Lock()
	r.file = nil
	r.tempPath = ""
	r.mu.Unlock()

	return finalPath, nil
}

func (r *SessionReceiver) Cleanup() error {
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

func writeReaderAt(file *os.File, offset int64, expectedLength int64, content io.Reader) (int64, error) {
	buffer := sessionReceiverBufferPool.Get().([]byte)
	defer sessionReceiverBufferPool.Put(buffer[:cap(buffer)])
	written := int64(0)
	for written < expectedLength {
		remaining := expectedLength - written
		chunk := buffer
		if remaining < int64(len(chunk)) {
			chunk = chunk[:remaining]
		}

		readBytes, readErr := content.Read(chunk)
		if readBytes > 0 {
			writeOffset := offset + written
			writeCount, err := file.WriteAt(chunk[:readBytes], writeOffset)
			written += int64(writeCount)
			if err != nil {
				return written, err
			}
			if writeCount != readBytes {
				return written, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return written, readErr
		}
	}
	if written != expectedLength {
		return written, fmt.Errorf("part size mismatch: have=%d want=%d", written, expectedLength)
	}
	return written, nil
}

func fileSHA256Hex(path string) (string, error) {
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
