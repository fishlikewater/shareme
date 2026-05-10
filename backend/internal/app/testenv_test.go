package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	prepareSharemeTestHome()
	os.Exit(m.Run())
}

func prepareSharemeTestHome() {
	tempHome, err := os.MkdirTemp("", "shareme-test-home-")
	if err != nil {
		panic(err)
	}

	_ = os.Setenv("HOME", tempHome)
	_ = os.Setenv("USERPROFILE", tempHome)
	_ = os.Setenv("APPDATA", filepath.Join(tempHome, "AppData", "Roaming"))
	_ = os.Setenv("LOCALAPPDATA", filepath.Join(tempHome, "AppData", "Local"))
	_ = os.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	_ = os.Setenv("XDG_DATA_HOME", filepath.Join(tempHome, ".local", "share"))
}
