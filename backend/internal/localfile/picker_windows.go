//go:build windows

package localfile

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func NewPicker() Picker {
	return PickerFunc(func(ctx context.Context) (PickedFile, error) {
		script := strings.Join([]string{
			"Add-Type -AssemblyName System.Windows.Forms",
			"$dialog = New-Object System.Windows.Forms.OpenFileDialog",
			"$dialog.Multiselect = $false",
			"$dialog.CheckFileExists = $true",
			"$result = $dialog.ShowDialog()",
			"if ($result -ne [System.Windows.Forms.DialogResult]::OK) { exit 3 }",
			"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8",
			"Write-Output $dialog.FileName",
		}, "; ")

		cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-STA", "-Command", script)
		output, err := cmd.Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 3 {
				return PickedFile{}, ErrPickerCancelled
			}
			return PickedFile{}, fmt.Errorf("open native file picker: %w", err)
		}

		path := strings.TrimSpace(string(output))
		if path == "" {
			return PickedFile{}, ErrPickerCancelled
		}
		info, err := os.Stat(path)
		if err != nil {
			return PickedFile{}, fmt.Errorf("stat picked file: %w", err)
		}
		return PickedFile{
			Path:        path,
			DisplayName: filepath.Base(path),
			Size:        info.Size(),
			ModifiedAt:  info.ModTime().UTC(),
		}, nil
	})
}
