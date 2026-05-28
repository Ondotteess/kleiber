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
	gioProcessExit  = os.Exit
)

func runGioAppMain() {
	gioapp.Main()
}

func defaultRunOptions() runOptions {
	return runOptions{
		gioUIAvailable: func() bool { return true },
		launchUI:       launchExperimentalGioUI,
	}
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
