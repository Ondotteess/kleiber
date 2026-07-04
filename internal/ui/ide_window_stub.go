//go:build !gio

package ui

import "context"

// RunIDEWindow is available only in builds with -tags=gio. Without the tag
// the experimental IDE window is unavailable and the call returns
// ErrGioUnavailable (declared in gio_renderer_stub.go).
func RunIDEWindow(ctx context.Context, wb *Workbench, opts IDEWindowOptions) error {
	_ = ctx
	_ = wb
	_ = opts
	return ErrGioUnavailable
}
