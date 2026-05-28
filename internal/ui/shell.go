package ui

import (
	"context"
	"errors"
	"sync"
)

const defaultShellTitle = "Kleiber"

// ErrNilShell is returned when a Shell method is called on a nil receiver.
var ErrNilShell = errors.New("ui: shell is nil")

// ShellOptions configures NewShell.
type ShellOptions struct {
	// Title is metadata for future window implementations. It is not rendered
	// by this dependency-free shell boundary.
	Title string
}

// ShellState is the read-only shell snapshot a future window/render loop can
// consume. It contains no platform window handle and no renderer-owned state.
type ShellState struct {
	Title        string
	State        State
	Palette      CommandPaletteSnapshot
	Dirty        bool
	Closed       bool
	RefreshError string
}

// Shell is the dependency-free boundary a future gioui window will drive. It
// composes the Presenter read side and Controller action side, but it does not
// render, capture input, or own app state.
type Shell struct {
	presenter  *Presenter
	controller *Controller
	title      string

	mu      sync.RWMutex
	closed  bool
	palette CommandPaletteState
	once    sync.Once
}

// NewShell constructs a UI shell boundary over an existing presenter and
// controller. Ownership of application state remains in internal/app.
func NewShell(presenter *Presenter, controller *Controller, opts ShellOptions) (*Shell, error) {
	if presenter == nil {
		return nil, ErrNilPresenter
	}
	if controller == nil {
		return nil, ErrNilController
	}
	title := opts.Title
	if title == "" {
		title = defaultShellTitle
	}
	return &Shell{
		presenter:  presenter,
		controller: controller,
		title:      title,
	}, nil
}

// State returns the current UI read model from the presenter.
func (s *Shell) State() State {
	if s == nil || s.presenter == nil {
		return State{}
	}
	return s.presenter.State()
}

// Snapshot returns shell metadata plus the current defensive UI state.
func (s *Shell) Snapshot() ShellState {
	if s == nil {
		return ShellState{}
	}
	state := s.State()
	s.mu.RLock()
	title := s.title
	closed := s.closed
	palette := s.palette.Snapshot(state.Commands)
	s.mu.RUnlock()
	return ShellState{
		Title:   title,
		State:   state,
		Palette: palette,
		Dirty:   s.Dirty(),
		Closed:  closed,
	}
}

// Updates returns the presenter's coalesced update channel so a future render
// loop can wake without polling.
func (s *Shell) Updates() <-chan struct{} {
	if s == nil || s.presenter == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return s.presenter.Updates()
}

// Dirty reports whether the presenter has seen events since the last refresh.
func (s *Shell) Dirty() bool {
	if s == nil || s.presenter == nil {
		return false
	}
	return s.presenter.Dirty()
}

// Refresh rebuilds the presenter's state snapshot. Rendering is still the
// responsibility of a future frontend.
func (s *Shell) Refresh(ctx context.Context) error {
	if s == nil {
		return ErrNilShell
	}
	if s.presenter == nil {
		return ErrNilPresenter
	}
	return s.presenter.Refresh(ctx)
}

// OpenPalette shows the command palette and clamps selection to the current
// command list. It does not execute commands.
func (s *Shell) OpenPalette() error {
	if s == nil {
		return ErrNilShell
	}
	commandCount := len(s.State().Commands)
	s.mu.Lock()
	s.palette = s.palette.Opened(commandCount)
	s.mu.Unlock()
	return nil
}

// ClosePalette hides the command palette. It is safe when the palette is
// already closed.
func (s *Shell) ClosePalette() error {
	if s == nil {
		return ErrNilShell
	}
	s.mu.Lock()
	s.palette = s.palette.Closed()
	s.mu.Unlock()
	return nil
}

// MovePaletteSelection moves the command-palette selection with wraparound.
// Movement is ignored while the palette is closed.
func (s *Shell) MovePaletteSelection(delta int) error {
	if s == nil {
		return ErrNilShell
	}
	commandCount := len(s.State().Commands)
	s.mu.Lock()
	s.palette = s.palette.Move(delta, commandCount)
	s.mu.Unlock()
	return nil
}

// Close releases presenter subscriptions owned by this shell boundary. Close
// is idempotent.
func (s *Shell) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		if s.presenter != nil {
			s.presenter.Close()
		}
	})
}
