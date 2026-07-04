//go:build !windows

package terminal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

// unixPTY runs a child process on a Unix pseudo terminal opened by
// creack/pty. The child is a session leader on its own controlling
// TTY, so Close can signal the whole process group.
type unixPTY struct {
	cmd *exec.Cmd
	f   *os.File // pty master

	mu     sync.Mutex
	closed bool

	closeOnce sync.Once
	closeErr  error
}

// startPTY launches argv on a fresh pseudo terminal. The context is
// honored only before the process starts.
func startPTY(ctx context.Context, cfg ptyConfig) (ptyBackend, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	argv := cfg.argv
	if len(argv) == 0 {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		argv = []string{shell}
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cfg.dir
	env := append(os.Environ(), cfg.env...)
	if !hasEnvKey(env, "TERM") {
		// Full-screen applications need a terminfo entry; xterm-256color
		// matches the sequences Parser understands.
		env = append(env, "TERM=xterm-256color")
	}
	cmd.Env = env
	// Session leader with a controlling TTY, per the creack/pty start
	// path; being explicit documents that Close relies on it for the
	// process-group kill.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}

	f, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(clampInt(cfg.rows, 1, 65535)),
		Cols: uint16(clampInt(cfg.cols, 1, 65535)),
	})
	if err != nil {
		return nil, fmt.Errorf("terminal: starting pty process %q: %w", argv[0], err)
	}
	return &unixPTY{cmd: cmd, f: f}, nil
}

// Read returns the application's output stream. After the child exits
// (EIO on Linux) or Close, reads report an error and the session
// reader stops.
func (p *unixPTY) Read(b []byte) (int, error) {
	return p.f.Read(b)
}

// Write delivers keyboard input to the terminal.
func (p *unixPTY) Write(b []byte) (int, error) {
	return p.f.Write(b)
}

// Resize changes the terminal dimensions and signals the foreground
// process group with SIGWINCH (via the tty driver). After Close it is
// a no-op.
func (p *unixPTY) Resize(cols, rows int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	ws := &pty.Winsize{
		Rows: uint16(clampInt(rows, 1, 65535)),
		Cols: uint16(clampInt(cols, 1, 65535)),
	}
	if err := pty.Setsize(p.f, ws); err != nil {
		return fmt.Errorf("terminal: resizing pty: %w", err)
	}
	return nil
}

// Wait reaps the child and returns its exit code; a child killed by a
// signal reports -1, following os.ProcessState. Session calls Wait
// exactly once.
func (p *unixPTY) Wait() (int, error) {
	err := p.cmd.Wait()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 0, fmt.Errorf("terminal: waiting for process: %w", err)
	}
	return 0, nil
}

// Close kills the child's process group and closes the PTY master,
// which unblocks any pending Read. It is idempotent; reaping is left
// to Wait.
func (p *unixPTY) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()

		if p.cmd.Process != nil {
			// The child is the group leader (Setsid), so a negative
			// pid signals the whole group. Fall back to a plain kill
			// if the group is already gone.
			if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL); err != nil {
				_ = p.cmd.Process.Kill()
			}
		}
		if err := p.f.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			p.closeErr = fmt.Errorf("terminal: closing pty: %w", err)
		}
	})
	return p.closeErr
}

func hasEnvKey(env []string, key string) bool {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}
