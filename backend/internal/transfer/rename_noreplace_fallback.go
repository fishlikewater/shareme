//go:build !windows && !linux && !darwin

package transfer

func renameStagedDownloadFileNoReplace(string, string) error {
	return errNoReplaceRenameUnsupported
}
