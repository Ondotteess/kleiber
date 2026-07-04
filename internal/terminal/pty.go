package terminal

import "io"

// ptyBackend is the platform seam between Session and the operating
// system pseudo terminal. Reads deliver the application's output
// stream; writes deliver keyboard input. Implementations live in
// pty_windows.go (ConPTY) and pty_unix.go (creack/pty), which keeps
// Session platform-agnostic and lets Screen and Parser tests run
// without any real PTY.
//
// The contract Session relies on:
//
//   - Read returns io.EOF or a terminal read error after Close and,
//     once buffered output is drained, after the child exits.
//   - Wait blocks until the child exits and returns its exit code.
//     Session calls it exactly once, from its waiter goroutine.
//   - Close terminates the child if it is still running and releases
//     all OS resources. It is idempotent and safe to call while Read,
//     Write or Wait are blocked; it must unblock them.
type ptyBackend interface {
	io.Reader
	io.Writer

	// Resize changes the PTY dimensions. After Close it is a no-op.
	Resize(cols, rows int) error

	// Wait blocks until the child process exits.
	Wait() (int, error)

	// Close terminates the child and releases OS resources.
	Close() error
}

// ptyConfig carries the platform-independent inputs to startPTY.
type ptyConfig struct {
	// argv is the resolved command line; empty means the platform's
	// default shell (%COMSPEC% or $SHELL).
	argv []string

	// dir is the child's working directory; empty inherits Kleiber's.
	dir string

	// env holds extra environment entries appended to os.Environ().
	env []string

	// cols and rows are the initial PTY dimensions; startPTY callers
	// validate them.
	cols, rows int
}
