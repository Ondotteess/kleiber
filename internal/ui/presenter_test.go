package ui

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ondotteess/kleiber/internal/app"
	"github.com/Ondotteess/kleiber/internal/editor"
)

func TestNewPresenter_NilSessionError(t *testing.T) {
	_, err := NewPresenter(nil, PresenterOptions{})
	if !errors.Is(err, ErrNilSession) {
		t.Fatalf("NewPresenter err = %v, want ErrNilSession", err)
	}
}

func TestPresenter_InitialStateMatchesBuildStateAndIsDefensive(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	presenter, err := NewPresenter(session, PresenterOptions{})
	if err != nil {
		t.Fatalf("NewPresenter: %v", err)
	}
	defer presenter.Close()

	want, err := BuildState(session)
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	got := presenter.State()
	if len(got.Commands) != len(want.Commands) {
		t.Fatalf("commands len = %d, want %d", len(got.Commands), len(want.Commands))
	}
	got.Commands[0].Name = "mutated"
	next := presenter.State()
	if next.Commands[0].Name == "mutated" {
		t.Fatal("Presenter.State returned mutable state")
	}
}

func TestPresenter_EditorEventMarksDirtyAndRefreshUpdatesState(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	presenter, err := NewPresenter(session, PresenterOptions{})
	if err != nil {
		t.Fatalf("NewPresenter: %v", err)
	}
	defer presenter.Close()

	if presenter.Dirty() {
		t.Fatal("Dirty = true before events")
	}
	path := filepath.Join(t.TempDir(), "main.go")
	writeFile(t, path, "package main\n")
	if err := session.Dispatcher().Dispatch(context.Background(), app.CommandOpenFile, map[string]any{"path": path}); err != nil {
		t.Fatalf("Dispatch openFile: %v", err)
	}
	waitForUpdate(t, presenter)
	if !presenter.Dirty() {
		t.Fatal("Dirty = false after editor event")
	}
	if len(presenter.State().Buffers) != 0 {
		t.Fatal("State updated before Refresh; presenter should only mark dirty")
	}
	if err := presenter.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if presenter.Dirty() {
		t.Fatal("Dirty = true after Refresh")
	}
	state := presenter.State()
	if len(state.Buffers) != 1 || state.Buffers[0].DisplayName != "main.go" {
		t.Fatalf("state buffers = %+v, want main.go", state.Buffers)
	}
}

func TestPresenter_CloseStopsUpdatesAndIsIdempotent(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	presenter, err := NewPresenter(session, PresenterOptions{})
	if err != nil {
		t.Fatalf("NewPresenter: %v", err)
	}
	updates := presenter.Updates()
	presenter.Close()
	presenter.Close()

	select {
	case _, ok := <-updates:
		if ok {
			t.Fatal("Updates received value after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("Updates was not closed")
	}

	session.Editor().NewBuffer("ignored")
	select {
	case _, ok := <-updates:
		if ok {
			t.Fatal("Updates received event after Close")
		}
	default:
	}
}

func TestPresenter_NoReceiverDoesNotBlockEditorMutation(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	presenter, err := NewPresenter(session, PresenterOptions{
		EventBuffer:  1,
		UpdateBuffer: 1,
	})
	if err != nil {
		t.Fatalf("NewPresenter: %v", err)
	}
	defer presenter.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 128; i++ {
			session.Editor().NewBuffer("burst")
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("editor mutations blocked with no update receiver")
	}
}

func TestPresenter_RefreshRespectsContext(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	presenter, err := NewPresenter(session, PresenterOptions{})
	if err != nil {
		t.Fatalf("NewPresenter: %v", err)
	}
	defer presenter.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := presenter.Refresh(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Refresh err = %v, want context.Canceled", err)
	}
}

func waitForUpdate(t *testing.T, presenter *Presenter) {
	t.Helper()
	select {
	case <-presenter.Updates():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for presenter update")
	}
}

func TestPresenter_StateNilSafe(t *testing.T) {
	var presenter *Presenter
	if got := presenter.State(); len(got.Commands) != 0 {
		t.Fatalf("nil presenter State = %+v, want zero", got)
	}
	if presenter.Dirty() {
		t.Fatal("nil presenter Dirty = true")
	}
	updates := presenter.Updates()
	select {
	case _, ok := <-updates:
		if ok {
			t.Fatal("nil presenter Updates yielded value")
		}
	case <-time.After(time.Second):
		t.Fatal("nil presenter Updates did not close")
	}
}

func TestPresenter_RefreshNilSafe(t *testing.T) {
	var presenter *Presenter
	err := presenter.Refresh(context.Background())
	if !errors.Is(err, ErrNilSession) {
		t.Fatalf("nil presenter Refresh err = %v, want ErrNilSession", err)
	}
}

func TestPresenter_CloseNilSafe(t *testing.T) {
	var presenter *Presenter
	presenter.Close()
}

func TestPresenter_EventAfterCloseDoesNotPanic(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	presenter, err := NewPresenter(session, PresenterOptions{})
	if err != nil {
		t.Fatalf("NewPresenter: %v", err)
	}
	presenter.Close()
	if _, err := session.Editor().NewView(editor.BufferID(123)); !errors.Is(err, editor.ErrBufferNotFound) {
		t.Fatalf("NewView err = %v, want ErrBufferNotFound", err)
	}
}
