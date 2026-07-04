//go:build windows

package terminal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// errConPTYUnsupported wraps failures indicating the host Windows
// predates the pseudo console API (Windows 10 1809 / build 17763).
var errConPTYUnsupported = errors.New("terminal: ConPTY is not supported on this Windows version")

// maxConPTYDim is the largest dimension ResizePseudoConsole accepts;
// windows.Coord fields are int16.
const maxConPTYDim = 32767

// windowsPTY runs a child process attached to a Windows pseudo console
// (ConPTY). The parent talks to the child through two anonymous pipes:
// input (keyboard bytes written to the console) and output (the VT
// stream the console renders).
type windowsPTY struct {
	input  *os.File // write side of the console input pipe
	output *os.File // read side of the console output pipe
	proc   windows.Handle

	// consoleMu guards console access so Resize never races the
	// one-shot ClosePseudoConsole.
	consoleMu     sync.Mutex
	console       windows.Handle
	consoleClosed bool

	// waitOnce runs the process reap exactly once; concurrent Wait
	// and Close callers share the result via waitDone.
	waitOnce sync.Once
	waitDone chan struct{}
	exitCode int
	waitErr  error

	closeOnce sync.Once
	closeErr  error
}

// startPTY launches argv on a fresh ConPTY. The context is honored
// only before any OS resources are created.
func startPTY(ctx context.Context, cfg ptyConfig) (ptyBackend, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := checkConPTY(); err != nil {
		return nil, err
	}

	argv := cfg.argv
	if len(argv) == 0 {
		shell := os.Getenv("COMSPEC")
		if shell == "" {
			shell = "cmd.exe"
		}
		argv = []string{shell}
	}

	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("terminal: creating input pipe: %w", err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		closeHandles(inRead, inWrite)
		return nil, fmt.Errorf("terminal: creating output pipe: %w", err)
	}

	var console windows.Handle
	size := conptyCoord(cfg.cols, cfg.rows)
	if err := windows.CreatePseudoConsole(size, inRead, outWrite, 0, &console); err != nil {
		closeHandles(inRead, inWrite, outRead, outWrite)
		if isUnsupportedError(err) {
			return nil, fmt.Errorf("%w: %v", errConPTYUnsupported, err)
		}
		return nil, fmt.Errorf("terminal: creating pseudo console: %w", err)
	}
	// The pseudo console duplicated its pipe ends; release ours.
	closeHandles(inRead, outWrite)

	proc, err := startConPTYProcess(argv, cfg.dir, cfg.env, console)
	if err != nil {
		windows.ClosePseudoConsole(console)
		closeHandles(inWrite, outRead)
		return nil, err
	}

	return &windowsPTY{
		input:    os.NewFile(uintptr(inWrite), "|conptyin"),
		output:   os.NewFile(uintptr(outRead), "|conptyout"),
		proc:     proc,
		console:  console,
		waitDone: make(chan struct{}),
	}, nil
}

// checkConPTY reports errConPTYUnsupported when kernel32.dll lacks
// CreatePseudoConsole, so callers get a matchable sentinel instead of
// a crash from a missing procedure.
func checkConPTY() error {
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("CreatePseudoConsole")
	if err := proc.Find(); err != nil {
		return fmt.Errorf("%w: %v", errConPTYUnsupported, err)
	}
	return nil
}

// startConPTYProcess spawns argv attached to the pseudo console and
// returns the process handle.
func startConPTYProcess(argv []string, dir string, extraEnv []string, console windows.Handle) (windows.Handle, error) {
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(argv))
	if err != nil {
		return 0, fmt.Errorf("terminal: encoding command line: %w", err)
	}
	var dirPtr *uint16
	if dir != "" {
		dirPtr, err = windows.UTF16PtrFromString(dir)
		if err != nil {
			return 0, fmt.Errorf("terminal: encoding working directory: %w", err)
		}
	}
	envBlock, err := environmentBlock(append(os.Environ(), extraEnv...))
	if err != nil {
		return 0, err
	}

	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return 0, fmt.Errorf("terminal: allocating process attribute list: %w", err)
	}
	defer attrs.Delete()
	// PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE takes the HPCON by value in
	// the lpValue slot, per the CreatePseudoConsole sample; reinterpret
	// the handle's bits as the pointer the API wants.
	hpcon := console
	if err := attrs.Update(
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		*(*unsafe.Pointer)(unsafe.Pointer(&hpcon)),
		unsafe.Sizeof(hpcon),
	); err != nil {
		return 0, fmt.Errorf("terminal: attaching pseudo console: %w", err)
	}

	si := new(windows.StartupInfoEx)
	si.Cb = uint32(unsafe.Sizeof(*si))
	si.ProcThreadAttributeList = attrs.List()
	// When the parent's standard handles are redirected (pipes, as
	// under go test or an IDE), CreateProcess silently duplicates them
	// into the child even with bInheritHandles=FALSE, and the child
	// writes past the pseudoconsole. USESTDHANDLES with NULL handles
	// suppresses that duplication so the child falls back to the
	// pseudoconsole for all three streams.
	si.Flags |= windows.STARTF_USESTDHANDLES

	var pi windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)
	if err := windows.CreateProcess(nil, cmdline, nil, nil, false, flags, envBlock, dirPtr, &si.StartupInfo, &pi); err != nil {
		return 0, fmt.Errorf("terminal: creating process %q: %w", argv[0], err)
	}
	_ = windows.CloseHandle(pi.Thread)
	return pi.Process, nil
}

