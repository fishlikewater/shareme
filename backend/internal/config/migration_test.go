package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shareme/backend/internal/device"
)

func TestMigrateLegacyDataCopiesRuntimeFilesAndCreatesMarker(t *testing.T) {
	legacyDir := filepath.Join(t.TempDir(), "legacy")
	newRootDir := filepath.Join(t.TempDir(), "new-root")
	legacy := writeLegacyRuntimeData(t, legacyDir)

	if err := MigrateLegacyData(MigrationOptions{
		LegacyDirs: []string{legacyDir},
		NewRootDir: newRootDir,
	}); err != nil {
		t.Fatalf("migrate legacy data: %v", err)
	}

	assertFileContent(t, filepath.Join(newRootDir, "config.json"), legacy.configContent)
	assertFileContent(t, filepath.Join(newRootDir, "local-device.json"), legacy.identityContent)
	assertFileContent(t, filepath.Join(newRootDir, "shareme.db"), legacy.databaseContent)
	assertMigrationDirExists(t, filepath.Join(newRootDir, "logs"))
	assertMigrationDirExists(t, filepath.Join(newRootDir, "downloads"))
	assertFileContent(t, filepath.Join(newRootDir, migrationMarkerFileName), []byte(legacyDir))
}

func TestMigrateLegacyDataDoesNotOverwriteInitializedNewRoot(t *testing.T) {
	legacyDir := filepath.Join(t.TempDir(), "legacy")
	newRootDir := filepath.Join(t.TempDir(), "new-root")
	legacy := writeLegacyRuntimeData(t, legacyDir)

	existingConfig := []byte("{\"deviceName\":\"new-device\"}\n")
	if err := os.MkdirAll(newRootDir, 0o755); err != nil {
		t.Fatalf("create new root dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(newRootDir, "config.json"), existingConfig, 0o600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	if err := MigrateLegacyData(MigrationOptions{
		LegacyDirs: []string{legacyDir},
		NewRootDir: newRootDir,
	}); err != nil {
		t.Fatalf("migrate legacy data: %v", err)
	}

	assertFileContent(t, filepath.Join(newRootDir, "config.json"), existingConfig)
	if _, err := os.Stat(filepath.Join(newRootDir, "local-device.json")); !os.IsNotExist(err) {
		t.Fatalf("expected identity file to remain absent when new root already initialized, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(newRootDir, "shareme.db")); !os.IsNotExist(err) {
		t.Fatalf("expected database file to remain absent when new root already initialized, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(newRootDir, migrationMarkerFileName)); !os.IsNotExist(err) {
		t.Fatalf("expected migration marker to remain absent when migration skipped, got %v", err)
	}
	assertFileContent(t, filepath.Join(legacyDir, "config.json"), legacy.configContent)
}

func TestMigrateLegacyDataTreatsInvalidConfigOnlyNewRootAsUninitialized(t *testing.T) {
	legacyDir := filepath.Join(t.TempDir(), "legacy")
	newRootDir := filepath.Join(t.TempDir(), "new-root")
	legacy := writeLegacyRuntimeData(t, legacyDir)

	if err := os.MkdirAll(newRootDir, 0o755); err != nil {
		t.Fatalf("create new root dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(newRootDir, "config.json"), []byte("{invalid-json"), 0o600); err != nil {
		t.Fatalf("write invalid new-root config: %v", err)
	}

	if err := MigrateLegacyData(MigrationOptions{
		LegacyDirs: []string{legacyDir},
		NewRootDir: newRootDir,
	}); err != nil {
		t.Fatalf("migrate legacy data: %v", err)
	}

	assertFileContent(t, filepath.Join(newRootDir, "config.json"), legacy.configContent)
	assertFileContent(t, filepath.Join(newRootDir, "local-device.json"), legacy.identityContent)
	assertFileContent(t, filepath.Join(newRootDir, "shareme.db"), legacy.databaseContent)
	assertFileContent(t, filepath.Join(newRootDir, migrationMarkerFileName), []byte(legacyDir))
}

func TestMigrateLegacyDataResumesAfterPartialFailure(t *testing.T) {
	legacyDir := filepath.Join(t.TempDir(), "legacy")
	newRootDir := filepath.Join(t.TempDir(), "new-root")
	legacy := writeLegacyRuntimeData(t, legacyDir)

	blockedIdentityPath := filepath.Join(newRootDir, "local-device.json")
	if err := os.MkdirAll(blockedIdentityPath, 0o755); err != nil {
		t.Fatalf("create blocked identity dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blockedIdentityPath, "child.txt"), []byte("blocked"), 0o600); err != nil {
		t.Fatalf("write blocked identity child: %v", err)
	}

	err := MigrateLegacyData(MigrationOptions{
		LegacyDirs: []string{legacyDir},
		NewRootDir: newRootDir,
	})
	if err == nil {
		t.Fatal("expected partial migration to fail")
	}

	assertFileContent(t, filepath.Join(newRootDir, "config.json"), legacy.configContent)
	assertFileContent(t, filepath.Join(newRootDir, migrationInProgressMarkerFileName), []byte(legacyDir))
	if _, statErr := os.Stat(filepath.Join(newRootDir, migrationMarkerFileName)); !os.IsNotExist(statErr) {
		t.Fatalf("expected final migration marker to be absent after partial failure, got %v", statErr)
	}

	if err := os.RemoveAll(blockedIdentityPath); err != nil {
		t.Fatalf("remove blocked identity path: %v", err)
	}
	if err := MigrateLegacyData(MigrationOptions{
		LegacyDirs: []string{legacyDir},
		NewRootDir: newRootDir,
	}); err != nil {
		t.Fatalf("resume migration after partial failure: %v", err)
	}

	assertFileContent(t, filepath.Join(newRootDir, "config.json"), legacy.configContent)
	assertFileContent(t, filepath.Join(newRootDir, "local-device.json"), legacy.identityContent)
	assertFileContent(t, filepath.Join(newRootDir, "shareme.db"), legacy.databaseContent)
	assertFileContent(t, filepath.Join(newRootDir, migrationMarkerFileName), []byte(legacyDir))
	if _, statErr := os.Stat(filepath.Join(newRootDir, migrationInProgressMarkerFileName)); !os.IsNotExist(statErr) {
		t.Fatalf("expected in-progress migration marker to be removed, got %v", statErr)
	}
}

func TestMigrateLegacyDataTreatsStructurallyInvalidIdentityAsUninitialized(t *testing.T) {
	legacyDir := filepath.Join(t.TempDir(), "legacy")
	newRootDir := filepath.Join(t.TempDir(), "new-root")
	legacy := writeLegacyRuntimeData(t, legacyDir)

	if err := os.MkdirAll(newRootDir, 0o755); err != nil {
		t.Fatalf("create new root dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(newRootDir, "local-device.json"), []byte("{\"deviceId\":\"legacy-id\",\"deviceName\":\"legacy-device\"}\n"), 0o600); err != nil {
		t.Fatalf("write invalid new-root identity: %v", err)
	}

	if err := MigrateLegacyData(MigrationOptions{
		LegacyDirs: []string{legacyDir},
		NewRootDir: newRootDir,
	}); err != nil {
		t.Fatalf("migrate legacy data: %v", err)
	}

	assertFileContent(t, filepath.Join(newRootDir, "config.json"), legacy.configContent)
	assertFileContent(t, filepath.Join(newRootDir, "local-device.json"), legacy.identityContent)
	assertFileContent(t, filepath.Join(newRootDir, "shareme.db"), legacy.databaseContent)
	assertFileContent(t, filepath.Join(newRootDir, migrationMarkerFileName), []byte(legacyDir))
}

func TestMigrateLegacyDataResumesAfterPartialDatabaseCopyFailure(t *testing.T) {
	legacyDir := filepath.Join(t.TempDir(), "legacy")
	newRootDir := filepath.Join(t.TempDir(), "new-root")
	legacy := writeLegacyRuntimeData(t, legacyDir)

	originalCopyStream := migrationCopyStream
	t.Cleanup(func() {
		migrationCopyStream = originalCopyStream
	})

	injectedFailure := true
	migrationCopyStream = func(dst io.Writer, src io.Reader) (int64, error) {
		dstFile, ok := dst.(*os.File)
		if injectedFailure && ok && strings.HasPrefix(filepath.Base(dstFile.Name()), "shareme.db.migration-") {
			injectedFailure = false
			buffer := make([]byte, 4)
			readBytes, readErr := src.Read(buffer)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return 0, readErr
			}
			writtenBytes, writeErr := dst.Write(buffer[:readBytes])
			if writeErr != nil {
				return int64(writtenBytes), writeErr
			}
			return int64(writtenBytes), errors.New("inject database copy failure")
		}
		return originalCopyStream(dst, src)
	}

	err := MigrateLegacyData(MigrationOptions{
		LegacyDirs: []string{legacyDir},
		NewRootDir: newRootDir,
	})
	if err == nil {
		t.Fatal("expected migration to fail during database copy")
	}

	if _, statErr := os.Stat(filepath.Join(newRootDir, "shareme.db")); !os.IsNotExist(statErr) {
		t.Fatalf("expected database file to stay absent after partial copy failure, got %v", statErr)
	}
	assertFileContent(t, filepath.Join(newRootDir, migrationInProgressMarkerFileName), []byte(legacyDir))

	migrationCopyStream = originalCopyStream
	if err := MigrateLegacyData(MigrationOptions{
		LegacyDirs: []string{legacyDir},
		NewRootDir: newRootDir,
	}); err != nil {
		t.Fatalf("resume migration after database copy failure: %v", err)
	}

	assertFileContent(t, filepath.Join(newRootDir, "config.json"), legacy.configContent)
	assertFileContent(t, filepath.Join(newRootDir, "local-device.json"), legacy.identityContent)
	assertFileContent(t, filepath.Join(newRootDir, "shareme.db"), legacy.databaseContent)
	assertFileContent(t, filepath.Join(newRootDir, migrationMarkerFileName), []byte(legacyDir))
}

func TestMigrateLegacyDataRunsOnlyOnceWhenMarkerExists(t *testing.T) {
	legacyDir := filepath.Join(t.TempDir(), "legacy")
	newRootDir := filepath.Join(t.TempDir(), "new-root")
	writeLegacyRuntimeData(t, legacyDir)

	if err := MigrateLegacyData(MigrationOptions{
		LegacyDirs: []string{legacyDir},
		NewRootDir: newRootDir,
	}); err != nil {
		t.Fatalf("first migrate legacy data: %v", err)
	}

	for _, name := range []string{"config.json", "local-device.json", "shareme.db"} {
		if err := os.Remove(filepath.Join(newRootDir, name)); err != nil {
			t.Fatalf("remove migrated file %s: %v", name, err)
		}
	}

	if err := os.WriteFile(filepath.Join(legacyDir, "config.json"), []byte("{\"deviceName\":\"updated-legacy\"}\n"), 0o600); err != nil {
		t.Fatalf("rewrite legacy config: %v", err)
	}

	if err := MigrateLegacyData(MigrationOptions{
		LegacyDirs: []string{legacyDir},
		NewRootDir: newRootDir,
	}); err != nil {
		t.Fatalf("second migrate legacy data: %v", err)
	}

	if _, err := os.Stat(filepath.Join(newRootDir, "config.json")); !os.IsNotExist(err) {
		t.Fatalf("expected config to stay absent after second migration attempt, got %v", err)
	}
	assertFileContent(t, filepath.Join(newRootDir, migrationMarkerFileName), []byte(legacyDir))
}

func TestLegacyDataDirCandidatesIncludeHistoricalDefaults(t *testing.T) {
	originalUserConfigDir := legacyUserConfigDir
	originalUserHomeDir := legacyUserHomeDir
	t.Cleanup(func() {
		legacyUserConfigDir = originalUserConfigDir
		legacyUserHomeDir = originalUserHomeDir
	})

	configBaseDir := filepath.Join(t.TempDir(), "config-base")
	homeDir := filepath.Join(t.TempDir(), "home")
	legacyUserConfigDir = func() (string, error) {
		return configBaseDir, nil
	}
	legacyUserHomeDir = func() (string, error) {
		return homeDir, nil
	}

	candidates := LegacyDataDirCandidates()
	expected := []string{
		filepath.Join(configBaseDir, "MessageShare"),
		filepath.Join(configBaseDir, "message-share"),
		filepath.Join(configBaseDir, "shareme"),
		filepath.Join(homeDir, "MessageShare"),
		filepath.Join(homeDir, ".message-share"),
		filepath.Join(homeDir, "AppData", "Roaming", "MessageShare"),
		filepath.Join(homeDir, ".config", "MessageShare"),
		filepath.Join(homeDir, ".config", "message-share"),
		filepath.Join(homeDir, ".config", "shareme"),
		filepath.Join(homeDir, "Library", "Application Support", "MessageShare"),
	}
	for _, want := range expected {
		if !containsPath(candidates, want) {
			t.Fatalf("expected candidates to include %s, got %#v", want, candidates)
		}
	}
}

type legacyRuntimeData struct {
	configContent   []byte
	identityContent []byte
	databaseContent []byte
}

func writeLegacyRuntimeData(t *testing.T, rootDir string) legacyRuntimeData {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(rootDir, "logs"), 0o755); err != nil {
		t.Fatalf("create legacy logs dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "downloads"), 0o755); err != nil {
		t.Fatalf("create legacy downloads dir: %v", err)
	}

	data := legacyRuntimeData{
		configContent:   []byte("{\"deviceName\":\"legacy-device\",\"maxAutoAcceptFileMB\":1024}\n"),
		databaseContent: []byte("legacy-db"),
	}
	if err := os.WriteFile(filepath.Join(rootDir, "config.json"), data.configContent, 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	identityPath := filepath.Join(rootDir, "local-device.json")
	if _, err := device.EnsureLocalDevice(identityPath, "legacy-device"); err != nil {
		t.Fatalf("write legacy identity: %v", err)
	}
	content, err := os.ReadFile(identityPath)
	if err != nil {
		t.Fatalf("read legacy identity: %v", err)
	}
	data.identityContent = content
	if err := os.WriteFile(filepath.Join(rootDir, legacyDatabaseFileName), data.databaseContent, 0o600); err != nil {
		t.Fatalf("write legacy database: %v", err)
	}
	return data
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file %s: %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("unexpected content for %s: got %q want %q", path, string(got), string(want))
	}
}

func assertMigrationDirExists(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat dir %s: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %s to be a directory", path)
	}
}

func containsPath(paths []string, want string) bool {
	cleanWant := filepath.Clean(want)
	for _, path := range paths {
		if filepath.Clean(path) == cleanWant {
			return true
		}
	}
	return false
}
