//go:build windows

package transfer

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func renameStagedDownloadFileNoReplace(from string, to string) error {
	fromUTF16, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	toUTF16, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}

	err = windows.MoveFileEx(fromUTF16, toUTF16, windows.MOVEFILE_WRITE_THROUGH)
	if err == nil {
		return nil
	}
	return mapNoReplaceRenameError(to, err)
}

func mapNoReplaceRenameError(targetPath string, err error) error {
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return os.ErrExist
	}
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		info, statErr := os.Lstat(targetPath)
		if statErr == nil && info.IsDir() {
			return os.ErrExist
		}
	}
	return err
}
