package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"shareme/backend/internal/device"
)

const (
	migrationMarkerFileName           = ".migrated"
	migrationInProgressMarkerFileName = ".migration-in-progress"
	legacyDatabaseFileName            = "message-share.db"
)

var (
	legacyUserConfigDir = os.UserConfigDir
	legacyUserHomeDir   = os.UserHomeDir
	migrationCopyStream = func(dst io.Writer, src io.Reader) (int64, error) {
		return io.Copy(dst, src)
	}
)

type MigrationOptions struct {
	LegacyDirs []string
	NewRootDir string
}

func LegacyDataDirCandidates() []string {
	candidates := make([]string, 0, 6)
	seen := make(map[string]struct{})

	appendCandidate := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		cleanPath := filepath.Clean(path)
		if _, exists := seen[cleanPath]; exists {
			return
		}
		seen[cleanPath] = struct{}{}
		candidates = append(candidates, cleanPath)
	}

	if configDir, err := legacyUserConfigDir(); err == nil {
		appendCandidate(filepath.Join(configDir, "MessageShare"))
		appendCandidate(filepath.Join(configDir, "message-share"))
		appendCandidate(filepath.Join(configDir, "shareme"))
	}
	if homeDir, err := legacyUserHomeDir(); err == nil {
		appendCandidate(filepath.Join(homeDir, "MessageShare"))
		appendCandidate(filepath.Join(homeDir, ".message-share"))
		appendCandidate(filepath.Join(homeDir, "AppData", "Roaming", "MessageShare"))
		appendCandidate(filepath.Join(homeDir, ".config", "MessageShare"))
		appendCandidate(filepath.Join(homeDir, ".config", "message-share"))
		appendCandidate(filepath.Join(homeDir, ".config", "shareme"))
		appendCandidate(filepath.Join(homeDir, "Library", "Application Support", "MessageShare"))
	}

	return candidates
}

func MigrateLegacyData(opts MigrationOptions) error {
	newRootDir := filepath.Clean(strings.TrimSpace(opts.NewRootDir))
	if newRootDir == "" {
		return errors.New("legacy migration requires new root dir")
	}
	if migrationAlreadyHandled(newRootDir) {
		return nil
	}
	inProgressLegacyDir, hasInProgress, err := readMigrationInProgressSource(newRootDir)
	if err != nil {
		return err
	}
	if !hasInProgress && hasInitializedRuntimeData(newRootDir) {
		return nil
	}

	legacyDirs := prioritizedLegacyDirs(opts.LegacyDirs, inProgressLegacyDir)
	for _, legacyDir := range legacyDirs {
		legacyDir = filepath.Clean(strings.TrimSpace(legacyDir))
		if legacyDir == "" || legacyDir == newRootDir {
			continue
		}
		if !hasMigratableRuntimeData(legacyDir) {
			continue
		}
		if err := writeMigrationInProgressMarker(newRootDir, legacyDir); err != nil {
			return err
		}
		if err := copyLegacyRuntimeData(legacyDir, newRootDir); err != nil {
			return err
		}
		if err := finalizeMigration(newRootDir, legacyDir); err != nil {
			return err
		}
		return nil
	}

	return nil
}

func migrationAlreadyHandled(rootDir string) bool {
	return pathExists(filepath.Join(rootDir, migrationMarkerFileName))
}

func hasInitializedRuntimeData(rootDir string) bool {
	layout := ResolveLayout(rootDir)
	if hasValidLocalDeviceFile(layout.IdentityFilePath) || hasAnyDatabaseFile(rootDir) {
		return true
	}

	return hasValidSettingsFile(layout.ConfigFilePath)
}

