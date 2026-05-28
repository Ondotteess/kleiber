package ui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Ondotteess/kleiber/internal/app"
)

func TestWindowActionForKeyStroke(t *testing.T) {
	tests := []struct {
		name string
		key  WindowKeyStroke
		want WindowAction
	}{
		{
			name: "f5 refresh",
			key:  WindowKeyStroke{Name: WindowKeyF5, Press: true},
			want: WindowActionRefresh,
		},
		{
			name: "ctrl r refresh",
			key:  WindowKeyStroke{Name: WindowKeyR, Press: true, Ctrl: true},
			want: WindowActionRefresh,
		},
		{
			name: "command r refresh",
			key:  WindowKeyStroke{Name: WindowKeyR, Press: true, Command: true},
			want: WindowActionRefresh,
		},
		{
			name: "ctrl p opens palette",
			key:  WindowKeyStroke{Name: WindowKeyP, Press: true, Ctrl: true},
			want: WindowActionOpenPalette,
		},
		{
			name: "command p opens palette",
			key:  WindowKeyStroke{Name: WindowKeyP, Press: true, Command: true},
			want: WindowActionOpenPalette,
		},
		{
			name: "escape quit",
			key:  WindowKeyStroke{Name: WindowKeyEscape, Press: true},
			want: WindowActionQuit,
		},
		{
			name: "ctrl q quit",
			key:  WindowKeyStroke{Name: WindowKeyQ, Press: true, Ctrl: true},
			want: WindowActionQuit,
		},
		{
			name: "command q quit",
			key:  WindowKeyStroke{Name: WindowKeyQ, Press: true, Command: true},
			want: WindowActionQuit,
		},
		{
			name: "plain r ignored",
			key:  WindowKeyStroke{Name: WindowKeyR, Press: true},
			want: WindowActionNone,
		},
		{
			name: "plain q ignored",
			key:  WindowKeyStroke{Name: WindowKeyQ, Press: true},
			want: WindowActionNone,
		},
		{
			name: "plain p ignored",
			key:  WindowKeyStroke{Name: WindowKeyP, Press: true},
			want: WindowActionNone,
		},
		{
			name: "up ignored when palette closed",
			key:  WindowKeyStroke{Name: WindowKeyUp, Press: true},
			want: WindowActionNone,
		},
		{
			name: "down ignored when palette closed",
			key:  WindowKeyStroke{Name: WindowKeyDown, Press: true},
			want: WindowActionNone,
		},
		{
			name: "enter ignored when palette closed",
			key:  WindowKeyStroke{Name: WindowKeyEnter, Press: true},
			want: WindowActionNone,
		},
		{
			name: "unrelated no-mod key ignored",
			key:  WindowKeyStroke{Name: "X", Press: true},
			want: WindowActionNone,
		},
		{
			name: "repeat press maps refresh",
			key:  WindowKeyStroke{Name: WindowKeyF5, Press: true},
			want: WindowActionRefresh,
		},
		{
			name: "release ignored",
			key:  WindowKeyStroke{Name: WindowKeyF5},
			want: WindowActionNone,
		},
		{
			name: "modified f5 ignored",
			key:  WindowKeyStroke{Name: WindowKeyF5, Press: true, Ctrl: true},
			want: WindowActionNone,
		},
		{
			name: "shifted shortcut ignored",
			key:  WindowKeyStroke{Name: WindowKeyR, Press: true, Ctrl: true, Shift: true},
			want: WindowActionNone,
		},
		{
			name: "unknown shortcut ignored",
			key:  WindowKeyStroke{Name: "X", Press: true, Ctrl: true},
			want: WindowActionNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WindowActionForKeyStroke(tt.key); got != tt.want {
				t.Fatalf("WindowActionForKeyStroke(%+v) = %s, want %s", tt.key, got, tt.want)
			}
		})
	}
}

