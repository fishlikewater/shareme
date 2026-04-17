//go:build windows

package config

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32DLL               = windows.NewLazySystemDLL("shell32.dll")
	ole32DLL                 = windows.NewLazySystemDLL("ole32.dll")
	shGetKnownFolderPathProc = shell32DLL.NewProc("SHGetKnownFolderPath")
	coTaskMemFreeProc        = ole32DLL.NewProc("CoTaskMemFree")
	downloadsFolderID        = windows.GUID{
		Data1: 0x374de290,
		Data2: 0x123f,
		Data3: 0x4565,
		Data4: [8]byte{0x91, 0x64, 0x39, 0xc4, 0x92, 0x5e, 0x46, 0x7b},
	}
)

func resolvePlatformDownloadDir() (string, error) {
	var rawPath uintptr
	result, _, callErr := shGetKnownFolderPathProc.Call(
		uintptr(unsafe.Pointer(&downloadsFolderID)),
		0,
		0,
		uintptr(unsafe.Pointer(&rawPath)),
	)
	if result != 0 {
		if errno, ok := callErr.(windows.Errno); ok && errno != windows.ERROR_SUCCESS {
			return "", fmt.Errorf("resolve known folder downloads: HRESULT 0x%x, Win32 0x%x", result, uint32(errno))
		}
		return "", fmt.Errorf("resolve known folder downloads: HRESULT 0x%x", result)
	}
	if rawPath == 0 {
		return "", fmt.Errorf("resolve known folder downloads: empty result")
	}
	defer coTaskMemFreeProc.Call(rawPath)

	return windows.UTF16PtrToString((*uint16)(unsafe.Pointer(rawPath))), nil
}
