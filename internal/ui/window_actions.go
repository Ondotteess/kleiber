package ui

import (
	"context"
	"sync"
)

// WindowAction is a bounded action handled by the experimental window shell.
// It is intentionally window-level only: no editor text input, file-tree
// selection, or command execution is modeled here.
type WindowAction int

const (
	WindowActionNone WindowAction = iota
	WindowActionRefresh
	WindowActionQuit
	WindowActionOpenPalette
	WindowActionClosePalette
	WindowActionPaletteUp
	WindowActionPaletteDown
	WindowActionPaletteAccept
)

const (
	WindowKeyF5     = "F5"
	WindowKeyEscape = "Escape"
	WindowKeyUp     = "Up"
	WindowKeyDown   = "Down"
	WindowKeyEnter  = "Enter"
	WindowKeyP      = "P"
	WindowKeyR      = "R"
	WindowKeyQ      = "Q"
)

// WindowKeyStroke is the dependency-free key shape used for window-level
// shortcuts. Gio-specific key events are translated into this struct in a
// gio-build-only file.
type WindowKeyStroke struct {
	Name    string
	Press   bool
	Ctrl    bool
	Command bool
	Shift   bool
	Alt     bool
	Super   bool
}

// WindowActionResult describes the side effect requested by HandleWindowAction.
type WindowActionResult struct {
	RefreshRequested bool
	Quit             bool
	PaletteChanged   bool
	PaletteAccepted  bool
}

type windowRefreshTarget interface {
	Refresh(context.Context) error
}

// WindowRefreshScheduler runs Shell.Refresh outside the window event path. It
// coalesces repeated requests so keyboard repeat cannot spawn unbounded
// goroutines.
type WindowRefreshScheduler struct {
	target windowRefreshTarget
	notify func()

	mu      sync.Mutex
	running bool
	pending bool
	closed  bool
	lastErr error
	wg      sync.WaitGroup
}

func (a WindowAction) String() string {
	switch a {
	case WindowActionRefresh:
		return "refresh"
	case WindowActionQuit:
		return "quit"
	case WindowActionOpenPalette:
		return "open-palette"
	case WindowActionClosePalette:
		return "close-palette"
	case WindowActionPaletteUp:
		return "palette-up"
	case WindowActionPaletteDown:
		return "palette-down"
	case WindowActionPaletteAccept:
		return "palette-accept"
	default:
		return "none"
	}
}

// WindowActionForKeyStroke maps stable window-level shortcuts to actions.
func WindowActionForKeyStroke(key WindowKeyStroke) WindowAction {
	return WindowActionForKeyStrokeWithPalette(key, false)
}

// WindowActionForKeyStrokeWithPalette maps stable window-level shortcuts to
// actions with palette priority rules. Escape closes an open palette before it
// is allowed to quit the window.
func WindowActionForKeyStrokeWithPalette(key WindowKeyStroke, paletteOpen bool) WindowAction {
	if !key.Press {
		return WindowActionNone
	}
	if key.shortcutOnly() && key.Name == WindowKeyP {
		return WindowActionOpenPalette
	}
	if paletteOpen && key.noModifiers() {
		switch key.Name {
		case WindowKeyEscape:
			return WindowActionClosePalette
		case WindowKeyUp:
			return WindowActionPaletteUp
		case WindowKeyDown:
			return WindowActionPaletteDown
		case WindowKeyEnter:
			return WindowActionPaletteAccept
		}
	}
	if key.noModifiers() {
		switch key.Name {
		case WindowKeyF5:
			return WindowActionRefresh
		case WindowKeyEscape:
			return WindowActionQuit
		}
	}
	if key.shortcutOnly() {
		switch key.Name {
		case WindowKeyR:
			return WindowActionRefresh
		case WindowKeyQ:
			return WindowActionQuit
		}
	}
	return WindowActionNone
}

