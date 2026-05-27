package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Ondotteess/kleiber/internal/commands"
	"github.com/Ondotteess/kleiber/internal/config"
)

func TestNewDefaultSession_MissingConfigUsesDefaultAndRegistersCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-config.json")

	s, err := NewDefaultSession(context.Background(), DefaultSessionOptions{ConfigPath: path})
	if err != nil {
		t.Fatalf("NewDefaultSession: %v", err)
	}
	if got := s.Config().Editor.TabSize; got != config.Default().Editor.TabSize {
		t.Fatalf("Config.Editor.TabSize = %d, want default", got)
	}
	if !s.Dispatcher().Has(CommandOpenFile) || !s.Dispatcher().Has(CommandProjectRefresh) {
		t.Fatalf("bootstrap did not register expected commands")
	}
}

func TestNewDefaultSession_LoadsConfigPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := config.Default()
	want.Editor.TabSize = 2
	want.Logging.Level = "debug"
	if err := config.SaveFile(path, want); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}

	s, err := NewDefaultSession(context.Background(), DefaultSessionOptions{ConfigPath: path})
	if err != nil {
		t.Fatalf("NewDefaultSession: %v", err)
	}
	if got := s.Config().Editor.TabSize; got != 2 {
		t.Fatalf("Config.Editor.TabSize = %d, want 2", got)
	}
}

func TestSession_Config_ReturnsDefensiveCopy(t *testing.T) {
	cfg := config.Default()
	cfg.KeyBinds["editor.saveBuffer"] = "ctrl+s"
	cfg.LSP.InitOptions = map[string]any{"gofumpt": true}
	cfg.AI.Providers["local"] = config.AIProviderConfig{
		Model: "test",
		Extra: map[string]any{"endpoint": "a"},
	}
	s, err := NewSession(Options{Config: &cfg})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	got := s.Config()
	got.KeyBinds["editor.saveBuffer"] = "mutated"
	got.LSP.InitOptions["gofumpt"] = false
	provider := got.AI.Providers["local"]
	provider.Extra["endpoint"] = "mutated"
	got.AI.Providers["local"] = provider

	next := s.Config()
	if next.KeyBinds["editor.saveBuffer"] != "ctrl+s" {
		t.Fatal("Config returned mutable KeyBinds")
	}
	if next.LSP.InitOptions["gofumpt"] != true {
		t.Fatal("Config returned mutable LSP.InitOptions")
	}
	if next.AI.Providers["local"].Extra["endpoint"] != "a" {
		t.Fatal("Config returned mutable AI provider Extra")
	}
}

func TestNewDefaultSession_OpensProjectRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test/bootstrap\n\ngo 1.25\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	cfg := config.Default()

	s, err := NewDefaultSession(context.Background(), DefaultSessionOptions{
		Config:      &cfg,
		ProjectRoot: root,
	})
	if err != nil {
		t.Fatalf("NewDefaultSession: %v", err)
	}
	snap, ok := s.ProjectSnapshot()
	if !ok {
		t.Fatal("ProjectSnapshot ok = false")
	}
	if snap.Root == "" || len(snap.Modules) != 1 {
		t.Fatalf("unexpected project snapshot: %+v", snap)
	}
}

func TestNewDefaultSession_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewDefaultSession(ctx, DefaultSessionOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("NewDefaultSession err = %v, want context.Canceled", err)
	}
}

func TestNewDefaultSession_DuplicateRegistrationErrorsCleanly(t *testing.T) {
	cfg := config.Default()
	d := commands.New(nil)
	s, err := NewSession(Options{Config: &cfg, Dispatcher: d})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := s.RegisterCommands(); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}

	_, err = NewDefaultSession(context.Background(), DefaultSessionOptions{
		Config:     &cfg,
		Dispatcher: d,
	})
	if !errors.Is(err, commands.ErrDuplicateName) {
		t.Fatalf("NewDefaultSession err = %v, want ErrDuplicateName", err)
	}
}
