package ui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Ondotteess/kleiber/internal/app"
)

func TestNewShell_NilInputsError(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	presenter, controller := newPresenterController(t, session)
	defer presenter.Close()

	if _, err := NewShell(nil, controller, ShellOptions{}); !errors.Is(err, ErrNilPresenter) {
		t.Fatalf("nil presenter err = %v, want ErrNilPresenter", err)
	}
	if _, err := NewShell(presenter, nil, ShellOptions{}); !errors.Is(err, ErrNilController) {
		t.Fatalf("nil controller err = %v, want ErrNilController", err)
	}

	var shell *Shell
	if err := shell.Refresh(context.Background()); !errors.Is(err, ErrNilShell) {
		t.Fatalf("nil shell Refresh err = %v, want ErrNilShell", err)
	}
}

func TestShell_StateDelegatesToPresenterAndIsDefensive(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	presenter, controller := newPresenterController(t, session)
	shell, err := NewShell(presenter, controller, ShellOptions{Title: "Test Shell"})
	if err != nil {
		t.Fatalf("NewShell: %v", err)
	}
	defer shell.Close()

	if err := controller.NewBuffer(context.Background(), "draft"); err != nil {
		t.Fatalf("NewBuffer: %v", err)
	}
	waitForUpdate(t, presenter)
	if err := shell.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	state := shell.State()
	if len(state.Buffers) != 1 {
		t.Fatalf("Buffers len = %d, want 1", len(state.Buffers))
	}
	state.Buffers[0].DisplayName = "mutated"
	next := shell.State()
	if next.Buffers[0].DisplayName == "mutated" {
		t.Fatal("Shell.State returned mutable state")
	}

	snap := shell.Snapshot()
	if snap.Title != "Test Shell" {
		t.Fatalf("Snapshot.Title = %q, want Test Shell", snap.Title)
	}
	snap.State.Buffers[0].DisplayName = "mutated again"
	if shell.Snapshot().State.Buffers[0].DisplayName == "mutated again" {
		t.Fatal("Shell.Snapshot returned mutable state")
	}
}

func TestShell_RefreshRebuildsPresenterState(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	presenter, controller := newPresenterController(t, session)
	shell, err := NewShell(presenter, controller, ShellOptions{})
	if err != nil {
		t.Fatalf("NewShell: %v", err)
	}
	defer shell.Close()

	if err := controller.NewBuffer(context.Background(), "draft"); err != nil {
		t.Fatalf("NewBuffer: %v", err)
	}
	waitForUpdate(t, presenter)
	if !shell.Dirty() {
		t.Fatal("Dirty = false after presenter update")
	}
	if len(shell.State().Buffers) != 0 {
		t.Fatal("State updated before Refresh; shell should delegate presenter snapshot")
	}
	if err := shell.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if shell.Dirty() {
		t.Fatal("Dirty = true after Refresh")
	}
	if got := shell.State().Buffers; len(got) != 1 {
		t.Fatalf("Buffers len after Refresh = %d, want 1", len(got))
	}
}

func TestShell_UpdatesDelegateAndCloseIsIdempotent(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	presenter, controller := newPresenterController(t, session)
	shell, err := NewShell(presenter, controller, ShellOptions{})
	if err != nil {
		t.Fatalf("NewShell: %v", err)
	}

	updates := shell.Updates()
	if err := controller.NewBuffer(context.Background(), "draft"); err != nil {
		t.Fatalf("NewBuffer: %v", err)
	}
	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for shell update")
	}

	shell.Close()
	shell.Close()
	snap := shell.Snapshot()
	if !snap.Closed {
		t.Fatal("Snapshot.Closed = false after Close")
	}
	select {
	case _, ok := <-updates:
		if ok {
			t.Fatal("Updates yielded value after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("Updates was not closed")
	}
}

func TestShell_DefaultTitleAndNilSafeAccessors(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	presenter, controller := newPresenterController(t, session)
	shell, err := NewShell(presenter, controller, ShellOptions{})
	if err != nil {
		t.Fatalf("NewShell: %v", err)
	}
	defer shell.Close()

	if got := shell.Snapshot().Title; got != defaultShellTitle {
		t.Fatalf("default title = %q, want %q", got, defaultShellTitle)
	}

	var nilShell *Shell
	if got := nilShell.State(); len(got.Commands) != 0 {
		t.Fatalf("nil shell State = %+v, want zero", got)
	}
	if nilShell.Dirty() {
		t.Fatal("nil shell Dirty = true")
	}
	updates := nilShell.Updates()
	select {
	case _, ok := <-updates:
		if ok {
			t.Fatal("nil shell Updates yielded value")
		}
	case <-time.After(time.Second):
		t.Fatal("nil shell Updates did not close")
	}
}

func newPresenterController(t *testing.T, session *app.Session) (*Presenter, *Controller) {
	t.Helper()
	presenter, err := NewPresenter(session, PresenterOptions{})
	if err != nil {
		t.Fatalf("NewPresenter: %v", err)
	}
	controller, err := NewController(presenter, session, ControllerOptions{})
	if err != nil {
		presenter.Close()
		t.Fatalf("NewController: %v", err)
	}
	return presenter, controller
}
