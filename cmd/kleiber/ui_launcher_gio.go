//go:build gio

package main

import (
	"context"
	"fmt"
	"io"
	"os"

	gioapp "gioui.org/app"
	"github.com/Ondotteess/kleiber/internal/ui"
)

var (
	gioAppMain      = runGioAppMain
	gioWindowRunner = ui.RunGioWindow
	gioIDERunner    = ui.RunIDEWindow
	gioProcessExit  = os.Exit
)

func runGioAppMain() {
	gioapp.Main()
}

func defaultRunOptions() runOptions {
	return runOptions{
		gioUIAvailable: func() bool { return true },
		launchUI:       launchExperimentalGioUI,
		launchIDE:      launchIDEGioWindow,
	}
}

// launchIDEGioWindow runs the IDE window on a background goroutine while
// gioapp.Main occupies the main thread, mirroring launchExperimentalGioUI.
// gioapp.Main never returns on desktop, so the goroutine maps the window
// result to a process exit once the window closes.
func launchIDEGioWindow(ctx context.Context, wb *ui.Workbench, opts ui.IDEWindowOptions, stderr io.Writer) error {
	if stderr == nil {
		stderr = io.Discard
	}
	windowResult := make(chan error, 1)
	go func() {
		err := runIDEWindowSafely(ctx, wb, opts)
		// gioProcessExit skips deferred cleanup, so stop the language
		// server here to avoid orphaning gopls after the window closes.
		wb.Close()
		windowResult <- err
		if err != nil {
			fmt.Fprintln(stderr, "kleiber edit:", err)
			gioProcessExit(1)
			return
		}
		gioProcessExit(0)
	}()

	gioAppMain()
	return <-windowResult
}

func runIDEWindowSafely(ctx context.Context, wb *ui.Workbench, opts ui.IDEWindowOptions) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("window panic: %v", v)
		}
	}()
	return gioIDERunner(ctx, wb, opts)
}

func launchExperimentalGioUI(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions, stderr io.Writer) error {
	if stderr == nil {
		stderr = io.Discard
	}
	windowResult := make(chan error, 1)
	go func() {
		err := runGioWindowSafely(ctx, shell, opts)
		windowResult <- err

		// gioapp.Main blocks forever on desktop platforms. Once the window
		// event loop exits, cmd/kleiber owns the terminal lifecycle and maps
		// the window result to a process exit so the prompt returns.
		if err != nil {
			fmt.Fprintln(stderr, "kleiber experimental-ui:", err)
			gioProcessExit(1)
			return
		}
		gioProcessExit(0)
	}()

	gioAppMain()
	return <-windowResult
}

func runGioWindowSafely(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("window panic: %v", v)
		}
	}()
	return gioWindowRunner(ctx, shell, opts)
}
