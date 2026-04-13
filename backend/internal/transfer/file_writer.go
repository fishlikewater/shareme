package transfer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FileWriter struct {
	dir      string
	fileName string
	tempPath string
	file     *os.File
}

func NewFileWriter(dir string, fileName string) (*FileWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	safeFileName := sanitizeFileName(fileName)
	file, err := os.CreateTemp(dir, safeFileName+".*.part")
	if err != nil {
		return nil, err
	}

	return &FileWriter{
		dir:      dir,
		fileName: safeFileName,
		tempPath: file.Name(),
		file:     file,
	}, nil
}

func (w *FileWriter) Write(data []byte) (int, error) {
	return w.file.Write(data)
}

func (w *FileWriter) Commit() (string, error) {
	if err := w.file.Sync(); err != nil {
		return "", err
	}
	if err := w.file.Close(); err != nil {
		return "", err
	}
	w.file = nil

	finalPath, err := nextAvailablePath(w.dir, w.fileName)
	if err != nil {
		return "", err
	}
	if err := os.Rename(w.tempPath, finalPath); err != nil {
		_ = os.Remove(w.tempPath)
		return "", err
	}
	w.tempPath = ""
	return finalPath, nil
}

func (w *FileWriter) Cleanup() error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	if w.tempPath != "" {
		_ = os.Remove(w.tempPath)
		w.tempPath = ""
	}
	return nil
}

func sanitizeFileName(fileName string) string {
	base := strings.TrimSpace(filepath.Base(fileName))
	if base == "" || base == "." {
		return "download.bin"
	}
	return base
}

func nextAvailablePath(dir string, fileName string) (string, error) {
	candidate := filepath.Join(dir, fileName)
	if _, err := os.Stat(candidate); err != nil {
		if os.IsNotExist(err) {
			return candidate, nil
		}
		return "", err
	}

	extension := filepath.Ext(fileName)
	baseName := strings.TrimSuffix(fileName, extension)
	for index := 1; ; index++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", baseName, index, extension))
		if _, err := os.Stat(candidate); err != nil {
			if os.IsNotExist(err) {
				return candidate, nil
			}
			return "", err
		}
	}
}