// environmentBlock builds the double-NUL-terminated UTF-16 block
// CreateProcess expects (with CREATE_UNICODE_ENVIRONMENT).
func environmentBlock(env []string) (*uint16, error) {
	block := make([]uint16, 0, 2048)
	for _, e := range env {
		if e == "" {
			continue
		}
		u, err := windows.UTF16FromString(e) // NUL-terminated
		if err != nil {
			return nil, fmt.Errorf("terminal: invalid environment entry %q: %w", e, err)
		}
		block = append(block, u...)
	}
	block = append(block, 0)
	if len(block) == 1 {
		block = append(block, 0)
	}
	return &block[0], nil
}

// Read returns the console's output stream. After the child exits and
// Wait closes the pseudo console, the pipe drains and reads report
// EOF (broken pipe).
func (p *windowsPTY) Read(b []byte) (int, error) {
	return p.output.Read(b)
}

// Write delivers keyboard input to the console.
func (p *windowsPTY) Write(b []byte) (int, error) {
	return p.input.Write(b)
}

// Resize changes the pseudo console dimensions. Once the console has
// been closed it is a no-op.
func (p *windowsPTY) Resize(cols, rows int) error {
	p.consoleMu.Lock()
	defer p.consoleMu.Unlock()
	if p.consoleClosed {
		return nil
	}
	if err := windows.ResizePseudoConsole(p.console, conptyCoord(cols, rows)); err != nil {
		return fmt.Errorf("terminal: resizing pseudo console: %w", err)
	}
	return nil
}

// Wait blocks until the child exits, records its exit code, and then
// closes the pseudo console so conhost flushes any remaining output
// and the reader drains to EOF. Concurrent callers share the result.
func (p *windowsPTY) Wait() (int, error) {
	p.waitOnce.Do(p.reap)
	<-p.waitDone
	return p.exitCode, p.waitErr
}

func (p *windowsPTY) reap() {
	defer close(p.waitDone)
	event, err := windows.WaitForSingleObject(p.proc, windows.INFINITE)
	switch {
	case err != nil:
		p.waitErr = fmt.Errorf("terminal: waiting for process: %w", err)
	case event != windows.WAIT_OBJECT_0:
		p.waitErr = fmt.Errorf("terminal: waiting for process: unexpected wait result %#x", event)
	default:
		var code uint32
		if err := windows.GetExitCodeProcess(p.proc, &code); err != nil {
			p.waitErr = fmt.Errorf("terminal: reading exit code: %w", err)
		} else {
			p.exitCode = int(code)
		}
	}
	p.closeConsole()
}

// closeConsole closes the pseudo console exactly once. Closing it
// terminates attached clients, flushes buffered output and closes
// conhost's pipe ends, which unblocks the session reader.
func (p *windowsPTY) closeConsole() {
	p.consoleMu.Lock()
	defer p.consoleMu.Unlock()
	if p.consoleClosed {
		return
	}
	p.consoleClosed = true
	windows.ClosePseudoConsole(p.console)
}

// Close terminates the child if it is still running, reaps it, and
// releases the console, pipes and process handle. It is idempotent;
// concurrent callers share the first result.
func (p *windowsPTY) Close() error {
	p.closeOnce.Do(func() {
		// Best effort: the process may already have exited.
		_ = windows.TerminateProcess(p.proc, 1)
		// Reap (shared with Wait); also closes the pseudo console.
		_, _ = p.Wait()
		// Closing the files is safe while a Read is in flight: the
		// console close above breaks the pipe first, and os.File
		// serializes Close against pending I/O.
		var firstErr error
		if err := p.input.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			firstErr = err
		}
		if err := p.output.Close(); err != nil && !errors.Is(err, os.ErrClosed) && firstErr == nil {
			firstErr = err
		}
		if err := windows.CloseHandle(p.proc); err != nil && firstErr == nil {
			firstErr = err
		}
		if firstErr != nil {
			p.closeErr = fmt.Errorf("terminal: closing pty: %w", firstErr)
		}
	})
	return p.closeErr
}

func conptyCoord(cols, rows int) windows.Coord {
	return windows.Coord{
		X: int16(clampInt(cols, 1, maxConPTYDim)),
		Y: int16(clampInt(rows, 1, maxConPTYDim)),
	}
}

func closeHandles(handles ...windows.Handle) {
	for _, h := range handles {
		_ = windows.CloseHandle(h)
	}
}

// isUnsupportedError reports whether err smells like "this Windows
// cannot do ConPTY" rather than a transient failure.
func isUnsupportedError(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_SUPPORTED) ||
		errors.Is(err, windows.ERROR_INVALID_FUNCTION) ||
		errors.Is(err, windows.ERROR_PROC_NOT_FOUND)
}
