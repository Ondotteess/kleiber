//go:build !gio

package main

import (
	"context"
	"io"

	"github.com/Ondotteess/kleiber/internal/ui"
)

func defaultRunOptions() runOptions {
	return runOptions{
		gioUIAvailable: func() bool { return false },
		launchUI: func(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions, stderr io.Writer) error {
			_ = ctx
			_ = opts
			_ = stderr
			shell.Close()
			return errExperimentalUIUnavailable
		},
	}
}
