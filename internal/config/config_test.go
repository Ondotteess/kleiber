package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefault_HasSensibleValues(t *testing.T) {
	cfg := Default()
	if cfg.Editor.TabSize != 4 {
		t.Errorf("Editor.TabSize = %d, want 4", cfg.Editor.TabSize)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level = %q, want %q", cfg.Logging.Level, "info")
	}
	if cfg.Editor.FormatOnSave {
		t.Error("Editor.FormatOnSave = true, want false until UI/settings UX exists")
	}
	if cfg.AI.Providers == nil {
		t.Error("AI.Providers should be non-nil for ease of assignment")
	}
	if cfg.KeyBinds == nil {
		t.Error("KeyBinds should be non-nil for ease of assignment")
	}
}

func TestDefault_ReturnsIndependentMaps(t *testing.T) {
	a := Default()
	b := Default()
	a.KeyBinds["editor.save"] = "ctrl+s"
	if _, ok := b.KeyBinds["editor.save"]; ok {
		t.Error("Default() should return independent KeyBinds maps")
	}
}

func TestSaveFile_LoadFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	want := Default()
	want.Editor.TabSize = 2
	want.Editor.Theme = "midnight"
	want.Logging.Level = "debug"
	want.KeyBinds["editor.save"] = "ctrl+s"

	if err := SaveFile(path, want); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}

	got, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestLoadFile_Missing_ReturnsDefaultAndErrNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")

	cfg, err := LoadFile(path)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if cfg.Editor.TabSize != 4 {
		t.Errorf("returned Config is not Default: TabSize = %d", cfg.Editor.TabSize)
	}
}

func TestLoadFile_UnknownField_Error(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"editor":{"tabSize":2},"bogus":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatal("LoadFile should reject unknown JSON fields")
	}
}

func TestLoadFile_MissingFields_FilledFromDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// User supplies only one nested field; everything else must come from Default.
	if err := os.WriteFile(path, []byte(`{"editor":{"tabSize":2}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got.Editor.TabSize != 2 {
		t.Errorf("Editor.TabSize = %d, want 2", got.Editor.TabSize)
	}
	if got.Editor.Theme != "default" {
		t.Errorf("Editor.Theme = %q, want %q (default)", got.Editor.Theme, "default")
	}
	if got.Logging.Level != "info" {
		t.Errorf("Logging.Level = %q, want %q (default)", got.Logging.Level, "info")
	}
}

func TestSaveFile_AtomicNoTempLeftover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := SaveFile(path, Default()); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file leaked: %s", e.Name())
		}
	}
}

func TestSaveFile_EmptyPath_Error(t *testing.T) {
	if err := SaveFile("", Default()); err == nil {
		t.Fatal("SaveFile with empty path should error")
	}
}

func TestUserConfigPath_NonEmpty(t *testing.T) {
	p, err := UserConfigPath()
	if err != nil {
		t.Fatalf("UserConfigPath: %v", err)
	}
	if p == "" {
		t.Fatal("UserConfigPath returned empty string")
	}
	if !filepath.IsAbs(p) {
		t.Errorf("UserConfigPath = %q, want absolute path", p)
	}
	if filepath.Base(p) != configFile {
		t.Errorf("UserConfigPath = %q, want basename %q", p, configFile)
	}
}

func TestUserCachePath_NonEmpty(t *testing.T) {
	p, err := UserCachePath()
	if err != nil {
		t.Fatalf("UserCachePath: %v", err)
	}
	if p == "" {
		t.Fatal("UserCachePath returned empty string")
	}
	if !filepath.IsAbs(p) {
		t.Errorf("UserCachePath = %q, want absolute path", p)
	}
}
