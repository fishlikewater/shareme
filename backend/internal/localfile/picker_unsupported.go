//go:build !windows

package localfile

import (
	"context"
	"fmt"
	"runtime"
)

func NewPicker() Picker {
	return PickerFunc(func(context.Context) (PickedFile, error) {
		return PickedFile{}, fmt.Errorf("local file picker unsupported on %s", runtime.GOOS)
	})
}
