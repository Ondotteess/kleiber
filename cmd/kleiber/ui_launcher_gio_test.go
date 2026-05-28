//go:build gio

package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Ondotteess/kleiber/internal/ui"
)

func TestLaunchExperimentalGioUI_CallsAppMainAndPropagatesWindowError(t *testing.T) {
	reset := replaceGioLauncherSeams(t)

	wantErr := errors.New("window failed")
	runnerStarted := make(chan struct{})
	gioWindowRunner = func(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions) error {
		_ = ctx
		_ = shell
		_ = opts
		close(runnerStarted)
		return wantErr
	}
	gioProcessExit = reset.recordExit

	mainCalled := false
	gioAppMain = func() {
		mainCalled = true
		<-runnerStarted
	}

	err := launchExperimentalGioUI(context.Background(), nil, ui.GioWindowOptions{}, io.Discard)
	if !mainCalled {
		t.Fatal("gio app main seam was not called")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if code := reset.exitCode(t); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestLaunchExperimentalGioUI_PropagatesNilWindowResult(t *testing.T) {
	reset := replaceGioLauncherSeams(t)

	runnerStarted := make(chan struct{})
	gioWindowRunner = func(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions) error {
		_ = ctx
		_ = shell
		_ = opts
		close(runnerStarted)
		return nil
	}
	gioAppMain = func() {
		<-runnerStarted
	}
	gioProcessExit = reset.recordExit

	if err := launchExperimentalGioUI(context.Background(), nil, ui.GioWindowOptions{}, io.Discard); err != nil {
		t.Fatalf("launchExperimentalGioUI err = %v, want nil", err)
	}
	if code := reset.exitCode(t); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestLaunchExperimentalGioUI_BlockedAppMainExitsWhenWindowReturnsNil(t *testing.T) {
	reset := replaceGioLauncherSeams(t)

	mainEntered := make(chan struct{})
	releaseMain := make(chan struct{})
	gioAppMain = func() {
		close(mainEntered)
		<-releaseMain
	}
	gioWindowRunner = func(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions) error {
		_ = ctx
		_ = shell
		_ = opts
		<-mainEntered
		return nil
	}
	gioProcessExit = reset.recordExit

	errs := make(chan error, 1)
	go func() {
		errs <- launchExperimentalGioUI(context.Background(), nil, ui.GioWindowOptions{}, io.Discard)
	}()

	if code := reset.exitCode(t); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	select {
	case err := <-errs:
		t.Fatalf("launch returned before app main was released: %v", err)
	default:
	}

	close(releaseMain)
	if err := awaitLaunchResult(t, errs); err != nil {
		t.Fatalf("launchExperimentalGioUI err = %v, want nil", err)
	}
}

func TestLaunchExperimentalGioUI_BlockedAppMainReportsErrorAndExitCode(t *testing.T) {
	reset := replaceGioLauncherSeams(t)

	wantErr := errors.New("renderer failed")
	mainEntered := make(chan struct{})
	releaseMain := make(chan struct{})
	var stderr strings.Builder
	gioAppMain = func() {
		close(mainEntered)
		<-releaseMain
	}
	gioWindowRunner = func(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions) error {
		_ = ctx
		_ = shell
		_ = opts
		<-mainEntered
		return wantErr
	}
	gioProcessExit = reset.recordExit

	errs := make(chan error, 1)
	go func() {
		errs <- launchExperimentalGioUI(context.Background(), nil, ui.GioWindowOptions{}, &stderr)
	}()

	if code := reset.exitCode(t); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if got := stderr.String(); !strings.Contains(got, "kleiber experimental-ui: renderer failed") {
		t.Fatalf("stderr = %q, want renderer error report", got)
	}
	select {
	case err := <-errs:
		t.Fatalf("launch returned before app main was released: %v", err)
	default:
	}

	close(releaseMain)
	if err := awaitLaunchResult(t, errs); !errors.Is(err, wantErr) {
		t.Fatalf("launchExperimentalGioUI err = %v, want %v", err, wantErr)
	}
}

func TestLaunchExperimentalGioUI_BlockedAppMainReportsPanicAndExitCode(t *testing.T) {
	reset := replaceGioLauncherSeams(t)

	mainEntered := make(chan struct{})
	releaseMain := make(chan struct{})
	var stderr strings.Builder
	gioAppMain = func() {
		close(mainEntered)
		<-releaseMain
	}
	gioWindowRunner = func(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions) error {
		_ = ctx
		_ = shell
		_ = opts
		<-mainEntered
		panic("boom")
	}
	gioProcessExit = reset.recordExit

	errs := make(chan error, 1)
	go func() {
		errs <- launchExperimentalGioUI(context.Background(), nil, ui.GioWindowOptions{}, &stderr)
	}()

	if code := reset.exitCode(t); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if got := stderr.String(); !strings.Contains(got, "kleiber experimental-ui: window panic: boom") {
		t.Fatalf("stderr = %q, want panic report", got)
	}
	select {
	case err := <-errs:
		t.Fatalf("launch returned before app main was released: %v", err)
	default:
	}

	close(releaseMain)
	err := awaitLaunchResult(t, errs)
	if err == nil || !strings.Contains(err.Error(), "window panic: boom") {
		t.Fatalf("launchExperimentalGioUI err = %v, want window panic error", err)
	}
}

type gioLauncherSeams struct {
	exitCodes chan int
}

func replaceGioLauncherSeams(t *testing.T) *gioLauncherSeams {
	t.Helper()
	origMain := gioAppMain
	origRunner := gioWindowRunner
	origExit := gioProcessExit
	seams := &gioLauncherSeams{exitCodes: make(chan int, 1)}
	t.Cleanup(func() {
		gioAppMain = origMain
		gioWindowRunner = origRunner
		gioProcessExit = origExit
	})
	return seams
}

func (s *gioLauncherSeams) recordExit(code int) {
	select {
	case s.exitCodes <- code:
	default:
	}
}

func (s *gioLauncherSeams) exitCode(t *testing.T) int {
	t.Helper()
	select {
	case code := <-s.exitCodes:
		return code
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for process exit seam")
		return -1
	}
}

func awaitLaunchResult(t *testing.T, errs <-chan error) error {
	t.Helper()
	select {
	case err := <-errs:
		return err
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for launch result")
		return nil
	}
}
