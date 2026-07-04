//go:build windows

package terminal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const sessionTestTimeout = 5 * time.Second

// startTestSession starts a session and skips the test when the host
// cannot create a ConPTY (Windows before build 17763).
func startTestSession(t *testing.T, opts Options) *Session {
	t.Helper()
	s, err := StartSession(context.Background(), opts)
	if err != nil {
		if errors.Is(err, errConPTYUnsupported) {
			t.Skipf("ConPTY unavailable: %v", err)
		}
		t.Fatalf("StartSession: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// sessionText renders the session's scrollback plus visible screen as
// one newline-joined string.
func sessionText(t *testing.T, s *Session) string {
	t.Helper()
	snap := s.Screen().Snapshot()
	var b strings.Builder
	for _, line := range s.Screen().ScrollbackLines(0, snap.ScrollbackLen) {
		b.WriteString(rowText(line))
		b.WriteByte('\n')
	}
	for _, row := range snap.Cells {
		b.WriteString(rowText(row))
		b.WriteByte('\n')
	}
	return b.String()
}

// waitForText polls the session until the marker shows up on screen or
// in scrollback, failing the test at the deadline.
func waitForText(t *testing.T, s *Session, marker string) {
	t.Helper()
	deadline := time.Now().Add(sessionTestTimeout)
	for {
		if strings.Contains(sessionText(t, s), marker) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("marker %q not found in terminal output:\n%s", marker, sessionText(t, s))
		}
		select {
		case <-s.Updates():
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestSession_StartSession_OneShotCommand(t *testing.T) {
	s := startTestSession(t, Options{
		Command: []string{"cmd", "/c", "echo kleiber-pty-ok"},
	})

	select {
	case <-s.Done():
	case <-time.After(sessionTestTimeout):
		t.Fatal("Done not closed within timeout")
	}

	waitForText(t, s, "kleiber-pty-ok")

	exited, code := s.ExitState()
	if !exited {
		t.Error("ExitState exited = false after Done")
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestSession_Write_InteractiveShell(t *testing.T) {
	s := startTestSession(t, Options{
		Command: []string{"cmd"},
		Cols:    120,
		Rows:    30,
	})

	if _, err := s.Write([]byte("echo kleiber-interactive-ok\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitForText(t, s, "kleiber-interactive-ok")

	if err := s.Resize(100, 40); err != nil {
		t.Errorf("Resize: %v", err)
	}
	if got := s.Screen().Snapshot(); got.Cols != 100 || got.Rows != 40 {
		t.Errorf("screen size after Resize = %dx%d, want 100x40", got.Cols, got.Rows)
	}

	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	select {
	case <-s.Done():
	case <-time.After(sessionTestTimeout):
		t.Fatal("Done not closed after Close")
	}

	if _, err := s.Write([]byte("x")); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Write after Close error = %v, want ErrSessionClosed", err)
	}
	if err := s.Resize(80, 24); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Resize after Close error = %v, want ErrSessionClosed", err)
	}
}
