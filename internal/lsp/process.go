package lsp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/Ondotteess/kleiber/internal/logging"
)

// DefaultStopGracePeriod is how long Process.Stop waits for a clean exit
// after closing stdin before sending SIGKILL / TerminateProcess. gopls
// shuts down in well under a second in practice; three seconds gives a
// generous margin for slow CI hosts.
const DefaultStopGracePeriod = 3 * time.Second

// stderrScannerBuf is the initial size of the stderr line scanner's
// buffer; stderrScannerMax bounds growth. gopls's stderr output is
// short, but a stuck startup error message can be long.
const (
	stderrScannerBuf = 64 * 1024
	stderrScannerMax = 1 * 1024 * 1024
)

// ErrEmptyBinary is returned by Start when ProcessOptions.Binary is the
// empty string.
var ErrEmptyBinary = errors.New("lsp: empty subprocess binary path")

// ProcessOptions configures a subprocess managed by Process.
type ProcessOptions struct {
	// Binary is the path to the executable (absolute, or resolvable by
	// the OS PATH lookup that os/exec performs).
	Binary string

	// Args are the command-line arguments passed after Binary.
	Args []string

	// Dir is the working directory. Empty means inherit from parent.
	Dir string

	// Env, when non-nil, replaces the inherited environment.
	Env []string

	// Logger receives structured records. Nil means discard.
	Logger *slog.Logger

	// Name is a short label attached to log records (e.g., "gopls").
	// Empty defaults to "subprocess".
	Name string

	// GracePeriod is how long Stop waits between closing stdin and
	// killing the process. Non-positive values use DefaultStopGracePeriod.
	GracePeriod time.Duration
}

// Process supervises a long-running subprocess that speaks any
// stdio-based protocol. It knows nothing about LSP or JSON-RPC: the
// caller wraps Stdin and Stdout in a Conn (or any other I/O pair) to
// drive the protocol.
//
// A Process is one-shot. After Wait or Stop returns, the Process is
// terminal; call Start again on a fresh Options to launch another.
//
// Process is safe for concurrent reads of its accessors; concurrent
// Stop calls coalesce.
type Process struct {
	logger *slog.Logger
	name   string
	grace  time.Duration

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	// waitErr is set inside the wait goroutine before waitDone closes.
	// Any code reading waitErr must first observe waitDone closed.
	waitErr  error
	waitDone chan struct{}

	stderrDone chan struct{}

	stopped atomic.Bool
}

// Start spawns the subprocess described by opts.
//
// The returned Process owns the child until Stop, Wait, or process exit
// returns. Closing stdin alone is not enough to release resources —
// callers must read from Stdout until EOF (or until Stop) so the
// supervisor goroutines can drain.
//
// The supplied context is honored only during the start phase; once
// the child is running, lifetime is controlled via Stop. This is
// intentional: tying child lifetime to a long-lived ctx leads to abrupt
// SIGKILL on cancellation, defeating the grace-period contract.
func Start(ctx context.Context, opts ProcessOptions) (*Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.Binary == "" {
		return nil, ErrEmptyBinary
	}
	logger := opts.Logger
	if logger == nil {
		logger = logging.Discard()
	}
	name := opts.Name
	if name == "" {
		name = "subprocess"
	}
	grace := opts.GracePeriod
	if grace <= 0 {
		grace = DefaultStopGracePeriod
	}

	cmd := exec.Command(opts.Binary, opts.Args...)
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	if opts.Env != nil {
		cmd.Env = opts.Env
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("lsp: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("lsp: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("lsp: starting %s: %w", opts.Binary, err)
	}

	p := &Process{
		logger:     logger.With("subprocess", name),
		name:       name,
		grace:      grace,
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdout,
		waitDone:   make(chan struct{}),
		stderrDone: make(chan struct{}),
	}

	go p.pumpStderr(stderr)
	go p.waitForExit()

	p.logger.Info("subprocess started", "pid", cmd.Process.Pid, "binary", opts.Binary)
	return p, nil
}

// Stdin returns the writer connected to the subprocess's stdin. It is
// safe for concurrent use insofar as the underlying pipe is.
func (p *Process) Stdin() io.Writer { return p.stdin }

// Stdout returns the reader connected to the subprocess's stdout.
// Reads must be serialized by the caller.
func (p *Process) Stdout() io.Reader { return p.stdout }

// Pid returns the subprocess's process ID.
func (p *Process) Pid() int {
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Wait blocks until the subprocess exits and returns the wait error
// (nil for a clean exit, *exec.ExitError otherwise).
func (p *Process) Wait() error {
	<-p.waitDone
	return p.waitErr
}

// Stop shuts the subprocess down gracefully.
//
// Sequence:
//  1. Close stdin so the child observes EOF and can exit cleanly.
//  2. Wait up to GracePeriod (or until ctx fires) for a clean exit.
//  3. If the deadline expires, send Kill and wait for exit.
//
// Returns the wait error (nil on clean exit, *exec.ExitError otherwise).
// Concurrent Stop calls coalesce: the first does the work; the rest
// block until the first returns and share its result.
func (p *Process) Stop(ctx context.Context) error {
	if !p.stopped.CompareAndSwap(false, true) {
		// Another caller is already stopping. Wait for the result.
		select {
		case <-p.waitDone:
			<-p.stderrDone
			return p.waitErr
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if err := p.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		p.logger.Debug("closing stdin", "err", err)
	}

	graceCtx, cancel := context.WithTimeout(ctx, p.grace)
	defer cancel()

	select {
	case <-p.waitDone:
		// Clean exit within grace.
	case <-graceCtx.Done():
		// Grace expired (or the caller's ctx fired earlier) — kill.
		p.logger.Warn("subprocess did not exit in grace period; killing", "grace", p.grace)
		if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			p.logger.Warn("killing subprocess", "err", err)
		}
		// Wait for the wait goroutine to observe the kill.
		select {
		case <-p.waitDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Drain stderr pump too, so callers can be sure no goroutines
	// outlive this call.
	<-p.stderrDone
	return p.waitErr
}

// pumpStderr scans the subprocess's stderr line-by-line and forwards
// each line to the logger at Debug level. It runs until stderr reaches
// EOF (the child exited or closed stderr), at which point stderrDone is
// closed.
func (p *Process) pumpStderr(stderr io.Reader) {
	defer close(p.stderrDone)
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, stderrScannerBuf), stderrScannerMax)
	for scanner.Scan() {
		p.logger.Debug("subprocess stderr", "line", scanner.Text())
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		p.logger.Warn("subprocess stderr scanner error", "err", err)
	}
}

// waitForExit blocks on cmd.Wait() and stores the result for Wait/Stop
// readers. After it returns, waitDone is closed.
func (p *Process) waitForExit() {
	defer close(p.waitDone)
	p.waitErr = p.cmd.Wait()
	if p.waitErr != nil {
		// Distinguish "expected exit code != 0" from a real launch
		// failure by checking for *exec.ExitError specifically.
		var exitErr *exec.ExitError
		if errors.As(p.waitErr, &exitErr) {
			p.logger.Info("subprocess exited with non-zero status",
				"code", exitErr.ExitCode())
		} else {
			p.logger.Warn("subprocess wait failed", "err", p.waitErr)
		}
		return
	}
	p.logger.Info("subprocess exited cleanly")
}
