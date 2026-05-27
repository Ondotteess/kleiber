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

func defaultRunOptions() runOptions {
	return runOptions{launchUI: launchExperimentalGioUI}
}

func launchExperimentalGioUI(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions, stderr io.Writer) error {
	go func() {
		if err := ui.RunGioWindow(ctx, shell, opts); err != nil {
			fmt.Fprintln(stderr, "kleiber experimental-ui:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	gioapp.Main()
	return nil
}
