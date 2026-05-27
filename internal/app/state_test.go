package app

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/Ondotteess/kleiber/internal/commands"
	"github.com/Ondotteess/kleiber/internal/editor"
	"github.com/Ondotteess/kleiber/internal/project"
)

func TestSession_CommandPalette_SortedAndDefensive(t *testing.T) {
	s := newRegisteredSession(t, Options{})

	palette := s.CommandPalette()
	if len(palette) == 0 {
		t.Fatal("CommandPalette returned no commands")
	}
	names := make([]string, len(palette))
	for i, cmd := range palette {
		names[i] = cmd.Name
		if cmd.Description == "" {
			t.Fatalf("command %s has empty description", cmd.Name)
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("palette is not sorted: %v", names)
	}

	palette[0].Name = "mutated"
	next := s.CommandPalette()
	if next[0].Name == "mutated" {
		t.Fatal("CommandPalette returned mutable internal state")
	}
}

func TestSession_Dispatch_UnknownCommandReturnsWrappedError(t *testing.T) {
	s := newRegisteredSession(t, Options{})

	err := s.Dispatch(context.Background(), "missing.command", nil)
	if !errors.Is(err, commands.ErrUnknownCommand) {
		t.Fatalf("Dispatch err = %v, want ErrUnknownCommand", err)
	}
}

func TestSession_Buffers_ReflectCommandsAndAreDefensive(t *testing.T) {
	s := newRegisteredSession(t, Options{})
	path := filepath.Join(t.TempDir(), "main.go")
	writeFile(t, path, "package main\n")

	if err := s.Dispatcher().Dispatch(context.Background(), CommandOpenFile, map[string]any{"path": path}); err != nil {
		t.Fatalf("Dispatch openFile: %v", err)
	}
	if err := s.Dispatcher().Dispatch(context.Background(), CommandNewBuffer, map[string]any{"text": "draft"}); err != nil {
		t.Fatalf("Dispatch newBuffer: %v", err)
	}

	refs := s.Buffers()
	if len(refs) != 2 {
		t.Fatalf("Buffers len = %d, want 2", len(refs))
	}
	if refs[0].Path == "" {
		t.Fatalf("first buffer path is empty: %+v", refs)
	}
	if refs[1].Path != "" || !refs[1].Dirty {
		t.Fatalf("second buffer = %+v, want dirty untitled", refs[1])
	}

	refs[0].Path = "mutated"
	next := s.Buffers()
	if next[0].Path == "mutated" {
		t.Fatal("Buffers returned mutable internal state")
	}
}

func TestSession_Views_ReflectCommandsAndAreDefensive(t *testing.T) {
	s := newRegisteredSession(t, Options{})
	bid := s.Editor().NewBuffer("hello")
	if err := s.Dispatcher().Dispatch(context.Background(), CommandNewView, map[string]any{"bufferID": bid}); err != nil {
		t.Fatalf("Dispatch newView: %v", err)
	}

	views := s.Views(bid)
	if len(views) != 1 {
		t.Fatalf("Views len = %d, want 1", len(views))
	}
	if views[0].BufferID != bid {
		t.Fatalf("View.BufferID = %d, want %d", views[0].BufferID, bid)
	}
	views[0].BufferID = editor.BufferID(999)
	next := s.Views(bid)
	if next[0].BufferID == 999 {
		t.Fatal("Views returned mutable internal state")
	}
}

func TestSession_ProjectSnapshot_NoProjectReturnsFalse(t *testing.T) {
	s := newRegisteredSession(t, Options{})
	if snap, ok := s.ProjectSnapshot(); ok || snap.Root != "" {
		t.Fatalf("ProjectSnapshot = (%+v, %v), want zero false", snap, ok)
	}

	var nilSession *Session
	if snap, ok := nilSession.ProjectSnapshot(); ok || snap.Root != "" {
		t.Fatalf("nil ProjectSnapshot = (%+v, %v), want zero false", snap, ok)
	}
}

func TestSession_ProjectSnapshot_WithProjectIsDefensive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test/appstate\n\ngo 1.25\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")

	p, err := project.Open(context.Background(), root, project.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s := newRegisteredSession(t, Options{Project: p})

	snap, ok := s.ProjectSnapshot()
	if !ok {
		t.Fatal("ProjectSnapshot ok = false")
	}
	if snap.Root == "" || len(snap.Modules) != 1 || len(snap.Packages) == 0 {
		t.Fatalf("unexpected ProjectSnapshot: %+v", snap)
	}
	snap.Modules[0].Path = "mutated"
	snap.Packages[0].ImportPath = "mutated"
	if len(snap.Packages[0].Files) > 0 {
		snap.Packages[0].Files[0] = "mutated.go"
	}

	next, ok := s.ProjectSnapshot()
	if !ok {
		t.Fatal("second ProjectSnapshot ok = false")
	}
	if next.Modules[0].Path == "mutated" {
		t.Fatal("ProjectSnapshot modules returned mutable internal state")
	}
	if next.Packages[0].ImportPath == "mutated" {
		t.Fatal("ProjectSnapshot packages returned mutable internal state")
	}
	for _, file := range next.Packages[0].Files {
		if file == "mutated.go" {
			t.Fatal("ProjectSnapshot package files returned mutable internal state")
		}
	}
}

func TestSession_ProjectSnapshot_AfterRefreshSeesNewPackages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test/apprefreshstate\n\ngo 1.25\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")

	p, err := project.Open(context.Background(), root, project.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s := newRegisteredSession(t, Options{Project: p})

	extraDir := filepath.Join(root, "pkg", "extra")
	writeFile(t, filepath.Join(extraDir, "extra.go"), "package extra\n\nfunc Extra() {}\n")
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	snap, ok := s.ProjectSnapshot()
	if !ok {
		t.Fatal("ProjectSnapshot ok = false")
	}
	if !hasPackage(snap.Packages, "example.test/apprefreshstate/pkg/extra") {
		t.Fatalf("Snapshot missing refreshed package: %+v", snap.Packages)
	}
}

func TestSession_SubscribeEditorEvents(t *testing.T) {
	s := newRegisteredSession(t, Options{})
	events, cancel := s.SubscribeEditorEvents(1)
	defer cancel()

	s.Editor().NewBuffer("hello")
	select {
	case ev := <-events:
		if _, ok := ev.(editor.BufferOpened); !ok {
			t.Fatalf("event = %T, want BufferOpened", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for editor event")
	}

	cancel()
	s.Editor().NewBuffer("ignored")
	select {
	case ev := <-events:
		t.Fatalf("received event after cancel: %T", ev)
	case <-time.After(20 * time.Millisecond):
	}
}
