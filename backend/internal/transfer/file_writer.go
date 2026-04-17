package transfer

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	stagedDownloadRenameNoReplace = renameStagedDownloadFileNoReplace
	stagedDownloadLink            = os.Link
	stagedDownloadRemoveFile      = os.Remove
	errFileWriterClosed           = errors.New("file writer already closed")
	errAtomicCommitUnsupported    = errors.New("atomic staged download commit unsupported")
)

type FileWriter struct {
	dir      string
	fileName string
	tempPath string
	file     *os.File
}

func NewFileWriter(dir string, fileName string) (*FileWriter, error) {
	file, safeFileName, tempPath, err := createStagedDownloadFile(dir, fileName, 0)
	if err != nil {
		return nil, err
	}

	return &FileWriter{
		dir:      dir,
		fileName: safeFileName,
		tempPath: tempPath,
		file:     file,
	}, nil
}

func (w *FileWriter) Write(data []byte) (int, error) {
	return w.file.Write(data)
}

func (w *FileWriter) Commit() (string, error) {
	if w.file == nil {
		return "", errFileWriterClosed
	}
	if err := w.file.Sync(); err != nil {
		return "", err
	}
	finalPath, err := commitStagedDownloadFile(w.file, w.tempPath, w.dir, w.fileName)
	if err != nil {
		return "", err
	}
	w.file = nil
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
	candidate := downloadCandidatePath(dir, fileName, 0)
	if _, err := os.Stat(candidate); err != nil {
		if os.IsNotExist(err) {
			return candidate, nil
		}
		return "", err
	}

	for index := 1; ; index++ {
		candidate = downloadCandidatePath(dir, fileName, index)
		if _, err := os.Stat(candidate); err != nil {
			if os.IsNotExist(err) {
				return candidate, nil
			}
			return "", err
		}
	}
}

func createStagedDownloadFile(dir string, fileName string, totalSize int64) (*os.File, string, string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", "", err
	}

	safeFileName := sanitizeFileName(fileName)
	file, err := os.CreateTemp(dir, safeFileName+".*.part")
	if err != nil {
		return nil, "", "", err
	}
	if totalSize > 0 {
		if err := file.Truncate(totalSize); err != nil {
			_ = file.Close()
			_ = os.Remove(file.Name())
			return nil, "", "", err
		}
	}

	return file, safeFileName, file.Name(), nil
}

func commitStagedDownloadFile(file *os.File, tempPath string, dir string, fileName string) (string, error) {
	if err := file.Close(); err != nil {
		return "", err
	}

	finalPath, err := renameStagedDownloadFileWithoutReplace(tempPath, dir, fileName)
	if err == nil {
		return finalPath, nil
	}
	if !errors.Is(err, errNoReplaceRenameUnsupported) {
		cleanupErr := cleanupFailedStagedDownload(tempPath)
		if cleanupErr != nil {
			return "", fmt.Errorf("commit staged download file: rename without replace failed: %v; cleanup failed: %w", err, cleanupErr)
		}
		return "", err
	}

	finalPath, err = linkStagedDownloadFile(tempPath, dir, fileName)
	if err != nil {
		cleanupErr := cleanupFailedStagedDownload(tempPath)
		if cleanupErr != nil {
			return "", fmt.Errorf("commit staged download file: atomic fallback failed: %v; cleanup failed: %w", err, cleanupErr)
		}
		return "", fmt.Errorf("%w: no-replace rename unavailable and hard link fallback failed: %v", errAtomicCommitUnsupported, err)
	}

	cleanupCommittedStagedDownload(tempPath)
	return finalPath, nil
}

func linkStagedDownloadFile(tempPath string, dir string, fileName string) (string, error) {
	for index := 0; ; index++ {
		candidate := downloadCandidatePath(dir, fileName, index)
		if err := stagedDownloadLink(tempPath, candidate); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", err
		}
		return candidate, nil
	}
}

func renameStagedDownloadFileWithoutReplace(tempPath string, dir string, fileName string) (string, error) {
	for index := 0; ; index++ {
		candidate := downloadCandidatePath(dir, fileName, index)
		if err := stagedDownloadRenameNoReplace(tempPath, candidate); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", err
		}
		return candidate, nil
	}
}

func cleanupFailedStagedDownload(tempPath string) error {
	if err := stagedDownloadRemoveFile(tempPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func cleanupCommittedStagedDownload(tempPath string) {
	if err := stagedDownloadRemoveFile(tempPath); err != nil && !os.IsNotExist(err) {
		// 最终文件已交付，临时名清理失败只保留现场。
	}
}

func copyStagedDownloadFile(tempPath string, dir string, fileName string) (string, error) {
	source, err := os.Open(tempPath)
	if err != nil {
		return "", err
	}
	defer source.Close()

	mode := os.FileMode(0o600)
	if info, statErr := source.Stat(); statErr == nil {
		mode = info.Mode().Perm()
	}

	for index := 0; ; index++ {
		candidate := downloadCandidatePath(dir, fileName, index)
		target, err := os.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			if isStagedDownloadNameConflict(candidate, err) {
				continue
			}
			return "", err
		}

		copyErr := copyStagedDownloadContents(source, target)
		closeErr := target.Close()
		if copyErr != nil {
			_ = stagedDownloadRemoveFile(candidate)
			return "", copyErr
		}
		if closeErr != nil {
			_ = stagedDownloadRemoveFile(candidate)
			return "", closeErr
		}
		return candidate, nil
	}
}

func copyStagedDownloadContents(source *os.File, target *os.File) error {
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		return err
	}
	if err := target.Sync(); err != nil {
		return err
	}
	return nil
}

func isStagedDownloadNameConflict(candidate string, err error) bool {
	if os.IsExist(err) {
		return true
	}
	if _, statErr := os.Lstat(candidate); statErr == nil {
		return true
	}
	return false
}

func downloadCandidatePath(dir string, fileName string, index int) string {
	if index <= 0 {
		return filepath.Join(dir, fileName)
	}

	extension := filepath.Ext(fileName)
	baseName := strings.TrimSuffix(fileName, extension)
	return filepath.Join(dir, fmt.Sprintf("%s (%d)%s", baseName, index, extension))
}
