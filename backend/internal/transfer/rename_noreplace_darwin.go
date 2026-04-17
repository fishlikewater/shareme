//go:build darwin

package transfer

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func renameStagedDownloadFileNoReplace(from string, to string) error {
	err := unix.RenamexNp(from, to, unix.RENAME_EXCL)
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.EEXIST) {
		return os.ErrExist
	}
	if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
		return errNoReplaceRenameUnsupported
	}
	return err
}
