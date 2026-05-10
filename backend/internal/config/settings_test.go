package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSettingsCreatesDefaultConfig(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), ".shareme")

	settings, err := LoadSettings(rootDir)
	if err != nil {
		t.Fatalf("首次加载配置失败: %v", err)
	}

	if settings.DeviceName == "" {
		t.Fatal("期望默认设备名已生成")
	}
	if settings.MaxAutoAcceptFileMB != 512 {
		t.Fatalf("期望默认自动接收上限为 512，实际为 %d", settings.MaxAutoAcceptFileMB)
	}

	configPath := filepath.Join(rootDir, "config.json")
	stored := readStoredSettings(t, configPath)
	if stored.DeviceName != settings.DeviceName {
		t.Fatalf("期望配置文件持久化默认设备名 %q，实际为 %q", settings.DeviceName, stored.DeviceName)
	}
	if stored.MaxAutoAcceptFileMB != 512 {
		t.Fatalf("期望配置文件持久化默认自动接收上限 512，实际为 %d", stored.MaxAutoAcceptFileMB)
	}

	for _, dir := range []string{
		rootDir,
		filepath.Join(rootDir, "downloads"),
		filepath.Join(rootDir, "logs"),
		filepath.Join(rootDir, "tmp"),
	} {
		assertDirExists(t, dir)
	}
}

func TestLoadSettingsPreservesUserValuesWhileBackfillingDefaults(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), ".shareme")
	configPath := filepath.Join(rootDir, "config.json")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("创建根目录失败: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("{\n  \"deviceName\": \"客厅电脑\"\n}\n"), 0o600); err != nil {
		t.Fatalf("写入用户配置失败: %v", err)
	}

	settings, err := LoadSettings(rootDir)
	if err != nil {
		t.Fatalf("再次加载配置失败: %v", err)
	}

	if settings.DeviceName != "客厅电脑" {
		t.Fatalf("期望保留用户设备名 \"客厅电脑\"，实际为 %q", settings.DeviceName)
	}
	if settings.MaxAutoAcceptFileMB != 512 {
		t.Fatalf("期望回填默认自动接收上限 512，实际为 %d", settings.MaxAutoAcceptFileMB)
	}

	stored := readStoredSettings(t, configPath)
	if stored.DeviceName != "客厅电脑" {
		t.Fatalf("期望配置文件保留用户设备名 \"客厅电脑\"，实际为 %q", stored.DeviceName)
	}
	if stored.MaxAutoAcceptFileMB != 512 {
		t.Fatalf("期望配置文件回填默认自动接收上限 512，实际为 %d", stored.MaxAutoAcceptFileMB)
	}
}

func TestSaveSettingsPreservesExistingFieldsAndUnknownKeys(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), ".shareme")
	configPath := filepath.Join(rootDir, "config.json")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("创建根目录失败: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("{\n  \"deviceName\": \"客厅电脑\",\n  \"downloadDir\": \"D:/Downloads\",\n  \"maxAutoAcceptFileMB\": 256,\n  \"theme\": \"dark\"\n}\n"), 0o600); err != nil {
		t.Fatalf("写入初始配置失败: %v", err)
	}

	if err := SaveSettings(rootDir, Settings{
		MaxAutoAcceptFileMB: 1024,
	}); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}

	stored := readStoredSettings(t, configPath)
	if stored.DeviceName != "客厅电脑" {
		t.Fatalf("期望保留已保存设备名，实际为 %q", stored.DeviceName)
	}
	if stored.DownloadDir != "D:/Downloads" {
		t.Fatalf("期望保留已保存下载目录，实际为 %q", stored.DownloadDir)
	}
	if stored.MaxAutoAcceptFileMB != 1024 {
		t.Fatalf("期望更新自动接收上限为 1024，实际为 %d", stored.MaxAutoAcceptFileMB)
	}

	raw := readStoredRawSettings(t, configPath)
	if raw["theme"] != "dark" {
		t.Fatalf("期望保留未知字段 theme=dark，实际为 %#v", raw["theme"])
	}
}

func readStoredSettings(t *testing.T, configPath string) Settings {
	t.Helper()

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取配置文件失败: %v", err)
	}

	var stored Settings
	if err := json.Unmarshal(content, &stored); err != nil {
		t.Fatalf("解析配置文件失败: %v", err)
	}
	return stored
}

func readStoredRawSettings(t *testing.T, configPath string) map[string]any {
	t.Helper()

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取原始配置文件失败: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(content, &raw); err != nil {
		t.Fatalf("解析原始配置文件失败: %v", err)
	}
	return raw
}

func assertDirExists(t *testing.T, dir string) {
	t.Helper()

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("期望目录 %s 已创建: %v", dir, err)
	}
	if !info.IsDir() {
		t.Fatalf("期望路径 %s 为目录", dir)
	}
}
