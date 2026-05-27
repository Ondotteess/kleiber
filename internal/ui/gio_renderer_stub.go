//go:build !gio

package ui

import (
	"context"
	"errors"
)

// ErrGioUnavailable is returned when the experimental UI is requested from a
// binary built without the gio build tag.
var ErrGioUnavailable = errors.New("ui: gio renderer is not built; rebuild with -tags=gio")

// RunGioWindow is available only in builds with -tags=gio.
func RunGioWindow(ctx context.Context, shell *Shell, opts GioWindowOptions) error {
	_ = ctx
	_ = opts
	if shell != nil {
		shell.Close()
	}
	return ErrGioUnavailable
}
