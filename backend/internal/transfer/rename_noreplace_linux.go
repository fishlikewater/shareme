//go:build linux

package transfer

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func renameStagedDownloadFileNoReplace(from string, to string) error {
	err := unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE)
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.EEXIST) {
		return os.ErrExist
	}
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) {
		return errNoReplaceRenameUnsupported
	}
	return err
}
