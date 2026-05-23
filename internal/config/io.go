package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrNotFound indicates a config file did not exist on disk. LoadFile and
// Load return it alongside the default Config so callers can distinguish
// "first run" from real errors with errors.Is.
var ErrNotFound = errors.New("config: file not found")

// LoadFile reads and decodes a Config from path. When the file does not
// exist LoadFile returns Default() together with ErrNotFound so callers
// can treat first-run gracefully:
//
//	cfg, err := config.LoadFile(p)
//	if err != nil && !errors.Is(err, config.ErrNotFound) {
//	    return err
//	}
//
// Decoding rejects unknown JSON fields to surface user typos early.
func LoadFile(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Default(), ErrNotFound
		}
		return Config{}, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()
	return decode(f)
}

// Load reads the user-level config from os.UserConfigDir. Like LoadFile
// it returns Default()+ErrNotFound when the file is missing.
func Load() (Config, error) {
	path, err := UserConfigPath()
	if err != nil {
		return Config{}, err
	}
	return LoadFile(path)
}

// SaveFile writes cfg to path. The write is atomic: data is staged in a
// sibling temp file and renamed into place, so a crash mid-write leaves
// the prior file (or no file) but never a half-written one.
//
// SaveFile creates parent directories with mode 0o755 if they are missing.
func SaveFile(path string, cfg Config) error {
	if path == "" {
		return errors.New("config: empty path")
	}
	parent := filepath.Dir(path)
	if err := ensureDir(parent); err != nil {
		return fmt.Errorf("creating config dir %s: %w", parent, err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	// Append a trailing newline so the file is well-formed in tools that
	// expect POSIX text files.
	data = append(data, '\n')

	tmp, err := os.CreateTemp(parent, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", parent, err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("renaming %s to %s: %w", tmpName, path, err)
	}
	return nil
}

// Save persists cfg to the user-level config location.
func Save(cfg Config) error {
	path, err := UserConfigPath()
	if err != nil {
		return err
	}
	return SaveFile(path, cfg)
}

// decode reads JSON config from r, refusing unknown fields, and overlays
// zero-valued top-level fields with their defaults from Default().
//
// The merge is intentionally shallow: nested structs the user partially
// specified keep the user's intent, with only zero scalars taking the
// default. The alternative (deep merge) tends to surprise users who
// expected their config to be authoritative.
func decode(r io.Reader) (Config, error) {
	var cfg Config
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decoding config: %w", err)
	}

	def := Default()
	if cfg.Editor.TabSize == 0 {
		cfg.Editor.TabSize = def.Editor.TabSize
	}
	if cfg.Editor.FontFamily == "" {
		cfg.Editor.FontFamily = def.Editor.FontFamily
	}
	if cfg.Editor.FontSize == 0 {
		cfg.Editor.FontSize = def.Editor.FontSize
	}
	if cfg.Editor.Theme == "" {
		cfg.Editor.Theme = def.Editor.Theme
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = def.Logging.Level
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = def.Logging.Format
	}
	if cfg.AI.Providers == nil {
		cfg.AI.Providers = def.AI.Providers
	}
	if cfg.KeyBinds == nil {
		cfg.KeyBinds = def.KeyBinds
	}
	return cfg, nil
}
