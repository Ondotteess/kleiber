//go:build gio

package ui

import (
	"errors"
	"testing"
)

func TestNewGioRenderer_NilShellError(t *testing.T) {
	_, err := NewGioRenderer(nil)
	if !errors.Is(err, ErrNilShell) {
		t.Fatalf("NewGioRenderer err = %v, want ErrNilShell", err)
	}
}
