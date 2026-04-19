package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type Settings struct {
	DeviceName          string `json:"deviceName"`
	DownloadDir         string `json:"downloadDir,omitempty"`
	MaxAutoAcceptFileMB int64  `json:"maxAutoAcceptFileMB"`
}

func LoadSettings(rootDir string) (Settings, error) {
	layout := ResolveLayout(rootDir)
	defaults := defaultSettings()

	if err := ensureLayout(layout); err != nil {
		return Settings{}, fmt.Errorf("create settings layout: %w", err)
	}

	content, err := os.ReadFile(layout.ConfigFilePath)
	if errors.Is(err, os.ErrNotExist) {
		if err := writeSettings(layout.ConfigFilePath, defaults); err != nil {
			return Settings{}, err
		}
		return defaults, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read settings file: %w", err)
	}

	settings, normalized, changed, err := normalizeSettings(content, defaults)
	if err != nil {
		return Settings{}, err
	}
	if changed {
		if err := os.WriteFile(layout.ConfigFilePath, normalized, 0o600); err != nil {
			return Settings{}, fmt.Errorf("write settings file: %w", err)
		}
	}

	return settings, nil
}

func SaveSettings(rootDir string, settings Settings) error {
	layout := ResolveLayout(rootDir)
	if err := ensureLayout(layout); err != nil {
		return fmt.Errorf("create settings layout: %w", err)
	}

	rawSettings := make(map[string]json.RawMessage)
	content, err := os.ReadFile(layout.ConfigFilePath)
	if errors.Is(err, os.ErrNotExist) {
		// 首次保存时从空配置开始合并，确保默认值与单项更新都能保留。
	} else if err != nil {
		return fmt.Errorf("read settings file: %w", err)
	} else {
		rawSettings, err = parseRawSettings(content)
		if err != nil {
			return err
		}
	}

	if err := mergeSettings(rawSettings, settings, defaultSettings()); err != nil {
		return err
	}
	return writeRawSettings(layout.ConfigFilePath, rawSettings)
}

func defaultSettings() Settings {
	return Settings{
		DeviceName:          "本机设备",
		MaxAutoAcceptFileMB: 512,
	}
}

func writeSettings(configPath string, settings Settings) error {
	rawSettings := make(map[string]json.RawMessage)
	if err := setSetting(rawSettings, "deviceName", settings.DeviceName); err != nil {
		return err
	}
	if settings.DownloadDir != "" {
		if err := setSetting(rawSettings, "downloadDir", settings.DownloadDir); err != nil {
			return err
		}
	}
	if err := setSetting(rawSettings, "maxAutoAcceptFileMB", settings.MaxAutoAcceptFileMB); err != nil {
		return err
	}
	return writeRawSettings(configPath, rawSettings)
}

func normalizeSettings(content []byte, defaults Settings) (Settings, []byte, bool, error) {
	rawSettings, err := parseRawSettings(content)
	if err != nil {
		return Settings{}, nil, false, err
	}

	changed, err := mergeMissingSetting(rawSettings, "deviceName", defaults.DeviceName)
	if err != nil {
		return Settings{}, nil, false, err
	}

	mergedMaxAutoAccept, err := mergeMissingSetting(rawSettings, "maxAutoAcceptFileMB", defaults.MaxAutoAcceptFileMB)
	if err != nil {
		return Settings{}, nil, false, err
	}
	changed = changed || mergedMaxAutoAccept

	normalized, err := marshalRawSettings(rawSettings)
	if err != nil {
		return Settings{}, nil, false, err
	}

	settings, err := decodeSettings(rawSettings)
	if err != nil {
		return Settings{}, nil, false, err
	}
	return settings, normalized, changed, nil
}

func parseRawSettings(content []byte) (map[string]json.RawMessage, error) {
	rawSettings := make(map[string]json.RawMessage)
	if trimmed := bytes.TrimSpace(content); len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, &rawSettings); err != nil {
			return nil, fmt.Errorf("unmarshal settings file: %w", err)
		}
	}
	return rawSettings, nil
}

func writeRawSettings(configPath string, rawSettings map[string]json.RawMessage) error {
	content, err := marshalRawSettings(rawSettings)
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		return fmt.Errorf("write settings file: %w", err)
	}
	return nil
}

func marshalRawSettings(rawSettings map[string]json.RawMessage) ([]byte, error) {
	content, err := json.MarshalIndent(rawSettings, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal normalized settings: %w", err)
	}
	content = append(content, '\n')
	return content, nil
}

func decodeSettings(rawSettings map[string]json.RawMessage) (Settings, error) {
	content, err := marshalRawSettings(rawSettings)
	if err != nil {
		return Settings{}, err
	}

	var settings Settings
	if err := json.Unmarshal(content, &settings); err != nil {
		return Settings{}, fmt.Errorf("decode normalized settings: %w", err)
	}
	return settings, nil
}

func mergeSettings(rawSettings map[string]json.RawMessage, settings Settings, defaults Settings) error {
	if err := mergeStringSetting(rawSettings, "deviceName", settings.DeviceName, defaults.DeviceName); err != nil {
		return err
	}
	if settings.DownloadDir != "" {
		if err := setSetting(rawSettings, "downloadDir", settings.DownloadDir); err != nil {
			return err
		}
	}
	if err := mergeInt64Setting(rawSettings, "maxAutoAcceptFileMB", settings.MaxAutoAcceptFileMB, defaults.MaxAutoAcceptFileMB); err != nil {
		return err
	}
	return nil
}

func mergeStringSetting(rawSettings map[string]json.RawMessage, key string, provided string, fallback string) error {
	if provided != "" {
		return setSetting(rawSettings, key, provided)
	}
	if _, exists := rawSettings[key]; exists {
		return nil
	}
	return setSetting(rawSettings, key, fallback)
}

func mergeInt64Setting(rawSettings map[string]json.RawMessage, key string, provided int64, fallback int64) error {
	if provided != 0 {
		return setSetting(rawSettings, key, provided)
	}
	if _, exists := rawSettings[key]; exists {
		return nil
	}
	return setSetting(rawSettings, key, fallback)
}

func setSetting(rawSettings map[string]json.RawMessage, key string, value any) error {
	content, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal setting %q: %w", key, err)
	}
	rawSettings[key] = content
	return nil
}

func mergeMissingSetting(rawSettings map[string]json.RawMessage, key string, value any) (bool, error) {
	if _, exists := rawSettings[key]; exists {
		return false, nil
	}

	content, err := json.Marshal(value)
	if err != nil {
		return false, fmt.Errorf("marshal default setting %q: %w", key, err)
	}
	rawSettings[key] = content
	return true, nil
}