func TestWindowActionForKeyStrokeWithPalette(t *testing.T) {
	tests := []struct {
		name        string
		key         WindowKeyStroke
		paletteOpen bool
		want        WindowAction
	}{
		{
			name:        "escape closes open palette before quit",
			key:         WindowKeyStroke{Name: WindowKeyEscape, Press: true},
			paletteOpen: true,
			want:        WindowActionClosePalette,
		},
		{
			name:        "up moves open palette",
			key:         WindowKeyStroke{Name: WindowKeyUp, Press: true},
			paletteOpen: true,
			want:        WindowActionPaletteUp,
		},
		{
			name:        "down moves open palette",
			key:         WindowKeyStroke{Name: WindowKeyDown, Press: true},
			paletteOpen: true,
			want:        WindowActionPaletteDown,
		},
		{
			name:        "enter is accepted no-op while palette open",
			key:         WindowKeyStroke{Name: WindowKeyEnter, Press: true},
			paletteOpen: true,
			want:        WindowActionPaletteAccept,
		},
		{
			name:        "ctrl q still quits while palette open",
			key:         WindowKeyStroke{Name: WindowKeyQ, Press: true, Ctrl: true},
			paletteOpen: true,
			want:        WindowActionQuit,
		},
		{
			name:        "f5 still refreshes while palette open",
			key:         WindowKeyStroke{Name: WindowKeyF5, Press: true},
			paletteOpen: true,
			want:        WindowActionRefresh,
		},
		{
			name:        "modified up ignored",
			key:         WindowKeyStroke{Name: WindowKeyUp, Press: true, Ctrl: true},
			paletteOpen: true,
			want:        WindowActionNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WindowActionForKeyStrokeWithPalette(tt.key, tt.paletteOpen); got != tt.want {
				t.Fatalf("WindowActionForKeyStrokeWithPalette(%+v, %v) = %s, want %s",
					tt.key, tt.paletteOpen, got, tt.want)
			}
		})
	}
}

func TestHandleWindowAction_RefreshOnlyRequestsScheduling(t *testing.T) {
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
		t.Fatal("Dirty = false after editor event")
	}
	if got := shell.State().Buffers; len(got) != 0 {
		t.Fatalf("Buffers before refresh = %d, want 0", len(got))
	}

	result, err := HandleWindowAction(context.Background(), shell, WindowActionRefresh)
	if err != nil {
		t.Fatalf("HandleWindowAction refresh: %v", err)
	}
	if !result.RefreshRequested || result.Quit {
		t.Fatalf("refresh result = %+v, want refresh request only", result)
	}
	if !shell.Dirty() {
		t.Fatal("Dirty = false; action should not synchronously refresh shell")
	}
	if got := shell.State().Buffers; len(got) != 0 {
		t.Fatalf("Buffers after action = %d, want still stale until scheduler refresh", len(got))
	}
}

func TestHandleWindowAction_QuitClosesShellWithoutProcessExit(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	presenter, controller := newPresenterController(t, session)
	shell, err := NewShell(presenter, controller, ShellOptions{})
	if err != nil {
		t.Fatalf("NewShell: %v", err)
	}

	result, err := HandleWindowAction(context.Background(), shell, WindowActionQuit)
	if err != nil {
		t.Fatalf("HandleWindowAction quit: %v", err)
	}
	if !result.Quit || result.RefreshRequested {
		t.Fatalf("quit result = %+v, want quit only", result)
	}
	if !shell.Snapshot().Closed {
		t.Fatal("shell is not closed after quit action")
	}
	shell.Close()
}

func TestHandleWindowAction_CommandPaletteNavigation(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	presenter, controller := newPresenterController(t, session)
	shell, err := NewShell(presenter, controller, ShellOptions{})
	if err != nil {
		t.Fatalf("NewShell: %v", err)
	}
	defer shell.Close()

	result, err := HandleWindowAction(context.Background(), shell, WindowActionOpenPalette)
	if err != nil {
		t.Fatalf("open palette: %v", err)
	}
	if !result.PaletteChanged || shell.Snapshot().Palette.Open == false {
		t.Fatalf("open result = %+v snapshot = %+v", result, shell.Snapshot().Palette)
	}
	commandCount := len(shell.Snapshot().Palette.Commands)
	if commandCount == 0 {
		t.Fatal("test session has no commands")
	}

	result, err = HandleWindowAction(context.Background(), shell, WindowActionPaletteUp)
	if err != nil {
		t.Fatalf("palette up: %v", err)
	}
	if !result.PaletteChanged {
		t.Fatalf("palette up result = %+v, want PaletteChanged", result)
	}
	if got := shell.Snapshot().Palette.SelectedIndex; got != commandCount-1 {
		t.Fatalf("selected index after up = %d, want %d", got, commandCount-1)
	}

	result, err = HandleWindowAction(context.Background(), shell, WindowActionPaletteAccept)
	if err != nil {
		t.Fatalf("palette accept: %v", err)
	}
	if !result.PaletteAccepted || result.PaletteChanged || result.Quit {
		t.Fatalf("palette accept result = %+v, want accepted no-op", result)
	}
	if !shell.Snapshot().Palette.Open {
		t.Fatal("palette accept closed palette; Enter should remain execution-pending no-op")
	}

	result, err = HandleWindowAction(context.Background(), shell, WindowActionClosePalette)
	if err != nil {
		t.Fatalf("close palette: %v", err)
	}
	if !result.PaletteChanged || shell.Snapshot().Palette.Open {
		t.Fatalf("close result = %+v snapshot = %+v", result, shell.Snapshot().Palette)
	}
}

