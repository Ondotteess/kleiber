package lsp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeEnvVar lets the test binary act as its own subprocess stub.
// process_test.go's TestMain inspects this variable on every invocation;
// when set, the binary executes a small canned behavior instead of
// running tests. This is the standard os/exec testing pattern — see
// the Go standard library's own exec_test.go.
const fakeEnvVar = "KLEIBER_LSP_FAKE"

// TestMain is the entry point of the lsp test binary. When fakeEnvVar
// is set, the binary runs as a stub; otherwise it runs the normal test
// suite.
func TestMain(m *testing.M) {
	switch os.Getenv(fakeEnvVar) {
	case "":
		os.Exit(m.Run())
	case "echo":
		// Copy stdin -> stdout until EOF, then exit cleanly. Closing
		// our stdin from the parent triggers a clean shutdown.
		_, _ = io.Copy(os.Stdout, os.Stdin)
		os.Exit(0)
	case "stderr_then_exit":
		_, _ = os.Stderr.WriteString("hello from stub\n")
		os.Exit(0)
	case "exit7":
		os.Exit(7)
	case "ignore_stdin":
		// Block forever; the parent will kill us after its grace
		// period expires.
		select {}
	default:
		_, _ = os.Stderr.WriteString("unknown KLEIBER_LSP_FAKE\n")
		os.Exit(99)
	}
}

// stubOptions returns ProcessOptions that spawn this very test binary
// in the given stub mode. Tests stay self-contained and cross-platform:
// no external helper executables, no shell.
func stubOptions(t *testing.T, mode string) ProcessOptions {
	t.Helper()
	bin := os.Args[0]
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("test binary %s not stat-able: %v", bin, err)
	}
	// Pass an absolute path so we don't depend on cwd / PATH resolution.
	abs, err := filepath.Abs(bin)
	if err != nil {
		t.Fatalf("resolving %s: %v", bin, err)
	}
	env := append(os.Environ(), fakeEnvVar+"="+mode)
	return ProcessOptions{
		Binary:      abs,
		Env:         env,
		GracePeriod: 200 * time.Millisecond,
	}
}

// stopOnCleanup arranges for the process to be killed at test end so a
// hung child cannot leak across tests.
func stopOnCleanup(t *testing.T, p *Process) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.Stop(ctx)
	})
}

func TestProcess_Start_EmptyBinary_Errors(t *testing.T) {
	ctx := context.Background()
	_, err := Start(ctx, ProcessOptions{})
	if !errors.Is(err, ErrEmptyBinary) {
		t.Errorf("err = %v, want ErrEmptyBinary", err)
	}
}

func TestProcess_Start_MissingBinary_Errors(t *testing.T) {
	ctx := context.Background()
	_, err := Start(ctx, ProcessOptions{Binary: filepath.Join(t.TempDir(), "does-not-exist")})
	if err == nil {
		t.Fatal("Start: nil error, want spawn failure")
	}
}

func TestProcess_Start_CanceledContext_Errors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Start(ctx, ProcessOptions{Binary: "anything"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestProcess_Start_RunsAndStops(t *testing.T) {
	ctx := context.Background()
	p, err := Start(ctx, stubOptions(t, "echo"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopOnCleanup(t, p)

	if p.Pid() <= 0 {
		t.Errorf("Pid = %d, want >0", p.Pid())
	}

	if err := p.Stop(ctx); err != nil {
		t.Errorf("Stop returned: %v (expected nil for clean exit)", err)
	}
}

func TestProcess_Stop_Idempotent(t *testing.T) {
	ctx := context.Background()
	p, err := Start(ctx, stubOptions(t, "echo"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopOnCleanup(t, p)
	if err := p.Stop(ctx); err != nil {
		t.Errorf("first Stop: %v", err)
	}
	// Second Stop should not panic and should return the same nil
	// (clean exit) result.
	if err := p.Stop(ctx); err != nil {
		t.Errorf("second Stop: %v", err)
	}
}

func TestProcess_Stop_KillsIfStubIgnoresStdin(t *testing.T) {
	ctx := context.Background()
	opts := stubOptions(t, "ignore_stdin")
	opts.GracePeriod = 100 * time.Millisecond
	p, err := Start(ctx, opts)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopOnCleanup(t, p)

	start := time.Now()
	stopCtx, stopCancel := context.WithTimeout(ctx, 5*time.Second)
	defer stopCancel()
	err = p.Stop(stopCtx)
	elapsed := time.Since(start)

	// We expect an *exec.ExitError (the kill terminated the process
	// non-cleanly), not a ctx.DeadlineExceeded.
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("err = %v, want *exec.ExitError after kill", err)
	}
	// Elapsed should be in the ballpark of GracePeriod, well under
	// the 5s ctx deadline. Allow generous slack for slow CI.
	if elapsed > 2*time.Second {
		t.Errorf("Stop took %v, want <2s", elapsed)
	}
}

func TestProcess_Wait_ReturnsExitCode(t *testing.T) {
	ctx := context.Background()
	p, err := Start(ctx, stubOptions(t, "exit7"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopOnCleanup(t, p)

	werr := p.Wait()
	var exitErr *exec.ExitError
	if !errors.As(werr, &exitErr) {
		t.Fatalf("Wait err = %v, want *exec.ExitError", werr)
	}
	if exitErr.ExitCode() != 7 {
		t.Errorf("ExitCode = %d, want 7", exitErr.ExitCode())
	}
}

func TestProcess_Stderr_PipedToLogger(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx := context.Background()
	opts := stubOptions(t, "stderr_then_exit")
	opts.Logger = logger
	opts.Name = "stub"
	p, err := Start(ctx, opts)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopOnCleanup(t, p)

	// Stop blocks until both the process has exited and the stderr
	// pump has drained, so logBuf is final by the time it returns.
	if err := p.Stop(ctx); err != nil {
		t.Errorf("Stop: %v", err)
	}

	if !strings.Contains(logBuf.String(), "hello from stub") {
		t.Errorf("log buffer missing stderr line; got:\n%s", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "subprocess=stub") {
		t.Errorf("log buffer missing subprocess attribute; got:\n%s", logBuf.String())
	}
}

func TestProcess_StdinStdout_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p, err := Start(ctx, stubOptions(t, "echo"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopOnCleanup(t, p)

	const payload = "ping\n"
	if _, err := io.WriteString(p.Stdin(), payload); err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	// Read exactly len(payload) bytes back.
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(p.Stdout(), got); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	if string(got) != payload {
		t.Errorf("got %q, want %q", got, payload)
	}

	// Now closing stdin should let the echo stub exit cleanly.
	if err := p.Stop(ctx); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestProcess_ConnIntegration_RoundTripsJSONRPC(t *testing.T) {
	// End-to-end check: Conn over Process stdio. Echo stub mirrors
	// whatever frame we send back, so writing a Notification and
	// reading the result must yield an identical Notification.
	ctx := context.Background()
	p, err := Start(ctx, stubOptions(t, "echo"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopOnCleanup(t, p)

	conn := NewConn(ConnOptions{Reader: p.Stdout(), Writer: p.Stdin()})

	want := &Notification{
		Method: "textDocument/didOpen",
		Params: []byte(`{"uri":"file:///x.go"}`),
	}
	if err := conn.Write(want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := conn.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	n, ok := got.(*Notification)
	if !ok {
		t.Fatalf("got %T, want *Notification", got)
	}
	if n.Method != want.Method {
		t.Errorf("Method = %q, want %q", n.Method, want.Method)
	}
}
