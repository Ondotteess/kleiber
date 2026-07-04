package terminal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/Ondotteess/kleiber/internal/logging"
)

// Default terminal dimensions used when Options leaves Cols or Rows
// unset.
const (
	defaultCols = 80
	defaultRows = 24
)

// readBufferSize is the chunk size of the PTY reader goroutine.
const readBufferSize = 32 * 1024

// ErrSessionClosed is returned by Session methods invoked after Close.
var ErrSessionClosed = errors.New("terminal: session closed")

// Options configures StartSession.
type Options struct {
	// Command is the argv of the program to run. Empty means the
	// platform default shell: %COMSPEC% (falling back to cmd.exe) on
	// Windows, $SHELL (falling back to /bin/sh) elsewhere.
	Command []string

	// Dir is the child's working directory. Empty inherits Kleiber's.
	Dir string

	// Env holds extra environment entries in "KEY=value" form,
	// appended to os.Environ().
	Env []string

	// Cols and Rows are the initial terminal dimensions. Values below
	// one default to 80x24.
	Cols, Rows int

	// Logger receives structured records. Nil means discard.
	Logger *slog.Logger
}

// Session runs one shell (or arbitrary command) on a PTY and keeps a
// Screen up to date with its output. One reader goroutine pumps PTY
// output through a Parser; a waiter goroutine reaps the child process.
// Both exit when the process ends or the Session is closed.
//
// Session is safe for concurrent use.
type Session struct {
	screen *Screen
	parser *Parser
	pty    ptyBackend
	logger *slog.Logger

	// updates is a 1-buffered coalescing channel: many screen changes
	// collapse into one pending signal while the UI is busy.
	updates chan struct{}

	// done closes when the child process has exited.
	done chan struct{}

	// readerDone closes when the reader goroutine has drained the PTY.
	readerDone chan struct{}

	mu       sync.Mutex
	exited   bool
	exitCode int
	closed   bool

	closeOnce sync.Once
	closeErr  error
}

// StartSession launches opts.Command on a new PTY and starts pumping
// its output into a fresh Screen. The context is honored only during
// startup; afterwards the Session's lifetime is controlled by Close
// and by the child process exiting.
func StartSession(ctx context.Context, opts Options) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cols, rows := opts.Cols, opts.Rows
	if cols < 1 {
		cols = defaultCols
	}
	if rows < 1 {
		rows = defaultRows
	}
	logger := opts.Logger
	if logger == nil {
		logger = logging.Discard()
	}

	backend, err := startPTY(ctx, ptyConfig{
		argv: opts.Command,
		dir:  opts.Dir,
		env:  opts.Env,
		cols: cols,
		rows: rows,
	})
	if err != nil {
		return nil, err
	}

	screen := NewScreen(cols, rows)
	s := &Session{
		screen: screen,
		// The PTY input doubles as the parser's reply writer so DSR
		// responses flow back to the application.
		parser:     NewParser(screen, backend),
		pty:        backend,
		logger:     logger.With("component", "terminal"),
		updates:    make(chan struct{}, 1),
		done:       make(chan struct{}),
		readerDone: make(chan struct{}),
	}
	go s.readLoop()
	go s.waitLoop()
	s.logger.Info("terminal session started", "cols", cols, "rows", rows)
	return s, nil
}

// Screen returns the live screen the session renders into. Use
// Screen.Snapshot to read it.
func (s *Session) Screen() *Screen {
	return s.screen
}

// Updates returns a channel that receives coalesced screen-changed
// signals. It is closed by Close.
func (s *Session) Updates() <-chan struct{} {
	return s.updates
}

// Done returns a channel that is closed once the child process has
// exited.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// ExitState reports whether the child process has exited and, if so,
// its exit code.
func (s *Session) ExitState() (exited bool, code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited, s.exitCode
}

// Write sends keyboard input to the PTY.
func (s *Session) Write(data []byte) (int, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return 0, ErrSessionClosed
	}
	return s.pty.Write(data)
}

// Resize changes both the screen model and the PTY to the given
// dimensions so the application receives the size change.
func (s *Session) Resize(cols, rows int) error {
	if cols < 1 || rows < 1 {
		return fmt.Errorf("terminal: invalid size %dx%d", cols, rows)
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return ErrSessionClosed
	}
	s.screen.Resize(cols, rows)
	if err := s.pty.Resize(cols, rows); err != nil {
		return fmt.Errorf("terminal: resizing pty: %w", err)
	}
	return nil
}

// Close terminates the child process if it is still running, releases
// the PTY, and joins the session's goroutines. It is idempotent:
// concurrent and repeated calls share the first call's result.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()

		err := s.pty.Close()
		<-s.readerDone
		<-s.done
		close(s.updates)
		if err != nil {
			s.closeErr = fmt.Errorf("terminal: closing pty: %w", err)
		}
		s.logger.Info("terminal session closed")
	})
	return s.closeErr
}

// readLoop pumps PTY output through the parser until the PTY reports
// EOF or an error, which happens when the child exits or the session
// closes.
func (s *Session) readLoop() {
	defer close(s.readerDone)
	buf := make([]byte, readBufferSize)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			// Parser.Write never fails.
			_, _ = s.parser.Write(buf[:n])
			s.notify()
		}
		if err != nil {
			s.logger.Debug("terminal reader finished", "err", err)
			return
		}
	}
}

// waitLoop reaps the child process, records its exit state, and closes
// done. It notifies before closing done so a consumer woken by Done
// sees the final screen state.
func (s *Session) waitLoop() {
	code, err := s.pty.Wait()
	s.mu.Lock()
	s.exited = true
	s.exitCode = code
	s.mu.Unlock()
	if err != nil {
		s.logger.Warn("terminal process wait failed", "err", err)
	} else {
		s.logger.Info("terminal process exited", "code", code)
	}
	s.notify()
	close(s.done)
}

// notify performs the non-blocking coalescing send on updates. It only
// runs from the reader and waiter goroutines, both of which are joined
// before Close closes the channel.
func (s *Session) notify() {
	select {
	case s.updates <- struct{}{}:
	default:
	}
}