func TestHandleWindowAction_NilAndNoop(t *testing.T) {
	result, err := HandleWindowAction(context.Background(), nil, WindowActionNone)
	if err != nil {
		t.Fatalf("none action err = %v", err)
	}
	if result != (WindowActionResult{}) {
		t.Fatalf("none action result = %+v, want zero", result)
	}
	if _, err := HandleWindowAction(context.Background(), nil, WindowActionRefresh); !errors.Is(err, ErrNilShell) {
		t.Fatalf("nil refresh err = %v, want ErrNilShell", err)
	}
	if _, err := HandleWindowAction(context.Background(), nil, WindowActionQuit); !errors.Is(err, ErrNilShell) {
		t.Fatalf("nil quit err = %v, want ErrNilShell", err)
	}
}

func TestWindowRefreshScheduler_CoalescesRepeatedRequests(t *testing.T) {
	target := newBlockingRefreshTarget()
	notify := make(chan struct{}, 4)
	scheduler := newWindowRefreshScheduler(target, func() {
		select {
		case notify <- struct{}{}:
		default:
		}
	})

	if !scheduler.Request(context.Background()) {
		t.Fatal("first request did not start worker")
	}
	target.waitStarted(t)
	if scheduler.Request(context.Background()) {
		t.Fatal("second request started another worker; want coalesced")
	}
	if scheduler.Request(context.Background()) {
		t.Fatal("third request started another worker; want coalesced")
	}

	close(target.release)
	scheduler.Wait()
	if got := target.calls(); got != 2 {
		t.Fatalf("refresh calls = %d, want 2 (initial plus one coalesced pending)", got)
	}
	if got := len(notify); got != 2 {
		t.Fatalf("notify count = %d, want 2", got)
	}
}

func TestWindowRefreshScheduler_StoresLastError(t *testing.T) {
	wantErr := errors.New("refresh failed")
	target := &fakeRefreshTarget{err: wantErr}
	scheduler := newWindowRefreshScheduler(target, nil)

	if !scheduler.Request(context.Background()) {
		t.Fatal("request did not start worker")
	}
	scheduler.Wait()
	if !errors.Is(scheduler.LastError(), wantErr) {
		t.Fatalf("LastError = %v, want %v", scheduler.LastError(), wantErr)
	}
}

func TestWindowRefreshScheduler_CloseDoesNotWaitForRunningRefresh(t *testing.T) {
	target := newBlockingRefreshTarget()
	scheduler := newWindowRefreshScheduler(target, nil)

	if !scheduler.Request(context.Background()) {
		t.Fatal("request did not start worker")
	}
	target.waitStarted(t)

	done := make(chan struct{})
	go func() {
		scheduler.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close blocked behind running refresh")
	}

	close(target.release)
	scheduler.Wait()
}

func TestWindowRefreshScheduler_SuppressesNotifyAfterClose(t *testing.T) {
	target := newBlockingRefreshTarget()
	notify := make(chan struct{}, 1)
	scheduler := newWindowRefreshScheduler(target, func() {
		notify <- struct{}{}
	})

	if !scheduler.Request(context.Background()) {
		t.Fatal("request did not start worker")
	}
	target.waitStarted(t)
	scheduler.Close()

	close(target.release)
	scheduler.Wait()

	select {
	case <-notify:
		t.Fatal("notify was called after Close")
	default:
	}
	if got := target.calls(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
}

type fakeRefreshTarget struct {
	err error
}

func (f *fakeRefreshTarget) Refresh(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.err
}

type blockingRefreshTarget struct {
	started chan struct{}
	release chan struct{}
	count   chan struct{}
}

func newBlockingRefreshTarget() *blockingRefreshTarget {
	return &blockingRefreshTarget{
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
		count:   make(chan struct{}, 4),
	}
}

func (b *blockingRefreshTarget) Refresh(ctx context.Context) error {
	b.count <- struct{}{}
	b.started <- struct{}{}
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *blockingRefreshTarget) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-b.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for refresh start")
	}
}

func (b *blockingRefreshTarget) calls() int {
	return len(b.count)
}
