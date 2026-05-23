package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// appDir is the per-platform subdirectory under the user-config and
// user-cache roots that Kleiber reserves for itself.
const appDir = "kleiber"

// configFile is the default basename for a serialized Config under appDir.
const configFile = "config.json"

// UserConfigPath returns the absolute path to the user-level config.json.
// It does not create any directories; callers that intend to write should
// pass the returned path to SaveFile, which creates the parent dir.
func UserConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolving user config dir: %w", err)
	}
	return filepath.Join(dir, appDir, configFile), nil
}

// UserCachePath returns the absolute path to the per-user Kleiber cache
// directory (e.g., ~/.cache/kleiber on Linux). It does not create the
// directory.
func UserCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolving user cache dir: %w", err)
	}
	return filepath.Join(dir, appDir), nil
}

// ensureDir creates dir (and any missing parents) with mode 0o755. It is
// a no-op if dir already exists.
func ensureDir(dir string) error {
	if dir == "" {
		return errors.New("config: empty directory path")
	}
	return os.MkdirAll(dir, 0o755)
}
