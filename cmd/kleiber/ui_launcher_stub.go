//go:build !gio

package main

import (
	"context"
	"fmt"
	"io"

	"github.com/Ondotteess/kleiber/internal/ui"
)

func defaultRunOptions() runOptions {
	return runOptions{
		launchUI: func(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions, stderr io.Writer) error {
			_ = ctx
			_ = opts
			_ = stderr
			shell.Close()
			return fmt.Errorf("experimental-ui requires a build with -tags=gio")
		},
	}
}
