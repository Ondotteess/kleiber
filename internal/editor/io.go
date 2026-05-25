package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// atomicWriteFile writes data to path atomically: the bytes are
// staged in a sibling temp file and renamed into place on success.
// A crash mid-write leaves the prior file (or no file) but never a
// half-written one.
//
// The parent directory is created with mode 0o755 if it is missing.
// The destination file is created with mode 0o644 (subject to the
// process umask) — atomicWriteFile does not attempt to preserve the
// permissions of an existing file at path. Callers that need that
// (rare for editor saves) should chmod after writing.
func atomicWriteFile(path string, data []byte) error {
	if path == "" {
		return errors.New("editor: empty path")
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("creating dir %s: %w", parent, err)
	}

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