// HandleWindowAction applies an already-resolved window action to the Shell.
// It never exits the process; callers decide how to close their native window.
func HandleWindowAction(ctx context.Context, shell *Shell, action WindowAction) (WindowActionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	switch action {
	case WindowActionNone:
		return WindowActionResult{}, nil
	case WindowActionRefresh:
		if shell == nil {
			return WindowActionResult{}, ErrNilShell
		}
		if err := ctx.Err(); err != nil {
			return WindowActionResult{}, err
		}
		return WindowActionResult{RefreshRequested: true}, nil
	case WindowActionQuit:
		if shell == nil {
			return WindowActionResult{}, ErrNilShell
		}
		shell.Close()
		return WindowActionResult{Quit: true}, nil
	case WindowActionOpenPalette:
		if shell == nil {
			return WindowActionResult{}, ErrNilShell
		}
		if err := shell.OpenPalette(); err != nil {
			return WindowActionResult{}, err
		}
		return WindowActionResult{PaletteChanged: true}, nil
	case WindowActionClosePalette:
		if shell == nil {
			return WindowActionResult{}, ErrNilShell
		}
		if err := shell.ClosePalette(); err != nil {
			return WindowActionResult{}, err
		}
		return WindowActionResult{PaletteChanged: true}, nil
	case WindowActionPaletteUp:
		if shell == nil {
			return WindowActionResult{}, ErrNilShell
		}
		if err := shell.MovePaletteSelection(-1); err != nil {
			return WindowActionResult{}, err
		}
		return WindowActionResult{PaletteChanged: true}, nil
	case WindowActionPaletteDown:
		if shell == nil {
			return WindowActionResult{}, ErrNilShell
		}
		if err := shell.MovePaletteSelection(1); err != nil {
			return WindowActionResult{}, err
		}
		return WindowActionResult{PaletteChanged: true}, nil
	case WindowActionPaletteAccept:
		if shell == nil {
			return WindowActionResult{}, ErrNilShell
		}
		return WindowActionResult{PaletteAccepted: true}, nil
	default:
		return WindowActionResult{}, nil
	}
}

// NewWindowRefreshScheduler creates a coalescing refresh scheduler for Shell.
func NewWindowRefreshScheduler(shell *Shell, notify func()) (*WindowRefreshScheduler, error) {
	if shell == nil {
		return nil, ErrNilShell
	}
	return newWindowRefreshScheduler(shell, notify), nil
}

func newWindowRefreshScheduler(target windowRefreshTarget, notify func()) *WindowRefreshScheduler {
	return &WindowRefreshScheduler{target: target, notify: notify}
}

// Request schedules a refresh. It returns true when a new worker was started,
// and false when the request was coalesced into an already-running refresh.
func (s *WindowRefreshScheduler) Request(ctx context.Context) bool {
	if s == nil || s.target == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	if s.running {
		s.pending = true
		s.mu.Unlock()
		return false
	}
	s.running = true
	s.wg.Add(1)
	s.mu.Unlock()

	go s.run(ctx)
	return true
}

// LastError returns the latest refresh error, if any.
func (s *WindowRefreshScheduler) LastError() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

// Close prevents future refresh requests. It does not wait for an in-flight
// refresh, so window quit cannot hang behind slow project/session work.
func (s *WindowRefreshScheduler) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	s.pending = false
	s.mu.Unlock()
}

// Wait blocks until the current worker exits. It is intended for tests.
func (s *WindowRefreshScheduler) Wait() {
	if s == nil {
		return
	}
	s.wg.Wait()
}

func (s *WindowRefreshScheduler) run(ctx context.Context) {
	defer s.wg.Done()
	for {
		err := s.target.Refresh(ctx)

		s.mu.Lock()
		s.lastErr = err
		closed := s.closed
		pending := s.pending && !closed && ctx.Err() == nil
		if pending {
			s.pending = false
		} else {
			s.running = false
		}
		notify := s.notify
		s.mu.Unlock()

		if !closed && notify != nil {
			notify()
		}
		if !pending {
			return
		}
	}
}

func (k WindowKeyStroke) noModifiers() bool {
	return !k.Ctrl && !k.Command && !k.Shift && !k.Alt && !k.Super
}

func (k WindowKeyStroke) shortcutOnly() bool {
	return (k.Ctrl || k.Command) && !k.Shift && !k.Alt && !k.Super
}