func hasMigratableRuntimeData(rootDir string) bool {
	layout := ResolveLayout(rootDir)
	return hasValidSettingsFile(layout.ConfigFilePath) ||
		hasValidLocalDeviceFile(layout.IdentityFilePath) ||
		hasAnyDatabaseFile(rootDir)
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyLegacyRuntimeData(legacyDir string, newRootDir string) error {
	legacyLayout := ResolveLayout(legacyDir)
	newLayout := ResolveLayout(newRootDir)

	if err := os.MkdirAll(newLayout.RootDir, 0o755); err != nil {
		return fmt.Errorf("create new root dir: %w", err)
	}

	copyFiles := []struct {
		src        string
		dst        string
		shouldCopy func(string) bool
	}{
		{src: legacyLayout.ConfigFilePath, dst: newLayout.ConfigFilePath, shouldCopy: hasValidSettingsFile},
		{src: legacyLayout.IdentityFilePath, dst: newLayout.IdentityFilePath, shouldCopy: hasValidLocalDeviceFile},
	}
	for _, item := range copyFiles {
		if item.shouldCopy != nil && !item.shouldCopy(item.src) {
			continue
		}
		if err := copyFileIfMissing(item.src, item.dst); err != nil {
			return err
		}
	}
	if err := copyFirstExistingFileIfMissing(databasePathCandidates(legacyDir), newLayout.DatabasePath); err != nil {
		return err
	}

	if err := ensureDirIfLegacyExists(legacyLayout.LogDir, newLayout.LogDir); err != nil {
		return err
	}
	if err := ensureDirIfLegacyExists(legacyLayout.DownloadsDir, newLayout.DownloadsDir); err != nil {
		return err
	}

	return nil
}

func hasAnyDatabaseFile(rootDir string) bool {
	for _, path := range databasePathCandidates(rootDir) {
		if pathExists(path) {
			return true
		}
	}
	return false
}

func databasePathCandidates(rootDir string) []string {
	layout := ResolveLayout(rootDir)
	legacyPath := filepath.Join(rootDir, legacyDatabaseFileName)
	if filepath.Clean(layout.DatabasePath) == filepath.Clean(legacyPath) {
		return []string{layout.DatabasePath}
	}
	return []string{layout.DatabasePath, legacyPath}
}

func copyFirstExistingFileIfMissing(srcPaths []string, dst string) error {
	for _, src := range srcPaths {
		if !pathExists(src) {
			continue
		}
		return copyFileIfMissing(src, dst)
	}
	return nil
}

func copyFileIfMissing(src string, dst string) error {
	srcInfo, err := os.Stat(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat legacy file %q: %w", src, err)
	}
	if srcInfo.IsDir() {
		return nil
	}

	if _, err := os.Stat(dst); err == nil {
		if shouldReplaceInvalidTargetFile(dst) {
			if err := os.Remove(dst); err != nil {
				return fmt.Errorf("remove invalid target file %q: %w", dst, err)
			}
		} else {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat target file %q: %w", dst, err)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create target file dir %q: %w", filepath.Dir(dst), err)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open legacy file %q: %w", src, err)
	}
	defer srcFile.Close()

	dstFile, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".migration-*")
	if err != nil {
		return fmt.Errorf("create temp target file for %q: %w", dst, err)
	}
	tempPath := dstFile.Name()
	defer func() {
		_ = dstFile.Close()
	}()
	if err := dstFile.Chmod(srcInfo.Mode().Perm()); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("set temp target file mode for %q: %w", dst, err)
	}

	if _, err := migrationCopyStream(dstFile, srcFile); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("copy legacy file %q: %w", src, err)
	}
	if err := dstFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close temp target file for %q: %w", dst, err)
	}
	if err := os.Rename(tempPath, dst); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("rename temp target file for %q: %w", dst, err)
	}

	return nil
}

func hasValidSettingsFile(path string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	_, err = parseRawSettings(content)
	return err == nil
}

func hasValidLocalDeviceFile(path string) bool {
	return device.ValidateLocalDeviceFile(path) == nil
}

func shouldReplaceInvalidTargetFile(path string) bool {
	switch filepath.Base(path) {
	case "config.json":
		return !hasValidSettingsFile(path)
	case "local-device.json":
		return !hasValidLocalDeviceFile(path)
	default:
		return false
	}
}

func readMigrationInProgressSource(rootDir string) (string, bool, error) {
	content, err := os.ReadFile(filepath.Join(rootDir, migrationInProgressMarkerFileName))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read migration in-progress marker: %w", err)
	}
	return strings.TrimSpace(string(content)), true, nil
}

func writeMigrationInProgressMarker(rootDir string, legacyDir string) error {
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return fmt.Errorf("create migration root dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, migrationInProgressMarkerFileName), []byte(legacyDir), 0o600); err != nil {
		return fmt.Errorf("write migration in-progress marker: %w", err)
	}
	return nil
}

func finalizeMigration(rootDir string, legacyDir string) error {
	if err := os.WriteFile(filepath.Join(rootDir, migrationMarkerFileName), []byte(legacyDir), 0o600); err != nil {
		return fmt.Errorf("write migration marker: %w", err)
	}
	_ = os.Remove(filepath.Join(rootDir, migrationInProgressMarkerFileName))
	return nil
}

func prioritizedLegacyDirs(legacyDirs []string, inProgressLegacyDir string) []string {
	if strings.TrimSpace(inProgressLegacyDir) == "" {
		return legacyDirs
	}

	prioritized := make([]string, 0, len(legacyDirs)+1)
	prioritized = append(prioritized, inProgressLegacyDir)
	for _, legacyDir := range legacyDirs {
		if filepath.Clean(strings.TrimSpace(legacyDir)) == filepath.Clean(strings.TrimSpace(inProgressLegacyDir)) {
			continue
		}
		prioritized = append(prioritized, legacyDir)
	}
	return prioritized
}

func ensureDirIfLegacyExists(src string, dst string) error {
	info, err := os.Stat(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat legacy dir %q: %w", src, err)
	}
	if !info.IsDir() {
		return nil
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("create target dir %q: %w", dst, err)
	}
	return nil
}
